package activities_test

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/dns"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func newPassingDNSResolverConfig(targetCNAME, domain, orgID string) dns.MockResolverConfig {
	return dns.MockResolverConfig{
		LookupCNAMEFunc: func(context.Context, string) (string, error) { return targetCNAME, nil },
		LookupNetIPFunc: func(context.Context, string, string) ([]netip.Addr, error) { return nil, fmt.Errorf("no A record") },
		LookupTXTFunc: func(context.Context, string) ([]string, error) {
			return []string{fmt.Sprintf("gram-domain-verify=%s,%s", domain, orgID)}, nil
		},
	}
}

type testInstance struct {
	conn     *pgxpool.Pool
	repo     *customdomainsRepo.Queries
	resolver dns.Resolver
}

const testTargetCNAME = "target.gram.ai."

func newTestInstance(t *testing.T, orgID, domain string) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "verify_domain_test")
	require.NoError(t, err)

	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	return ctx, &testInstance{
		conn:     conn,
		repo:     customdomainsRepo.New(conn),
		resolver: dns.NewMockResolver(newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)),
	}
}

func newActivity(t *testing.T, ti *testInstance) *activities.VerifyCustomDomain {
	t.Helper()

	logger := testenv.NewLogger(t)
	activity := activities.NewVerifyCustomDomain(logger, ti.conn, audit.NewLogger(), testTargetCNAME, nil)
	activity.SetResolver(ti.resolver)

	return activity
}

func createDomainRow(t *testing.T, ctx context.Context, ti *testInstance, orgID, domain string) customdomainsRepo.CustomDomain {
	t.Helper()

	row, err := ti.repo.CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          domain,
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	return row
}

func TestVerifyCustomDomain_VerifiesAndPersists(t *testing.T) {
	t.Parallel()

	const orgID = "org-verify-persist"
	const domain = "verify-persist.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)
	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)

	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, activities.VerifyStatusVerified, result.Status)

	got, err := ti.repo.GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, err)
	require.True(t, got.Verified, "TXT success must persist verified=true")
	require.False(t, got.Activated, "verification must not activate the domain")
}

func TestVerifyCustomDomain_MissingRowTerminates(t *testing.T) {
	t.Parallel()

	const orgID = "org-missing-row"
	const domain = "missing-row.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)
	activity := newActivity(t, ti)

	_, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  uuid.Must(uuid.NewV7()),
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, activities.ErrTypeCustomDomainInvalid, appErr.Type())
	require.True(t, appErr.NonRetryable(), "a vanished row must terminate the workflow")
}

func TestVerifyCustomDomain_DeletedRowTerminates(t *testing.T) {
	t.Parallel()

	const orgID = "org-deleted-row"
	const domain = "deleted-row.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)
	row := createDomainRow(t, ctx, ti, orgID, domain)
	require.NoError(t, ti.repo.DeleteCustomDomain(ctx, orgID))
	activity := newActivity(t, ti)

	_, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, activities.ErrTypeCustomDomainInvalid, appErr.Type())
	require.True(t, appErr.NonRetryable())
}

func TestVerifyCustomDomain_HostnameReuseTerminatesStaleWorkflow(t *testing.T) {
	t.Parallel()

	const orgID = "org-hostname-reuse"
	const domain = "hostname-reuse.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	// The stale workflow's row was deleted and the hostname re-registered
	// under a new row; the stale workflow still carries the old row's ID.
	stale := createDomainRow(t, ctx, ti, orgID, domain)
	require.NoError(t, ti.repo.DeleteCustomDomain(ctx, orgID))
	successor := createDomainRow(t, ctx, ti, orgID, domain)
	require.NotEqual(t, stale.ID, successor.ID)

	activity := newActivity(t, ti)
	_, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  stale.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.True(t, appErr.NonRetryable())

	got, err := ti.repo.GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, err)
	require.False(t, got.Verified, "the successor row must not be verified by the stale workflow")
}

func TestVerifyCustomDomain_LegacyArgsWithoutIDCreateMissingRow(t *testing.T) {
	t.Parallel()

	const orgID = "org-legacy-create"
	const domain = "legacy-create.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)
	activity := newActivity(t, ti)

	// Workflows started by the pre-v2 API rely on this activity to create the
	// row; a missing row on the zero-ID path is creation, not termination.
	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  uuid.Nil,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, activities.VerifyStatusVerified, result.Status)

	got, err := ti.repo.GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, err)
	require.Equal(t, orgID, got.OrganizationID)
	require.True(t, got.Verified)
}

