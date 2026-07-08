package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestUpdateMcpServer_FullReplace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverA := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	serverB := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "test mcp server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverA,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)

	// Full-record replace: swap backend to serverB, flip visibility, drop
	// any optional fields.
	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverB,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, updated.ID)
	require.NotNil(t, updated.RemoteMcpServerID)
	require.Equal(t, serverB, *updated.RemoteMcpServerID)
	require.Equal(t, types.McpServerVisibility("public"), updated.Visibility)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.NotNil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)
}

func TestUpdateMcpServer_InvalidBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "test mcp server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	// Update with neither backend — should fail validation.
	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		EnvironmentID:     nil,
		RemoteMcpServerID: nil,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpServer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                uuid.NewString(),
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateMcpServer_LeavesNameWhenOmitted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "original name",
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                nil,
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Name)
	require.Equal(t, "original name", *updated.Name)
}

func TestUpdateMcpServer_TunneledMcpRejectsPublicVisibility(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tunneledServerID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "private tunneled mcp server",
		EnvironmentID:       nil,
		TunneledMcpServerID: &tunneledServerID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                nil,
		EnvironmentID:       nil,
		TunneledMcpServerID: &tunneledServerID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("public"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpServer_RecomputesSlugOnNameChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "before name",
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.Slug)
	originalSlug := *created.Slug

	newName := "after name"
	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                &newName,
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Name)
	require.Equal(t, newName, *updated.Name)
	require.NotNil(t, updated.Slug)
	require.NotEqual(t, originalSlug, *updated.Slug, "slug should recompute when name changes")
}

func TestUpdateMcpServer_RejectsEmptyName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "original name",
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	emptyName := "   "
	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                &emptyName,
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateMcpServer_SetUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "set issuer",
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Nil(t, created.UserSessionIssuerID)

	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                nil,
		EnvironmentID:       nil,
		UserSessionIssuerID: &issuerID,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.UserSessionIssuerID)
	require.Equal(t, issuerID, *updated.UserSessionIssuerID)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Nil(t, beforeSnapshot["UserSessionIssuerID"])

	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, issuerID, afterSnapshot["UserSessionIssuerID"])
}

func TestUpdateMcpServer_ClearUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "clear issuer",
		EnvironmentID:       nil,
		UserSessionIssuerID: &issuerID,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.UserSessionIssuerID)

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                nil,
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Nil(t, updated.UserSessionIssuerID)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, issuerID, beforeSnapshot["UserSessionIssuerID"])

	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Nil(t, afterSnapshot["UserSessionIssuerID"])
}

func TestUpdateMcpServer_InvalidUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "bogus issuer",
		EnvironmentID:       nil,
		UserSessionIssuerID: nil,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	bogusIssuerID := uuid.NewString()

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                nil,
		EnvironmentID:       nil,
		UserSessionIssuerID: &bogusIssuerID,
		RemoteMcpServerID:   &serverID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpServer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                uuid.NewString(),
		EnvironmentID:     nil,
		RemoteMcpServerID: nil,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// seedEndpointFor inserts an mcp_endpoints row directly through the generated
// repo so attach-on-enable tests control endpoint existence without going
// through the mcpendpoints service (whose create path runs its own
// default-plugin attach).
func seedEndpointFor(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, mcpServerID string) {
	t.Helper()

	_, err := mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    uuid.MustParse(mcpServerID),
		Slug:           "attach-test-" + uuid.NewString(),
	})
	require.NoError(t, err)
}

// createDisabledRemoteServer provisions a remote-backed mcp_server through the
// service in the "disabled" state the dashboard's remote MCP create flow
// leaves servers in before auth is configured.
func createDisabledRemoteServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, name string) (*types.McpServer, string) {
	t.Helper()

	remoteServerID := seedRemoteMcpServer(t, ctx, ti.conn, projectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              name,
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteServerID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	return created, remoteServerID
}

func TestUpdateMcpServer_EnableAttachesToDefaultPlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	pluginsQueries := pluginsrepo.New(ti.conn)
	defaultPlugin, err := pluginsQueries.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	// The dashboard's remote MCP flow: server created disabled, endpoint
	// pre-staged while it's still disabled (so no attach fires there).
	created, remoteServerID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "Attach On Enable Server")
	seedEndpointFor(t, ctx, ti.conn, *authCtx.ProjectID, created.ID)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)

	// Enabling the server is what completes publishability — it must attach.
	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		Name:              nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteServerID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, created.ID, servers[0].McpServerID.UUID.String())
	require.Equal(t, "Attach On Enable Server", servers[0].DisplayName)
	require.Equal(t, "required", servers[0].Policy)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestUpdateMcpServer_EnableWithoutEndpointDoesNotAttach(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	pluginsQueries := pluginsrepo.New(ti.conn)
	defaultPlugin, err := pluginsQueries.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	created, remoteServerID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "No Endpoint Server")

	// Enabled but endpointless — not publishable, so no attach (matching
	// AddPluginServer's own rejection of endpointless servers).
	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		Name:              nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteServerID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Empty(t, servers)
}

