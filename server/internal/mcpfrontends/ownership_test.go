package mcpfrontends_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_frontends"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedOtherProjectRemoteMcpServer creates an additional project in the caller's
// organization and inserts a remote MCP server under that *other* project.
// Used to exercise cross-tenant ownership rejection.
func seedOtherProjectRemoteMcpServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	slug := "other-" + uuid.New().String()[:8]
	otherProject, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	server, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ProjectID:     otherProject.ID,
		TransportType: "streamable-http",
		Url:           "https://other.example.com/mcp/" + uuid.NewString(),
	})
	require.NoError(t, err)

	return server.ID
}

// seedOtherProjectToolset creates an additional project in the caller's
// organization and inserts a toolset under that *other* project.
// Used to exercise cross-tenant ownership rejection on toolset_id.
func seedOtherProjectToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	slug := "other-" + uuid.New().String()[:8]
	otherProject, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         organizationID,
		ProjectID:              otherProject.ID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	return toolset.ID
}

func TestCreateMcpFrontend_RejectsCrossTenantToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Toolset lives in a different project in the same org.
	otherToolsetID := seedOtherProjectToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID).String()

	_, err := ti.service.CreateMcpFrontend(ctx, &gen.CreateMcpFrontendPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		EnvironmentID:         nil,
		ExternalOauthServerID: nil,
		OauthProxyServerID:    nil,
		RemoteMcpServerID:     nil,
		ToolsetID:             &otherToolsetID,
		Visibility:            types.McpFrontendVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpFrontend_RejectsCrossTenantToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Start with a valid frontend pointing at a same-project remote MCP server.
	ownServerID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpFrontend(ctx, &gen.CreateMcpFrontendPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		EnvironmentID:         nil,
		ExternalOauthServerID: nil,
		OauthProxyServerID:    nil,
		RemoteMcpServerID:     &ownServerID,
		ToolsetID:             nil,
		Visibility:            types.McpFrontendVisibility("disabled"),
	})
	require.NoError(t, err)

	// Attempt to swap the backend to a toolset in another project.
	otherToolsetID := seedOtherProjectToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID).String()

	_, err = ti.service.UpdateMcpFrontend(ctx, &gen.UpdateMcpFrontendPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    created.ID,
		EnvironmentID:         nil,
		ExternalOauthServerID: nil,
		OauthProxyServerID:    nil,
		RemoteMcpServerID:     nil,
		ToolsetID:             &otherToolsetID,
		Visibility:            types.McpFrontendVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpFrontend_RejectsCrossTenantRemoteMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Remote MCP server lives in a different project in the same org.
	otherServerID := seedOtherProjectRemoteMcpServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID).String()

	_, err := ti.service.CreateMcpFrontend(ctx, &gen.CreateMcpFrontendPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		EnvironmentID:         nil,
		ExternalOauthServerID: nil,
		OauthProxyServerID:    nil,
		RemoteMcpServerID:     &otherServerID,
		ToolsetID:             nil,
		Visibility:            types.McpFrontendVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpFrontend_RejectsCrossTenantRemoteMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Start with a valid frontend in the caller's own project.
	ownServerID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpFrontend(ctx, &gen.CreateMcpFrontendPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		EnvironmentID:         nil,
		ExternalOauthServerID: nil,
		OauthProxyServerID:    nil,
		RemoteMcpServerID:     &ownServerID,
		ToolsetID:             nil,
		Visibility:            types.McpFrontendVisibility("disabled"),
	})
	require.NoError(t, err)

	// Attempt to update the backend to point at a server in another project.
	otherServerID := seedOtherProjectRemoteMcpServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID).String()

	_, err = ti.service.UpdateMcpFrontend(ctx, &gen.UpdateMcpFrontendPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    created.ID,
		EnvironmentID:         nil,
		ExternalOauthServerID: nil,
		OauthProxyServerID:    nil,
		RemoteMcpServerID:     &otherServerID,
		ToolsetID:             nil,
		Visibility:            types.McpFrontendVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}