func TestVerifyCustomDomain_LegacyArgsWithoutIDVerifyByHostname(t *testing.T) {
	t.Parallel()

	const orgID = "org-legacy-args"
	const domain = "legacy-args.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)
	createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)

	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  uuid.Nil,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, activities.VerifyStatusVerified, result.Status)
}

func TestVerifyCustomDomain_ExistingDomainDifferentOrg(t *testing.T) {
	t.Parallel()

	const ownerOrg = "org-owner"
	const otherOrg = "org-other"
	const domain = "owned.example.com"
	ctx, ti := newTestInstance(t, otherOrg, domain)
	row := createDomainRow(t, ctx, ti, ownerOrg, domain)
	activity := newActivity(t, ti)

	_, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           otherOrg,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom domain does not match this registration")

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.True(t, appErr.NonRetryable())
}

func TestVerifyCustomDomain_TransientDBError(t *testing.T) {
	t.Parallel()

	const orgID = "org-transient"
	const domain = "transient.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)
	activity := newActivity(t, ti)

	// Close the pool to simulate a transient DB error during the row lookup.
	ti.conn.Close()

	_, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  uuid.Nil,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	if fmt.Sprintf("%T", err) == "*temporal.ApplicationError" {
		require.ErrorAs(t, err, &appErr)
		require.False(t, appErr.NonRetryable(), "infrastructure failures must stay retryable")
	}
}

func TestVerifyCustomDomain_InvalidDomain(t *testing.T) {
	t.Parallel()

	const orgID = "org-invalid"
	ctx, ti := newTestInstance(t, orgID, "x.example.com")
	activity := newActivity(t, ti)

	_, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          "not a valid domain!!!",
		CustomDomainID:  uuid.Nil,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "domain is invalid")

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.True(t, appErr.NonRetryable())
}

func TestVerifyCustomDomain_ProhibitedDomain(t *testing.T) {
	t.Parallel()

	const orgID = "org-prohibited"
	ctx, ti := newTestInstance(t, orgID, "x.example.com")
	activity := newActivity(t, ti)

	_, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          "docs.getgram.ai",
		CustomDomainID:  uuid.Nil,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "prohibited")
}

func TestVerifyCustomDomain_CNAMEMismatchProceedsToOwnership(t *testing.T) {
	t.Parallel()

	const orgID = "org-cname-mismatch"
	const domain = "cname-mismatch.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	// A CNAME pointing elsewhere is advisory (proxied/CDN setups do this);
	// the TXT ownership record decides verification.
	cfg := newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)
	cfg.LookupCNAMEFunc = func(context.Context, string) (string, error) { return "wrong.target.com.", nil }
	ti.resolver = dns.NewMockResolver(cfg)

	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)

	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, activities.VerifyStatusVerified, result.Status)
}

func TestVerifyCustomDomain_TXTRecordMismatchIsPending(t *testing.T) {
	t.Parallel()

	const orgID = "org-txt-mismatch"
	const domain = "txt-mismatch.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	// A wrong TXT value is indistinguishable from a stale record still
	// propagating, so the pass reports pending instead of failing.
	cfg := newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)
	cfg.LookupTXTFunc = func(context.Context, string) ([]string, error) { return []string{"wrong-value"}, nil }
	ti.resolver = dns.NewMockResolver(cfg)

	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)

	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, activities.VerifyStatusDNSPending, result.Status)
	require.Contains(t, result.Reason, "TXT record")

	got, err := ti.repo.GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, err)
	require.False(t, got.Verified)
}

func TestVerifyCustomDomain_DNSLookupFailsNoARecord(t *testing.T) {
	t.Parallel()

	const orgID = "org-dns-fail"
	const domain = "dns-fail.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	// CNAME fails and the address lookup errors in a non-NXDOMAIN way.
	cfg := newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)
	cfg.LookupCNAMEFunc = func(context.Context, string) (string, error) { return "", fmt.Errorf("no CNAME") }
	cfg.LookupNetIPFunc = func(context.Context, string, string) ([]netip.Addr, error) { return nil, fmt.Errorf("no A record") }
	ti.resolver = dns.NewMockResolver(cfg)

	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)

	_, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to find custom domain mapping")
}

