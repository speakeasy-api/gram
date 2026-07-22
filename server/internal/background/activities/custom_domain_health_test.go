package activities_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
)

func noopEmailService(t *testing.T) *email.Service {
	t.Helper()
	loopsClient := loops.New(t.Context(), testenv.NewLogger(t), nil, "")
	return email.NewService(testenv.NewLogger(t), loopsClient)
}

type stubInfrastructureChecker struct {
	resources []k8s.ManagedCustomDomainResource
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
	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, nil, "custom-domain.example.com", noopEmailService(t), nil)

	err = checker.Check(t.Context(), activities.CheckCustomDomainHealthArgs{
		CustomDomainID: uuid.New(),
		OrganizationID: "test-organization",
		CheckedAt:      time.Now().UTC(),
	})

	require.NoError(t, err)
}

func TestFindOrphanCustomDomainResourcesFlagsUnknownDomains(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_orphan_flagged")
	require.NoError(t, err)
	_, err = customdomainsrepo.New(conn).CreateCustomDomain(t.Context(), customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: "test-organization",
		Domain:         "active.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)

	stub := &stubInfrastructureChecker{resources: []k8s.ManagedCustomDomainResource{
		{Kind: k8s.ProvisionerKindIngress, Name: "active-example-com", Domain: "active.example.com"},
		{Kind: k8s.ProvisionerKindIngress, Name: "orphan-example-com", Domain: "orphan.example.com"},
	}}
	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, stub, "custom-domain.example.com", noopEmailService(t), nil)

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
	_, err = customdomainsrepo.New(conn).CreateCustomDomain(t.Context(), customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: "test-organization",
		Domain:         "active.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)

	stub := &stubInfrastructureChecker{resources: []k8s.ManagedCustomDomainResource{
		{Kind: k8s.ProvisionerKindIngress, Name: "active-example-com", Domain: "active.example.com"},
	}}
	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, stub, "custom-domain.example.com", noopEmailService(t), nil)

	require.NoError(t, checker.FindOrphanResources(t.Context()))
}
