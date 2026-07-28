package activities_test

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func noopEmailService(t *testing.T) *email.Service {
	t.Helper()
	loopsClient := loops.New(t.Context(), testenv.NewLogger(t), nil, "")
	return email.NewService(testenv.NewLogger(t), loopsClient)
}

type stubInfrastructureChecker struct {
	resources []k8s.ManagedCustomDomainResource
}

func createActivatedCustomDomainResource(
	t *testing.T,
	repository *customdomainsrepo.Queries,
	organizationID string,
	domainName string,
	resourceName string,
	kind k8s.ProvisionerKind,
) {
	t.Helper()

	domain, err := repository.CreateCustomDomain(t.Context(), customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  organizationID,
		Domain:          domainName,
		IngressName:     pgtype.Text{String: "", Valid: false},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: string(kind),
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	_, err = repository.UpdateCustomDomain(t.Context(), customdomainsrepo.UpdateCustomDomainParams{
		Verified:        true,
		Activated:       true,
		IngressName:     pgtype.Text{String: resourceName, Valid: true},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: string(kind),
		ID:              domain.ID,
	})
	require.NoError(t, err)
}

func (s *stubInfrastructureChecker) CheckCustomDomainInfrastructure(ctx context.Context, check k8s.CustomDomainInfrastructureCheck) (k8s.CustomDomainInfrastructureHealth, error) {
	return k8s.CustomDomainInfrastructureHealth{Issue: "", CertificateExpiresAt: nil}, nil
}

func (s *stubInfrastructureChecker) ListManagedCustomDomainResources(ctx context.Context) ([]k8s.ManagedCustomDomainResource, error) {
	return s.resources, nil
}

func TestCustomDomainHealthCheckMissingDomainIsNoop(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_health_missing")
	require.NoError(t, err)
	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, nil, "custom-domain.example.com", noopEmailService(t), nil, nil)

	_, err = checker.Check(t.Context(), activities.CheckCustomDomainHealthArgs{
		CustomDomainID: uuid.New(),
		OrganizationID: "test-organization",
		CheckedAt:      time.Now().UTC(),
	})

	require.NoError(t, err)
}

func createActivatedCustomDomain(t *testing.T, repository *customdomainsrepo.Queries, organizationID, domainName string) uuid.UUID {
	t.Helper()

	domain, err := repository.CreateCustomDomain(t.Context(), customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  organizationID,
		Domain:          domainName,
		IngressName:     pgtype.Text{String: "", Valid: false},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: string(k8s.ProvisionerKindIngress),
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	_, err = repository.UpdateCustomDomain(t.Context(), customdomainsrepo.UpdateCustomDomainParams{
		Verified:        true,
		Activated:       true,
		IngressName:     pgtype.Text{String: domainName + "-resource", Valid: true},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: string(k8s.ProvisionerKindIngress),
		ID:              domain.ID,
	})
	require.NoError(t, err)
	return domain.ID
}

// mismatchResolverConfig simulates a proxied domain: the CNAME is flattened to
// the domain itself and its A records do not intersect the expected target's.
func mismatchResolverConfig(domainName string) dns.MockResolverConfig {
	return dns.MockResolverConfig{
		LookupCNAMEFunc: func(_ context.Context, host string) (string, error) { return host + ".", nil },
		LookupHostFunc: func(_ context.Context, host string) ([]string, error) {
			if host == domainName {
				return []string{"1.2.3.4"}, nil
			}
			return []string{"5.6.7.8"}, nil
		},
		LookupTXTFunc: func(context.Context, string) ([]string, error) { return nil, nil },
	}
}

func TestCustomDomainHealthCheckProbeRescuesDNSMismatch(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_health_probe_rescue")
	require.NoError(t, err)
	repository := customdomainsrepo.New(conn)
	domainID := createActivatedCustomDomain(t, repository, "test-organization", "proxied.example.com")

	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, &stubInfrastructureChecker{resources: nil}, "custom-domain.example.com", noopEmailService(t), nil, nil)
	checker.SetResolver(dns.NewMockResolver(mismatchResolverConfig("proxied.example.com")))
	checker.SetProbe(func(context.Context, string) error { return nil })
	checker.SetDryRun(false)

	notification, err := checker.Check(t.Context(), activities.CheckCustomDomainHealthArgs{
		CustomDomainID: domainID,
		OrganizationID: "test-organization",
		CheckedAt:      time.Now().UTC(),
	})
	require.NoError(t, err)
	require.Empty(t, notification.Issue)

	domain, err := repository.GetCustomDomainByIDAndOrganization(t.Context(), customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{
		ID:             domainID,
		OrganizationID: "test-organization",
	})
	require.NoError(t, err)
	require.Equal(t, "healthy", domain.HealthStatus.String)
}