func TestVerifyCustomDomain_CNAMEFailsButARecordExists(t *testing.T) {
	t.Parallel()

	const orgID = "org-a-record"
	const domain = "a-record.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	// Both hosts resolve to the same A record, so verification can fall back after CNAME fails.
	ti.resolver = dns.NewMockResolver(dns.MockResolverConfig{
		LookupCNAMEFunc: func(context.Context, string) (string, error) { return "", fmt.Errorf("no CNAME") },
		LookupNetIPFunc: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.2.3.4")}, nil
		},
		LookupTXTFunc: func(context.Context, string) ([]string, error) {
			return []string{fmt.Sprintf("gram-domain-verify=%s,%s", domain, orgID)}, nil
		},
	})

	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)

	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, activities.VerifyStatusVerified, result.Status)
}

func TestVerifyCustomDomain_ApexMatchesConfiguredARecords(t *testing.T) {
	t.Parallel()

	const orgID = "org-apex-configured"
	const domain = "apex-configured.example"
	ctx, ti := newTestInstance(t, orgID, domain)

	// The apex resolves straight to the configured static ingress IP; the
	// CNAME target itself does not resolve at all. Verification must not
	// depend on live resolution of the CNAME target when static IPs match.
	ti.resolver = dns.NewMockResolver(dns.MockResolverConfig{
		LookupCNAMEFunc: func(context.Context, string) (string, error) { return "", fmt.Errorf("no CNAME") },
		LookupNetIPFunc: func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
			if host == domain {
				return []netip.Addr{netip.MustParseAddr("34.127.46.134")}, nil
			}
			return nil, fmt.Errorf("expected target does not resolve")
		},
		LookupTXTFunc: func(context.Context, string) ([]string, error) {
			return []string{fmt.Sprintf("gram-domain-verify=%s,%s", domain, orgID)}, nil
		},
	})

	row := createDomainRow(t, ctx, ti, orgID, domain)
	logger := testenv.NewLogger(t)
	activity := activities.NewVerifyCustomDomain(logger, ti.conn, audit.NewLogger(), testTargetCNAME, []netip.Addr{netip.MustParseAddr("34.127.46.134")})
	activity.SetResolver(ti.resolver)

	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, activities.VerifyStatusVerified, result.Status)
}

func TestVerifyCustomDomain_CanonicalNameIsDomainAndARecordMatches(t *testing.T) {
	t.Parallel()

	const orgID = "org-flattened-record"
	const domain = "flattened-record.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	ti.resolver = dns.NewMockResolver(dns.MockResolverConfig{
		LookupCNAMEFunc: func(context.Context, string) (string, error) { return domain + ".", nil },
		LookupNetIPFunc: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.2.3.4")}, nil
		},
		LookupTXTFunc: func(context.Context, string) ([]string, error) {
			return []string{fmt.Sprintf("gram-domain-verify=%s,%s", domain, orgID)}, nil
		},
	})

	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)
	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})

	require.NoError(t, err)
	require.Equal(t, activities.VerifyStatusVerified, result.Status)
}

func TestVerifyCustomDomain_CNAMEFailsAndARecordPointsElsewhere(t *testing.T) {
	t.Parallel()

	const orgID = "org-a-record-mismatch"
	const domain = "a-record-mismatch.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	ti.resolver = dns.NewMockResolver(dns.MockResolverConfig{
		LookupCNAMEFunc: func(context.Context, string) (string, error) { return "", fmt.Errorf("no CNAME") },
		LookupNetIPFunc: func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
			if host == domain {
				return []netip.Addr{netip.MustParseAddr("1.2.3.4")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("5.6.7.8")}, nil
		},
		LookupTXTFunc: func(context.Context, string) ([]string, error) {
			return []string{fmt.Sprintf("gram-domain-verify=%s,%s", domain, orgID)}, nil
		},
	})

	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)
	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})

	// A records pointing elsewhere (forwarding proxies) are advisory; the TXT
	// ownership record decides verification.
	require.NoError(t, err)
	require.Equal(t, activities.VerifyStatusVerified, result.Status)
}

