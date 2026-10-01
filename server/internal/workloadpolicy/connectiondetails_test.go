package workloadpolicy_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// newMCPServer creates a remote-backed MCP server in projectID.
func newMCPServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, name string) mcpservers_repo.McpServer {
	t.Helper()

	remote := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcp_repo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		Name:          conv.ToPGText(name + " upstream"),
		Slug:          conv.ToPGText("upstream-" + uuid.NewString()[:8]),
		TransportType: "streamable-http",
		Url:           "https://upstream.example.com/mcp",
	})
	server, err := mcpservers_repo.New(ti.conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             projectID,
		Name:                  conv.ToPGText(name),
		Slug:                  conv.ToPGText("server-" + uuid.NewString()[:8]),
		EnvironmentID:         uuid.NullUUID{},
		UserSessionIssuerID:   uuid.NullUUID{},
		RemoteMcpServerID:     uuid.NullUUID{UUID: remote.ID, Valid: true},
		TunneledMcpServerID:   uuid.NullUUID{},
		ToolsetID:             uuid.NullUUID{},
		UnproxiedMcpServerID:  uuid.NullUUID{},
		ToolVariationsGroupID: uuid.NullUUID{},
		Visibility:            "private",
		NetworkAccessMode:     conv.ToPGText("public_only"),
	})
	require.NoError(t, err)
	return server
}

// newProject creates a second project in the caller's organization.
func newProject(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
	t.Helper()

	slug := "other-" + uuid.NewString()[:8]
	project, err := projects_repo.New(ti.conn).CreateProject(ctx, projects_repo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	return project.ID
}

// newToolsetBackedMCPServer creates an MCP server in projectID backed by a new
// toolset, under an id of its own.
func newToolsetBackedMCPServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, name string) mcpservers_repo.McpServer {
	t.Helper()

	toolset, err := toolsets_repo.New(ti.conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         ti.orgID,
		ProjectID:              projectID,
		Name:                   name + " toolset",
		Slug:                   "toolset-" + uuid.NewString()[:8],
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                pgtype.Text{},
		McpEnabled:             false,
	})
	require.NoError(t, err)
	server, err := mcpservers_repo.New(ti.conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             projectID,
		Name:                  conv.ToPGText(name),
		Slug:                  conv.ToPGText("server-" + uuid.NewString()[:8]),
		EnvironmentID:         uuid.NullUUID{},
		UserSessionIssuerID:   uuid.NullUUID{},
		RemoteMcpServerID:     uuid.NullUUID{},
		TunneledMcpServerID:   uuid.NullUUID{},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		UnproxiedMcpServerID:  uuid.NullUUID{},
		ToolVariationsGroupID: uuid.NullUUID{},
		Visibility:            "private",
		NetworkAccessMode:     conv.ToPGText("public_only"),
	})
	require.NoError(t, err)
	require.NotEqual(t, toolset.ID, server.ID)
	return server
}

// withConnectionReadGrants gives the caller workload:read on the organization
// and mcp:read on server, the two grants reading its connection details takes.
func withConnectionReadGrants(t *testing.T, ctx context.Context, ti *testInstance, server mcpservers_repo.McpServer) context.Context {
	t.Helper()

	return authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeWorkloadRead, ti.orgID),
		authz.NewGrant(authz.ScopeMCPRead, server.ID.String()),
	)
}

func connectionDetails(ctx context.Context, ti *testInstance, serverID uuid.UUID) (*gen.WorkloadConnectionDetails, error) {
	return ti.service.ConnectionDetails(ctx, &gen.ConnectionDetailsPayload{ //nolint:wrapcheck // tests assert on the handler's own error
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      serverID.String(),
	})
}

