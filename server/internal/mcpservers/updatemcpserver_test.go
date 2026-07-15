package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "original name",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		Name:              nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
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
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "before name",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.Slug)
	originalSlug := *created.Slug

	newName := "after name"
	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		Name:              &newName,
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
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
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "original name",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	emptyName := "   "
	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		Name:              &emptyName,
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestUpdateMcpServer_PreservesUserSessionIssuer: the issuer is attached at
// create time for the server's lifetime. The update payload carries no issuer
// field, and a full-record-replace update must leave the stored issuer intact
// (the query COALESCEs a NULL input to the existing value).
func TestUpdateMcpServer_PreservesUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "perpetual issuer",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.UserSessionIssuerID)
	issuerID := *created.UserSessionIssuerID

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		Name:              nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.UserSessionIssuerID)
	require.Equal(t, issuerID, *updated.UserSessionIssuerID)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, issuerID, beforeSnapshot["UserSessionIssuerID"])

	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, issuerID, afterSnapshot["UserSessionIssuerID"])
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

// TestUpdateMcpServer_TunneledMcpPublicAllowedWithConsent: flipping a private
// tunneled server to public succeeds once the source owner has consented.
func TestUpdateMcpServer_TunneledMcpPublicAllowedWithConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tunneledServerID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	tunneledServerIDStr := tunneledServerID.String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "private tunneled mcp server pending consent",
		EnvironmentID:       nil,
		TunneledMcpServerID: &tunneledServerIDStr,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	enableTunneledPublicConsent(t, ctx, ti.conn, *authCtx.ProjectID, tunneledServerID)

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                nil,
		EnvironmentID:       nil,
		TunneledMcpServerID: &tunneledServerIDStr,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("public"), updated.Visibility)
}