func TestCustomDomainHealthCheckProbeFailureKeepsDNSMismatch(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_health_probe_fail")
	require.NoError(t, err)
	repository := customdomainsrepo.New(conn)
	domainID := createActivatedCustomDomain(t, repository, "test-organization", "broken.example.com")

	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, &stubInfrastructureChecker{resources: nil}, "custom-domain.example.com", noopEmailService(t), nil, nil)
	checker.SetResolver(dns.NewMockResolver(mismatchResolverConfig("broken.example.com")))
	checker.SetProbe(func(context.Context, string) error { return errors.New("connection refused") })
	checker.SetDryRun(false)

	notification, err := checker.Check(t.Context(), activities.CheckCustomDomainHealthArgs{
		CustomDomainID: domainID,
		OrganizationID: "test-organization",
		CheckedAt:      time.Now().UTC(),
	})
	require.NoError(t, err)
	require.Equal(t, customdomains.HealthIssueDNSTargetMismatch, notification.Issue)

	domain, err := repository.GetCustomDomainByIDAndOrganization(t.Context(), customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{
		ID:             domainID,
		OrganizationID: "test-organization",
	})
	require.NoError(t, err)
	require.Equal(t, "unhealthy", domain.HealthStatus.String)
	require.Equal(t, string(customdomains.HealthIssueDNSTargetMismatch), domain.HealthIssue.String)
}

func TestCustomDomainHealthCheckRetryReemitsNotification(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_health_retry_reemit")
	require.NoError(t, err)
	repository := customdomainsrepo.New(conn)
	domainID := createActivatedCustomDomain(t, repository, "test-organization", "retry.example.com")

	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, &stubInfrastructureChecker{resources: nil}, "custom-domain.example.com", noopEmailService(t), nil, nil)
	checker.SetResolver(dns.NewMockResolver(mismatchResolverConfig("retry.example.com")))
	checker.SetProbe(func(context.Context, string) error { return errors.New("connection refused") })
	checker.SetDryRun(false)

	// Match the workflow's pinned timestamp precision.
	checkedAt := time.Now().UTC().Truncate(time.Microsecond)
	args := activities.CheckCustomDomainHealthArgs{
		CustomDomainID: domainID,
		OrganizationID: "test-organization",
		CheckedAt:      checkedAt,
	}

	first, err := checker.Check(t.Context(), args)
	require.NoError(t, err)
	require.Equal(t, customdomains.HealthIssueDNSTargetMismatch, first.Issue)

	// A retry after the transition committed must return the same answer.
	second, err := checker.Check(t.Context(), args)
	require.NoError(t, err)
	require.Equal(t, first, second)

	// A later check of the still-unhealthy domain is not a transition.
	third, err := checker.Check(t.Context(), activities.CheckCustomDomainHealthArgs{
		CustomDomainID: domainID,
		OrganizationID: "test-organization",
		CheckedAt:      checkedAt.Add(time.Hour),
	})
	require.NoError(t, err)
	require.Empty(t, third.Issue)
}

