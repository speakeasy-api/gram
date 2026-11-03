package platformmcp

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"

	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
)

func TestCustomDomainStatusContract(t *testing.T) {
	t.Parallel()
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil))
	registerCustomDomainStatusTool(reg, nil, nil)
	descriptors := reg.Descriptors()
	require.Len(t, descriptors, 1)
	require.Equal(t, ExternalAuthorizationOrgAdmin, descriptors[0].Meta.Authorization)
	require.Equal(t, ProjectScopeNone, descriptors[0].Meta.ProjectScope)
	require.Equal(t, externalOnly, descriptors[0].Meta.Audiences)
	_, err := readCustomDomainStatus(t.Context(), nil, "org-example")
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestCustomDomainStatusAuthorizationAndUnavailable(t *testing.T) {
	t.Parallel()
	server := newTestMCPServer()
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerCustomDomainStatusTool(reg, nil, nil)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "domain-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	params := &mcp.CallToolParams{Name: "get_custom_domain_status", Arguments: map[string]any{}}
	result, err := session.CallTool(t.Context(), params)
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	message, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, message.Text, "temporarily unavailable")
	reg.withExternalAuthorizer(denyExternalCallAuthorizer{err: &ExternalAuthorizationError{RequiredScope: "org:admin", cause: ErrForbidden}})
	result, err = session.CallTool(t.Context(), params)
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	message, ok = result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, message.Text, "permission_denied")
	require.NotContains(t, message.Text, "temporarily unavailable")
}

func TestCustomDomainStatusTenantIsolation(t *testing.T) {
	t.Parallel()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "custom_domain_diagnostics")
	require.NoError(t, err)
	repo := customdomainsrepo.New(conn)
	domain, err := repo.CreateCustomDomain(t.Context(), customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: "org-example", Domain: "mcp.example.com", ProvisionerKind: "local", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	checkedAt := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	_, err = repo.UpdateCustomDomainHealth(t.Context(), customdomainsrepo.UpdateCustomDomainHealthParams{
		ID: domain.ID, OrganizationID: "org-example",
		DnsConfigurationStatus: pgtype.Text{String: "unconfirmed", Valid: true},
		DnsRoutingMethod:       pgtype.Text{String: "unknown", Valid: true},
		HealthStatus:           pgtype.Text{String: "healthy", Valid: true},
		CheckedAt:              checkedAt,
		DnsCheckedAt:           checkedAt,
	})
	require.NoError(t, err)
	got, err := readCustomDomainStatus(t.Context(), conn, "org-example")
	require.NoError(t, err)
	require.True(t, got.Configured)
	require.Equal(t, "unconfirmed", got.DNSConfigurationStatus)
	require.Equal(t, "healthy", got.InfrastructureStatus)
	other, err := readCustomDomainStatus(t.Context(), conn, "org-other")
	require.NoError(t, err)
	require.Equal(t, CustomDomainStatus{}, other)
	require.NoError(t, repo.DeleteCustomDomain(t.Context(), "org-example"))
	deleted, err := readCustomDomainStatus(t.Context(), conn, "org-example")
	require.NoError(t, err)
	require.False(t, deleted.Configured)
}

func TestCustomDomainStatusDNSFailureIsNotInfrastructureFailure(t *testing.T) {
	t.Parallel()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "domain_mixed_worker_observation")
	require.NoError(t, err)
	repo := customdomainsrepo.New(conn)
	domain, err := repo.CreateCustomDomain(t.Context(), customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: "org-example", Domain: "mcp.example.com", ProvisionerKind: "local", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	checkedAt := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	_, err = repo.UpdateCustomDomainHealth(t.Context(), customdomainsrepo.UpdateCustomDomainHealthParams{
		ID: domain.ID, OrganizationID: "org-example",
		DnsConfigurationStatus: pgtype.Text{String: "matched", Valid: true},
		DnsRoutingMethod:       pgtype.Text{String: "shared_cname", Valid: true},
		HealthStatus:           pgtype.Text{String: "unhealthy", Valid: true},
		HealthIssue:            pgtype.Text{String: "dns_target_mismatch", Valid: true},
		CheckedAt:              checkedAt,
		DnsCheckedAt:           checkedAt,
	})
	require.NoError(t, err)
	got, err := readCustomDomainStatus(t.Context(), conn, "org-example")
	require.NoError(t, err)
	require.Equal(t, "unknown", got.InfrastructureStatus)
	require.Empty(t, got.InfrastructureIssue)
}

func TestCustomDomainStatusOlderWorkerCannotRefreshDNSObservation(t *testing.T) {
	t.Parallel()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "domain_older_worker_freshness")
	require.NoError(t, err)
	queries := customdomainsrepo.New(conn)
	domain, err := queries.CreateCustomDomain(t.Context(), customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: "org-example", Domain: "mcp.example.com", ProvisionerKind: "local", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	dnsTime := time.Now().Add(-time.Hour).Truncate(time.Second)
	_, err = queries.UpdateCustomDomainHealth(t.Context(), customdomainsrepo.UpdateCustomDomainHealthParams{
		ID: domain.ID, OrganizationID: "org-example",
		DnsConfigurationStatus: pgtype.Text{String: "matched", Valid: true},
		DnsRoutingMethod:       pgtype.Text{String: "per_domain_cname", Valid: true},
		DnsCheckedAt:           pgtype.Timestamptz{Time: dnsTime, Valid: true},
		HealthStatus:           pgtype.Text{String: "healthy", Valid: true},
		CheckedAt:              pgtype.Timestamptz{Time: dnsTime, Valid: true},
	})
	require.NoError(t, err)
	// Execute the older worker's write shape: it never names DNS columns.
	err = testrepo.New(conn).SetCustomDomainHealthWithoutDNSObservation(t.Context(), testrepo.SetCustomDomainHealthWithoutDNSObservationParams{
		CheckedAt: pgtype.Timestamptz{Time: dnsTime.Add(time.Minute), Valid: true}, ID: domain.ID, OrganizationID: "org-example",
	})
	require.NoError(t, err)
	got, err := readCustomDomainStatus(t.Context(), conn, "org-example")
	require.NoError(t, err)
	require.Equal(t, "matched", got.DNSConfigurationStatus)
	require.Equal(t, dnsTime.UTC().Format(time.RFC3339), got.CheckedAt)
	require.Equal(t, "unknown", got.InfrastructureStatus)
	require.Empty(t, got.InfrastructureIssue)
}
