package metamcp_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/stretchr/testify/require"
)

func TestGatewayDiscoverySettingsAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	gateway, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{Name: "Discovery gateway"})
	require.NoError(t, err)
	require.Equal(t, "progressive", gateway.DiscoveryMode)
	gateway, err = ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{ID: gateway.ID, Name: gateway.Name, DiscoveryMode: conv.PtrEmpty("direct")})
	require.NoError(t, err)
	require.Equal(t, "direct", gateway.DiscoveryMode)
	event, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMetaMcpServerUpdate)
	require.NoError(t, err)
	var after map[string]any
	require.NoError(t, json.Unmarshal(event.AfterSnapshot, &after))
	require.Equal(t, "direct", after["DiscoveryMode"])
	gateway, err = ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{ID: gateway.ID, Name: "Renamed gateway"})
	require.NoError(t, err)
	require.Equal(t, "direct", gateway.DiscoveryMode, "unrelated writes preserve the mode")
	_, err = ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{ID: gateway.ID, Name: gateway.Name, DiscoveryMode: conv.PtrEmpty("unsupported")})
	require.Error(t, err)
}

func TestGatewayDiscoverySettingsFollowGatewayWrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	gateway, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{Name: "Project editor gateway"})
	require.NoError(t, err)
	ctx = withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, authCtx.ProjectID.String()), authz.NewGrant(authz.ScopeMCPWrite, authCtx.ProjectID.String()))
	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{ID: gateway.ID, Name: gateway.Name, DiscoveryMode: conv.PtrEmpty("direct")})
	require.NoError(t, err)
	require.Equal(t, "direct", updated.DiscoveryMode)
}

func TestGatewayCodeModeSelectionGatePreservesStoredMode(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	gateway, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{Name: "Python gateway"})
	require.NoError(t, err)
	require.False(t, *gateway.CodeModeEnabled)
	update := &gen.UpdateMetaMcpServerPayload{ID: gateway.ID, Name: gateway.Name, DiscoveryMode: conv.PtrEmpty("code_mode")}
	_, err = ti.service.UpdateMetaMcpServer(ctx, update)
	require.ErrorContains(t, err, "not available")
	ti.codeModeEnabled.Store(true)
	gateway, err = ti.service.UpdateMetaMcpServer(ctx, update)
	require.NoError(t, err)
	require.Equal(t, "code_mode", gateway.DiscoveryMode)
	require.True(t, *gateway.CodeModeEnabled)
	ti.codeModeEnabled.Store(false)
	_, err = ti.service.UpdateMetaMcpServer(ctx, update)
	require.NoError(t, err, "unchanged stored choice is not new issuance")
	gateway, err = ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{ID: gateway.ID, Name: "Renamed Python gateway"})
	require.NoError(t, err)
	require.Equal(t, "code_mode", gateway.DiscoveryMode)
	require.False(t, *gateway.CodeModeEnabled)
	listed, err := ti.service.ListMetaMcpServers(ctx, &gen.ListMetaMcpServersPayload{})
	require.NoError(t, err)
	require.Len(t, listed.MetaMcpServers, 1)
	require.Equal(t, "code_mode", listed.MetaMcpServers[0].DiscoveryMode)
	require.False(t, *listed.MetaMcpServers[0].CodeModeEnabled)
	update.DiscoveryMode = conv.PtrEmpty("direct")
	_, err = ti.service.UpdateMetaMcpServer(ctx, update)
	require.NoError(t, err, "an unavailable runtime must allow leaving code mode")
}

// TestGatewayCodeModeRequiresSignIn pins that Code Mode is never offered to a
// gateway that allows anonymous callers: an issuer-less row reports Code Mode
// unavailable, and selecting Code Mode leaves the gateway requiring sign-in.
func TestGatewayCodeModeRequiresSignIn(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.codeModeEnabled.Store(true)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	// A gateway without an issuer can no longer be created through the API.
	row, err := repo.New(ti.conn).CreateMetaMCPServer(ctx, repo.CreateMetaMCPServerParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, Name: "Anonymous gateway", UserSessionIssuerID: uuid.NullUUID{}, Visibility: "private", NetworkAccessMode: pgtype.Text{}})
	require.NoError(t, err)
	gateway := &gen.GetMetaMcpServerPayload{ID: row.ID.String()}
	anonymous, err := ti.service.GetMetaMcpServer(ctx, gateway)
	require.NoError(t, err)
	require.Nil(t, anonymous.UserSessionIssuerID)
	require.False(t, *anonymous.CodeModeEnabled, "a gateway without sign-in cannot select Code Mode")
	listed, err := ti.service.ListMetaMcpServers(ctx, &gen.ListMetaMcpServersPayload{})
	require.NoError(t, err)
	require.Len(t, listed.MetaMcpServers, 1)
	require.False(t, *listed.MetaMcpServers[0].CodeModeEnabled)

	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{ID: gateway.ID, Name: anonymous.Name, DiscoveryMode: conv.PtrEmpty("code_mode")})
	require.NoError(t, err)
	require.Equal(t, "code_mode", updated.DiscoveryMode)
	require.NotNil(t, updated.UserSessionIssuerID, "a Code Mode gateway always requires sign-in")
	require.True(t, *updated.CodeModeEnabled)
}