func TestCustomDomainHealthCheckDryRunObservesWithoutPersisting(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_health_dry_run")
	require.NoError(t, err)
	repository := customdomainsrepo.New(conn)
	domainID := createActivatedCustomDomain(t, repository, "test-organization", "dryrun.example.com")

	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, &stubInfrastructureChecker{resources: nil}, "custom-domain.example.com", noopEmailService(t), nil, nil)
	checker.SetResolver(dns.NewMockResolver(mismatchResolverConfig("dryrun.example.com")))
	checker.SetProbe(func(context.Context, string) error { return errors.New("connection refused") })

	notification, err := checker.Check(t.Context(), activities.CheckCustomDomainHealthArgs{
		CustomDomainID: domainID,
		OrganizationID: "test-organization",
		CheckedAt:      time.Now().UTC(),
	})
	require.NoError(t, err)
	require.Equal(t, customdomains.HealthIssueDNSTargetMismatch, notification.Issue)
	require.Equal(t, "dryrun.example.com", notification.Domain)

	domain, err := repository.GetCustomDomainByIDAndOrganization(t.Context(), customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{
		ID:             domainID,
		OrganizationID: "test-organization",
	})
	require.NoError(t, err)
	require.False(t, domain.HealthStatus.Valid)
	require.False(t, domain.HealthIssue.Valid)
	require.False(t, domain.HealthCheckedAt.Valid)
	require.False(t, domain.UnhealthySince.Valid)
	require.False(t, domain.ConsecutiveFailures.Valid)
}

// seedCustomDomainAdminRecipient creates an organization with one member who
// holds an org:admin grant, returning the member's email.
func seedCustomDomainAdminRecipient(t *testing.T, conn *pgxpool.Pool, organizationID string) string {
	t.Helper()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Test Org",
		Slug:        organizationID,
		WorkosID:    conv.PtrToPGText(conv.PtrEmpty("workos-" + organizationID)),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	userID := "custom-domain-health-admin"
	adminEmail := "domain-health-admin@example.com"
	_, err = usersrepo.New(conn).UpsertUser(t.Context(), usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       adminEmail,
		DisplayName: "Domain Health Admin",
		PhotoUrl:    pgtype.Text{String: "", Valid: false},
		Admin:       false,
	})
	require.NoError(t, err)

	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(t.Context(), testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: organizationID,
		UserID:         pgtype.Text{String: userID, Valid: true},
	}))

	selectors, err := authz.NewSelector(authz.ScopeOrgAdmin, organizationID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, userID),
		Scope:          string(authz.ScopeOrgAdmin),
		Effect:         pgtype.Text{String: "", Valid: false},
		Selectors:      selectors,
	})
	require.NoError(t, err)

	return adminEmail
}

func TestCustomDomainNotifyOrgAdminsDryRunSendsNoEmail(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_notify_dry_run")
	require.NoError(t, err)
	organizationID := "org-custom-domain-health-dry"
	seedCustomDomainAdminRecipient(t, conn, organizationID)

	captured := &captureLoopsClient{sent: nil, failNext: 0}
	siteURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, &stubInfrastructureChecker{resources: nil}, "custom-domain.example.com", email.NewService(testenv.NewLogger(t), captured), siteURL, nil)

	err = checker.NotifyOrgAdmins(t.Context(), activities.NotifyCustomDomainUnhealthyArgs{
		CustomDomainID: uuid.New(),
		OrganizationID: organizationID,
		Domain:         "dryrun.example.com",
		Issue:          customdomains.HealthIssueDNSNotFound,
		CheckedAt:      time.Now().UTC().Truncate(time.Microsecond),
	})
	require.NoError(t, err)
	require.Empty(t, captured.Sent())
}

