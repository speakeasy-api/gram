package admin

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsRepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func seedMCPEndpoint(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, serverID uuid.UUID, customDomainID uuid.NullUUID, slug string) uuid.UUID {
	t.Helper()

	e, err := mcpendpointsRepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsRepo.CreateMCPEndpointParams{
		ProjectID:       projectID,
		CustomDomainID:  customDomainID,
		McpServerID:     uuid.NullUUID{UUID: serverID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            slug,
	})
	require.NoError(t, err)

	return e.ID
}

// seedCustomDomain creates a custom domain that is verified and activated, and
// so addressable, unless live is false.
func seedCustomDomain(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, domain string, live bool) uuid.UUID {
	t.Helper()

	queries := customdomainsRepo.New(conn)
	d, err := queries.CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          domain,
		IngressName:     pgtype.Text{String: "", Valid: false},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	if !live {
		return d.ID
	}

	_, err = queries.UpdateCustomDomain(ctx, customdomainsRepo.UpdateCustomDomainParams{
		Verified:        true,
		Activated:       true,
		IngressName:     pgtype.Text{String: "", Valid: false},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: "ingress",
		ID:              d.ID,
	})
	require.NoError(t, err)

	return d.ID
}

func newTestMCPServersService(t *testing.T) (context.Context, *Service, *pgxpool.Pool) {
	t.Helper()

	ctx, svc, conn := newTestAdminService(t)
	serverURL, err := url.Parse("https://gram.example.com")
	require.NoError(t, err)
	svc.SetMCPServerURL(serverURL)

	return ctx, svc, conn
}

func listMCPServers(t *testing.T, ctx context.Context, svc *Service, orgID string, projectID uuid.UUID) []*gen.AdminMcpServer {
	t.Helper()

	res, err := svc.ListProjectMcpServers(ctx, &gen.ListProjectMcpServersPayload{
		AdminSessionToken: nil,
		OrganizationID:    orgID,
		ProjectID:         projectID.String(),
	})
	require.NoError(t, err)

	return res.McpServers
}

func TestListProjectMcpServers_EmptyProject(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestMCPServersService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "empty")

	require.Empty(t, listMCPServers(t, ctx, svc, "org_one", projectID))
}

// The project id is checked against the organization in the request, so an
// operator on one record cannot read another organization's servers.
func TestListProjectMcpServers_ProjectInAnotherOrganizationIsNotFound(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestMCPServersService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_two", name: "Two", slug: "two"})
	projectID := seedProject(t, ctx, conn, "org_two", "theirs")

	_, err := svc.ListProjectMcpServers(ctx, &gen.ListProjectMcpServersPayload{
		AdminSessionToken: nil,
		OrganizationID:    "org_one",
		ProjectID:         projectID.String(),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

// Both server models, deduplicated the way the project count is: a toolset
// with an mcp_servers row appears once, as that row.
func TestListProjectMcpServers_ListsBothModelsOnce(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestMCPServersService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "mixed")
	migrated := seedToolset(t, ctx, conn, "org_one", projectID, "migrated", true)
	serverID := seedMCPServer(t, ctx, conn, projectID, migrated, "migrated-server")
	seedMCPEndpoint(t, ctx, conn, projectID, serverID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "one-migrated")
	legacyID := seedToolset(t, ctx, conn, "org_one", projectID, "legacy", true)
	seedToolset(t, ctx, conn, "org_one", projectID, "not-served", false)

	got := listMCPServers(t, ctx, svc, "org_one", projectID)
	require.Len(t, got, 2)

	require.Equal(t, serverID.String(), got[0].ID)
	require.Equal(t, "migrated-server", got[0].Name)
	require.Equal(t, "toolset", got[0].Source)
	require.Equal(t, "disabled", got[0].Visibility)
	require.NotNil(t, got[0].URL)
	require.Equal(t, "https://gram.example.com/mcp/one-migrated", *got[0].URL)

	require.Equal(t, legacyID.String(), got[1].ID)
	require.Equal(t, "legacy", got[1].Name)
	require.Equal(t, "toolset_only", got[1].Source)
	require.Equal(t, "private", got[1].Visibility)
	require.NotNil(t, got[1].URL)
	require.Equal(t, "https://gram.example.com/mcp/legacy", *got[1].URL)
}

// A custom-domain endpoint is the address the dashboard shows, so it wins over
// the platform one, and the server is still listed once.
func TestListProjectMcpServers_PrefersCustomDomainEndpoint(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestMCPServersService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "domains")
	backing := seedToolset(t, ctx, conn, "org_one", projectID, "backing", false)
	serverID := seedMCPServer(t, ctx, conn, projectID, backing, "served")
	domainID := seedCustomDomain(t, ctx, conn, "org_one", "mcp.example.org", true)
	seedMCPEndpoint(t, ctx, conn, projectID, serverID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "one-served")
	seedMCPEndpoint(t, ctx, conn, projectID, serverID, uuid.NullUUID{UUID: domainID, Valid: true}, "served")

	got := listMCPServers(t, ctx, svc, "org_one", projectID)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].URL)
	require.Equal(t, "https://mcp.example.org/mcp/served", *got[0].URL)
}