func TestConnectionDetails_RendersEachEndpointTheResolverReturns(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	server := newMCPServer(t, ctx, ti, ti.projectID, "Payments")
	ti.federation.endpoints = []mcp.FederationEndpoint{
		{
			ResourceURL:             "https://app.example.com/mcp/payments",
			Issuer:                  "https://auth.example.com/mcp/payments",
			TokenEndpoint:           "https://auth.example.com/mcp/payments/token",
			OnAuthenticationHost:    true,
			GrantTypesSupported:     []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:jwt-bearer"},
			WorkloadGrantAdvertised: true,
			NotReady:                mcp.FederationReady,
		},
		{
			ResourceURL:             "https://mcp.customer.example/mcp/payments",
			Issuer:                  "https://mcp.customer.example/mcp/payments",
			TokenEndpoint:           "https://mcp.customer.example/mcp/payments/token",
			OnAuthenticationHost:    false,
			GrantTypesSupported:     []string{"authorization_code", "refresh_token"},
			WorkloadGrantAdvertised: false,
			NotReady:                mcp.FederationWorkloadGrantUnavailable,
		},
	}

	details, err := connectionDetails(withConnectionReadGrants(t, withoutProject(t, ctx), ti, server), ti, server.ID)
	require.NoError(t, err)

	require.Equal(t, server.ID.String(), details.McpServerID)
	require.Equal(t, "Payments", details.McpServerName)
	require.Equal(t, []uuid.UUID{server.ID}, ti.federation.resolved)
	require.Len(t, details.Endpoints, 2)

	ready := details.Endpoints[0]
	require.Equal(t, "https://app.example.com/mcp/payments", ready.ResourceURL)
	require.Equal(t, "app.example.com", ready.APIHost)
	require.Equal(t, "https://auth.example.com/mcp/payments", ready.Issuer)
	require.Equal(t, "https://auth.example.com/mcp/payments/token", ready.TokenEndpoint)
	require.True(t, ready.OnAuthenticationHost)
	require.True(t, ready.WorkloadGrantAdvertised)
	require.Equal(t, []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:jwt-bearer"}, ready.GrantTypesSupported)
	require.True(t, ready.Ready)
	require.Nil(t, ready.NotReadyReason)

	notReady := details.Endpoints[1]
	require.Equal(t, "mcp.customer.example", notReady.APIHost)
	require.False(t, notReady.OnAuthenticationHost)
	require.False(t, notReady.WorkloadGrantAdvertised)
	require.Equal(t, []string{"authorization_code", "refresh_token"}, notReady.GrantTypesSupported)
	require.False(t, notReady.Ready)
	require.NotNil(t, notReady.NotReadyReason)
	require.Equal(t, string(mcp.FederationWorkloadGrantUnavailable), *notReady.NotReadyReason)
}

func TestConnectionDetails_ReachesServersInAnyProjectWithoutAProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	server := newMCPServer(t, ctx, ti, newProject(t, ctx, ti), "Elsewhere")
	ti.federation.endpoints = []mcp.FederationEndpoint{{
		ResourceURL:             "https://app.example.com/mcp/elsewhere",
		Issuer:                  "",
		TokenEndpoint:           "",
		OnAuthenticationHost:    false,
		GrantTypesSupported:     []string{},
		WorkloadGrantAdvertised: false,
		NotReady:                mcp.FederationNoAuthorizationServer,
	}}

	details, err := connectionDetails(withConnectionReadGrants(t, withoutProject(t, ctx), ti, server), ti, server.ID)
	require.NoError(t, err)
	require.Equal(t, server.ID.String(), details.McpServerID)
	require.Equal(t, []uuid.UUID{server.ID}, ti.federation.resolved)
	require.Len(t, details.Endpoints, 1)
	require.Equal(t, "https://app.example.com/mcp/elsewhere", details.Endpoints[0].ResourceURL)
}

func TestConnectionDetails_APIKeyReachesOnlyItsProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	inProject := newMCPServer(t, ctx, ti, ti.projectID, "Here")
	elsewhere := newMCPServer(t, ctx, ti, newProject(t, ctx, ti), "Elsewhere")

	_, err := connectionDetails(asAPIKey(t, ctx), ti, inProject.ID)
	require.NoError(t, err)

	_, err = connectionDetails(asAPIKey(t, ctx), ti, elsewhere.ID)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestConnectionDetails_RequiresWorkloadRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	server := newMCPServer(t, ctx, ti, ti.projectID, "Payments")

	orgCtx := authztest.WithExactGrants(t, withoutProject(t, ctx), authz.NewGrant(authz.ScopeMCPRead, server.ID.String()))
	_, err := connectionDetails(orgCtx, ti, server.ID)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, ti.federation.resolved)
}

func TestConnectionDetails_RequiresMCPReadOnTheServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	server := newMCPServer(t, ctx, ti, ti.projectID, "Payments")
	other := newMCPServer(t, ctx, ti, ti.projectID, "Other")

	orgCtx := withConnectionReadGrants(t, withoutProject(t, ctx), ti, other)
	_, err := connectionDetails(orgCtx, ti, server.ID)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, ti.federation.resolved)
}

func TestConnectionDetails_UnknownServerIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := connectionDetails(withoutProject(t, ctx), ti, uuid.New())
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A toolset-backed server's mcp:read grant is written against its toolset, so
// a grant naming the toolset reads its connection details and one naming only
// the server's own id does not.
func TestConnectionDetails_ToolsetBackedServerReadsTheToolsetGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	server := newToolsetBackedMCPServer(t, ctx, ti, ti.projectID, "Hosted")

	onToolset := authztest.WithExactGrants(t, withoutProject(t, ctx),
		authz.NewGrant(authz.ScopeWorkloadRead, ti.orgID),
		authz.NewGrant(authz.ScopeMCPRead, server.ToolsetID.UUID.String()),
	)
	details, err := connectionDetails(onToolset, ti, server.ID)
	require.NoError(t, err)
	require.Equal(t, server.ID.String(), details.McpServerID)
	require.Equal(t, []uuid.UUID{server.ID}, ti.federation.resolved)

	onServer := withConnectionReadGrants(t, withoutProject(t, ctx), ti, server)
	_, err = connectionDetails(onServer, ti, server.ID)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, []uuid.UUID{server.ID}, ti.federation.resolved)
}