func TestCustomDomainNotifyOrgAdminsSendsIdempotentEmail(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_notify_live")
	require.NoError(t, err)
	organizationID := "org-custom-domain-health-live"
	adminEmail := seedCustomDomainAdminRecipient(t, conn, organizationID)

	captured := &captureLoopsClient{sent: nil, failNext: 0}
	siteURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, &stubInfrastructureChecker{resources: nil}, "custom-domain.example.com", email.NewService(testenv.NewLogger(t), captured), siteURL, nil)
	checker.SetDryRun(false)

	err = checker.NotifyOrgAdmins(t.Context(), activities.NotifyCustomDomainUnhealthyArgs{
		CustomDomainID: uuid.New(),
		OrganizationID: organizationID,
		Domain:         "live.example.com",
		Issue:          customdomains.HealthIssueDNSNotFound,
		CheckedAt:      time.Now().UTC().Truncate(time.Microsecond),
	})
	require.NoError(t, err)

	sent := captured.Sent()
	require.Len(t, sent, 1)
	require.Equal(t, adminEmail, sent[0].Email)
	require.NotEmpty(t, sent[0].IdempotencyKey)
}

func TestFindOrphanCustomDomainResourcesFlagsUnknownDomains(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_orphan_flagged")
	require.NoError(t, err)
	createActivatedCustomDomainResource(t, customdomainsrepo.New(conn), "test-organization", "active.example.com", "active-example-com", k8s.ProvisionerKindIngress)

	stub := &stubInfrastructureChecker{resources: []k8s.ManagedCustomDomainResource{
		{Kind: k8s.ProvisionerKindIngress, Name: "active-example-com", Domain: "active.example.com"},
		{Kind: k8s.ProvisionerKindIngress, Name: "orphan-example-com", Domain: "orphan.example.com"},
	}}
	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, stub, "custom-domain.example.com", noopEmailService(t), nil, nil)

	err = checker.FindOrphanResources(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "1 orphaned custom domain resources")
	require.Contains(t, err.Error(), "orphan.example.com")
	require.NotContains(t, err.Error(), "active.example.com")
}

func TestFindOrphanCustomDomainResourcesAllResourcesAccountedFor(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_orphan_clean")
	require.NoError(t, err)
	createActivatedCustomDomainResource(t, customdomainsrepo.New(conn), "test-organization", "active.example.com", "active-example-com", k8s.ProvisionerKindIngress)

	stub := &stubInfrastructureChecker{resources: []k8s.ManagedCustomDomainResource{
		{Kind: k8s.ProvisionerKindIngress, Name: "active-example-com", Domain: "active.example.com"},
	}}
	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, stub, "custom-domain.example.com", noopEmailService(t), nil, nil)

	require.NoError(t, checker.FindOrphanResources(t.Context()))
}

func TestFindOrphanCustomDomainResourcesFlagsUnactivatedDomain(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_orphan_unactivated")
	require.NoError(t, err)
	_, err = customdomainsrepo.New(conn).CreateCustomDomain(t.Context(), customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  "test-organization",
		Domain:          "pending.example.com",
		IngressName:     pgtype.Text{String: "", Valid: false},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: string(k8s.ProvisionerKindIngress),
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	stub := &stubInfrastructureChecker{resources: []k8s.ManagedCustomDomainResource{
		{Kind: k8s.ProvisionerKindIngress, Name: "pending-example-com", Domain: "pending.example.com"},
	}}
	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, stub, "custom-domain.example.com", noopEmailService(t), nil, nil)

	err = checker.FindOrphanResources(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "1 orphaned custom domain resources")
	require.Contains(t, err.Error(), "pending-example-com")
}

func TestFindOrphanCustomDomainResourcesFlagsMismatchedIdentity(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_orphan_identity")
	require.NoError(t, err)
	createActivatedCustomDomainResource(t, customdomainsrepo.New(conn), "test-organization", "active.example.com", "active-example-com", k8s.ProvisionerKindIngress)

	stub := &stubInfrastructureChecker{resources: []k8s.ManagedCustomDomainResource{
		{Kind: k8s.ProvisionerKindIngress, Name: "active-example-com", Domain: "active.example.com"},
		{Kind: k8s.ProvisionerKindIngress, Name: "duplicate-active-example-com", Domain: "active.example.com"},
	}}
	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, stub, "custom-domain.example.com", noopEmailService(t), nil, nil)

	err = checker.FindOrphanResources(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "1 orphaned custom domain resources")
	require.Contains(t, err.Error(), "duplicate-active-example-com")
}