func TestVerifyCustomDomain_SpecialTestDomainAllowed(t *testing.T) {
	t.Parallel()

	const orgID = "org-special"
	const domain = "chat.speakeasy.com"
	ctx, ti := newTestInstance(t, orgID, domain)
	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)

	_, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	// Should not be rejected as prohibited
	if err != nil {
		require.NotContains(t, err.Error(), "domain is prohibited")
	}
}

func TestVerifyCustomDomain_TXTLookupError(t *testing.T) {
	t.Parallel()

	const orgID = "org-txt-error"
	const domain = "txt-error.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	// TXT lookup fails entirely
	cfg := newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)
	cfg.LookupTXTFunc = func(context.Context, string) ([]string, error) { return nil, fmt.Errorf("DNS timeout") }
	ti.resolver = dns.NewMockResolver(cfg)

	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)

	_, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to find TXT record")
}

func TestVerifyCustomDomain_NXDOMAINOnCNAMEAndAIsPending(t *testing.T) {
	t.Parallel()

	const orgID = "org-nxdomain-cname"
	const domain = "nxdomain-cname.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	nxdomain := &net.DNSError{Err: "no such host", Name: domain, IsNotFound: true}
	cfg := newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)
	cfg.LookupCNAMEFunc = func(context.Context, string) (string, error) { return "", nxdomain }
	cfg.LookupNetIPFunc = func(context.Context, string, string) ([]netip.Addr, error) { return nil, nxdomain }
	ti.resolver = dns.NewMockResolver(cfg)

	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)

	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.NoError(t, err, "NXDOMAIN is in-flight propagation, not a failure")
	require.Equal(t, activities.VerifyStatusDNSPending, result.Status)
}

func TestVerifyCustomDomain_NXDOMAINOnTXTIsPending(t *testing.T) {
	t.Parallel()

	const orgID = "org-nxdomain-txt"
	const domain = "nxdomain-txt.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	txtName := "_gram." + domain
	nxdomain := &net.DNSError{Err: "no such host", Name: txtName, IsNotFound: true}
	cfg := newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)
	cfg.LookupTXTFunc = func(context.Context, string) ([]string, error) { return nil, nxdomain }
	ti.resolver = dns.NewMockResolver(cfg)

	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)

	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, activities.VerifyStatusDNSPending, result.Status)
	require.Contains(t, result.Reason, txtName)

	got, err := ti.repo.GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, err)
	require.False(t, got.Verified)
}

func TestVerifyCustomDomain_CAAForbiddenIsPending(t *testing.T) {
	t.Parallel()

	const orgID = "org-caa-forbidden"
	const domain = "caa-forbidden.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	cfg := newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)
	cfg.LookupCAAFunc = func(_ context.Context, name string) ([]dns.CAA, error) {
		if name == domain {
			return []dns.CAA{{Flag: 0, Tag: "issue", Value: "pki.goog"}}, nil
		}
		return nil, nil
	}
	ti.resolver = dns.NewMockResolver(cfg)

	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)

	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, activities.VerifyStatusDNSPending, result.Status)
	require.Contains(t, result.Reason, "letsencrypt.org")

	got, err := ti.repo.GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, err)
	require.False(t, got.Verified)
}

func TestVerifyCustomDomain_CAAAllowsLetsEncrypt(t *testing.T) {
	t.Parallel()

	const orgID = "org-caa-allows"
	const domain = "caa-allows.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	cfg := newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)
	cfg.LookupCAAFunc = func(_ context.Context, name string) ([]dns.CAA, error) {
		if name == domain {
			return []dns.CAA{
				{Flag: 0, Tag: "issue", Value: "pki.goog"},
				{Flag: 0, Tag: "issue", Value: "letsencrypt.org"},
			}, nil
		}
		return nil, nil
	}
	ti.resolver = dns.NewMockResolver(cfg)

	row := createDomainRow(t, ctx, ti, orgID, domain)
	activity := newActivity(t, ti)

	result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  row.ID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: "",
		IPAllowlist:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, activities.VerifyStatusVerified, result.Status)
}

// Verify ErrNoRows is what sqlc returns for missing rows (sanity check).
func TestGetCustomDomainByDomain_ReturnsErrNoRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestInstance(t, "org-sanity", "x.example.com")

	_, err := ti.repo.GetCustomDomainByDomain(ctx, "nonexistent.example.com")
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