// A domain that is not verified and activated serves nothing, so the server
// falls back to its platform endpoint.
func TestListProjectMcpServers_UnaddressableCustomDomainFallsBackToPlatform(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestMCPServersService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "pending-domain")
	backing := seedToolset(t, ctx, conn, "org_one", projectID, "backing", false)
	serverID := seedMCPServer(t, ctx, conn, projectID, backing, "served")
	domainID := seedCustomDomain(t, ctx, conn, "org_one", "pending.example.org", false)
	seedMCPEndpoint(t, ctx, conn, projectID, serverID, uuid.NullUUID{UUID: domainID, Valid: true}, "served")

	// Its slug is only reserved on that domain, so it is not shown on the
	// platform origin either.
	got := listMCPServers(t, ctx, svc, "org_one", projectID)
	require.Len(t, got, 1)
	require.Nil(t, got[0].URL)

	seedMCPEndpoint(t, ctx, conn, projectID, serverID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "one-served")

	got = listMCPServers(t, ctx, svc, "org_one", projectID)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].URL)
	require.Equal(t, "https://gram.example.com/mcp/one-served", *got[0].URL)
}

// A domain-root endpoint is served at the bare domain and outranks the other
// endpoints, as it does everywhere else endpoints are resolved.
func TestListProjectMcpServers_DomainRootEndpointIsBareDomain(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestMCPServersService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "root-domain")
	backing := seedToolset(t, ctx, conn, "org_one", projectID, "backing", false)
	serverID := seedMCPServer(t, ctx, conn, projectID, backing, "served")
	domainID := seedCustomDomain(t, ctx, conn, "org_one", "mcp.example.org", true)
	customDomain := uuid.NullUUID{UUID: domainID, Valid: true}
	seedMCPEndpoint(t, ctx, conn, projectID, serverID, customDomain, "served")
	rootID := seedMCPEndpoint(t, ctx, conn, projectID, serverID, customDomain, "root")
	_, err := mcpendpointsRepo.New(conn).UpdateMCPEndpoint(ctx, mcpendpointsRepo.UpdateMCPEndpointParams{
		CustomDomainID:  customDomain,
		McpServerID:     uuid.NullUUID{UUID: serverID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            "root",
		IsDomainRoot:    pgtype.Bool{Bool: true, Valid: true},
		ID:              rootID,
		ProjectID:       projectID,
	})
	require.NoError(t, err)

	got := listMCPServers(t, ctx, svc, "org_one", projectID)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].URL)
	require.Equal(t, "https://mcp.example.org", *got[0].URL)
}

func TestListProjectMcpServers_ServerWithoutEndpointHasNoURL(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestMCPServersService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "no-endpoint")
	backing := seedToolset(t, ctx, conn, "org_one", projectID, "backing", false)
	seedMCPServer(t, ctx, conn, projectID, backing, "unrouted")

	got := listMCPServers(t, ctx, svc, "org_one", projectID)
	require.Len(t, got, 1)
	require.Nil(t, got[0].URL)
}

// Without an mcp_slug a toolset is addressed by project, toolset and default
// environment slugs on the platform origin, even with a custom domain: that
// domain only serves an mcp_slug.
func TestListProjectMcpServers_ToolsetWithoutMCPSlugUsesLegacyPath(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestMCPServersService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "old")
	toolsets := toolsetsRepo.New(conn)
	_, err := toolsets.CreateToolset(ctx, toolsetsRepo.CreateToolsetParams{
		OrganizationID:         "org_one",
		ProjectID:              projectID,
		Name:                   "Old",
		Slug:                   "old-toolset",
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "prod", Valid: true},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	requireLegacyURL := func() {
		t.Helper()
		got := listMCPServers(t, ctx, svc, "org_one", projectID)
		require.Len(t, got, 1)
		require.NotNil(t, got[0].URL)
		require.Equal(t, "https://gram.example.com/mcp/old/old-toolset/prod", *got[0].URL)
	}
	requireLegacyURL()

	domainID := seedCustomDomain(t, ctx, conn, "org_one", "mcp.example.org", true)
	require.NoError(t, toolsets.SetToolsetCustomDomain(ctx, toolsetsRepo.SetToolsetCustomDomainParams{
		CustomDomainID: uuid.NullUUID{UUID: domainID, Valid: true},
		Slug:           "old-toolset",
		ProjectID:      projectID,
	}))
	requireLegacyURL()
}

func TestMcpServerSource(t *testing.T) {
	t.Parallel()

	backend := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	tests := map[string]repo.AdminListProjectMcpServerRowsRow{
		"toolset":   {ToolsetID: backend},
		"remote":    {RemoteMcpServerID: backend},
		"tunneled":  {TunneledMcpServerID: backend},
		"unproxied": {},
	}
	for want, row := range tests {
		require.Equal(t, want, mcpServerSource(row), want)
	}
}

func TestListProjectMcpServers_IgnoresDeletedServers(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestMCPServersService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	projectID := seedProject(t, ctx, conn, "org_one", "deleted")
	backing := seedToolset(t, ctx, conn, "org_one", projectID, "backing", false)
	serverID := seedMCPServer(t, ctx, conn, projectID, backing, "gone")
	deleteMCPServer(t, ctx, conn, projectID, serverID)

	require.Empty(t, listMCPServers(t, ctx, svc, "org_one", projectID))
}