func TestUpdateMcpServer_EnableLazilyCreatesDefaultPluginWhenMissing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created, remoteServerID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "Lazy Plugin Server")
	seedEndpointFor(t, ctx, ti.conn, *authCtx.ProjectID, created.ID)

	pluginsQueries := pluginsrepo.New(ti.conn)
	_, err := pluginsQueries.GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "fixture project (created directly via projectsrepo) has no Default plugin yet")

	beforeCreateCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginCreate)
	require.NoError(t, err)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		Name:              nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteServerID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	defaultPlugin, err := pluginsQueries.GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, created.ID, servers[0].McpServerID.UUID.String())

	afterCreateCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCreateCount+1, afterCreateCount)
}

func TestUpdateMcpServer_EnableAlreadyAttachedIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	pluginsQueries := pluginsrepo.New(ti.conn)
	defaultPlugin, err := pluginsQueries.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	created, remoteServerID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "Already Attached Server")
	seedEndpointFor(t, ctx, ti.conn, *authCtx.ProjectID, created.ID)

	// Manually attached beforehand (e.g. via the Plugins page while the
	// server was briefly enabled, or by an admin API call).
	_, err = pluginsQueries.AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID:    defaultPlugin.ID,
		ToolsetID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID: uuid.NullUUID{UUID: uuid.MustParse(created.ID), Valid: true},
		DisplayName: "Already Attached Server",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		Name:              nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteServerID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
}

func TestUpdateMcpServer_AlreadyEnabledUpdateDoesNotAttach(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	pluginsQueries := pluginsrepo.New(ti.conn)
	defaultPlugin, err := pluginsQueries.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	remoteServerID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "Already Enabled Server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteServerID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	seedEndpointFor(t, ctx, ti.conn, *authCtx.ProjectID, created.ID)

	// No disabled -> enabled transition here, so the update must not attach;
	// attach-on-first-endpoint (mcpendpoints) owns the already-enabled case.
	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		Name:              nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteServerID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Empty(t, servers)
}

// TestUpdateMcpServer_RecreatedServerReattachesUnderOriginalName is the
// end-to-end regression for the customer flow: attach a server, delete it,
// re-create a same-named replacement, and enable the replacement. Before the
// delete cascade freed the display name, the replacement's attach collided
// with the stale plugin_servers row and failed the enable outright.
func TestUpdateMcpServer_RecreatedServerReattachesUnderOriginalName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	pluginsQueries := pluginsrepo.New(ti.conn)
	defaultPlugin, err := pluginsQueries.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	original, originalRemoteID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "Ashby")
	seedEndpointFor(t, ctx, ti.conn, *authCtx.ProjectID, original.ID)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                original.ID,
		Name:              nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &originalRemoteID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)

	err = ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               original.ID,
	})
	require.NoError(t, err)

	replacement, replacementRemoteID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "Ashby")
	seedEndpointFor(t, ctx, ti.conn, *authCtx.ProjectID, replacement.ID)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                replacement.ID,
		Name:              nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &replacementRemoteID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, replacement.ID, servers[0].McpServerID.UUID.String())
	require.Equal(t, "Ashby", servers[0].DisplayName, "the freed display name must be reused, not suffixed")
}

// TestUpdateMcpServer_EnableAdoptsPreexistingDefaultSlugPlugin is the
// regression for the prod failure where every enable/endpoint-create in a
// project errored with "attach mcp server to default plugin": the project
// had a plugin slugged "default" from before auto-provisioning (is_default
// false), so EnsureDefaultPlugin's create collided with its slug on every
// attach. Enabling must adopt that plugin and attach into it.
func TestUpdateMcpServer_EnableAdoptsPreexistingDefaultSlugPlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	pluginsQueries := pluginsrepo.New(ti.conn)
	preexisting, err := pluginsQueries.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Default",
		Slug:           "default",
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)

	created, remoteServerID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "Adopted Plugin Server")
	seedEndpointFor(t, ctx, ti.conn, *authCtx.ProjectID, created.ID)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		Name:              nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteServerID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)

	servers, err := pluginsQueries.ListPluginServers(ctx, preexisting.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1, "the server must attach into the adopted pre-existing plugin")
	require.Equal(t, created.ID, servers[0].McpServerID.UUID.String())
}
