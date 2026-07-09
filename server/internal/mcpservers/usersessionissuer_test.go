package mcpservers_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// TestCreateMcpServer_TunneledAutoProvisionsIssuer: a tunneled server always
// gets an issuer (never anonymous), even when created disabled — the fix for
// the tunnel-create gap.
func TestCreateMcpServer_TunneledAutoProvisionsIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tunID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	res, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:                "tunnel auto issuer",
		TunneledMcpServerID: &tunID,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, res.UserSessionIssuerID, "tunneled server must auto-provision an issuer")
	require.NotEmpty(t, *res.UserSessionIssuerID)
}

// TestCreateMcpServer_PrivateRemoteAutoProvisionsIssuer: a private remote
// server gets an issuer.
func TestCreateMcpServer_PrivateRemoteAutoProvisionsIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	res, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:              "private remote auto issuer",
		RemoteMcpServerID: &remoteID,
		Visibility:        types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.NotNil(t, res.UserSessionIssuerID, "private remote server must auto-provision an issuer")
	require.NotEmpty(t, *res.UserSessionIssuerID)
}

// TestCreateMcpServer_PublicRemoteHasNoIssuer: public remote servers are
// anonymous — no issuer is provisioned.
func TestCreateMcpServer_PublicRemoteHasNoIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	res, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:              "public remote no issuer",
		RemoteMcpServerID: &remoteID,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	require.Nil(t, res.UserSessionIssuerID, "public remote server is anonymous, no issuer")
}

// TestCreateMcpServer_RespectsSuppliedIssuer: an explicitly supplied issuer is
// used as-is; no second one is provisioned.
func TestCreateMcpServer_RespectsSuppliedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// First private remote auto-provisions an issuer we can reuse explicitly.
	firstRemote := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	first, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:              "supplies issuer",
		RemoteMcpServerID: &firstRemote,
		Visibility:        types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.NotNil(t, first.UserSessionIssuerID)
	issuerID := *first.UserSessionIssuerID

	secondRemote := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	second, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:                "reuses supplied issuer",
		RemoteMcpServerID:   &secondRemote,
		UserSessionIssuerID: &issuerID,
		Visibility:          types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.NotNil(t, second.UserSessionIssuerID)
	require.Equal(t, issuerID, *second.UserSessionIssuerID, "supplied issuer must be used as-is")
}

// TestUpdateMcpServer_EnableDisabledRemoteToPrivateProvisions: a disabled
// remote server carries no issuer; enabling it to private provisions one.
func TestUpdateMcpServer_EnableDisabledRemoteToPrivateProvisions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:              "disabled then private",
		RemoteMcpServerID: &remoteID,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Nil(t, created.UserSessionIssuerID, "disabled remote server needs no issuer")

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID:                created.ID,
		RemoteMcpServerID: &remoteID,
		Visibility:        types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.UserSessionIssuerID, "enabling to private must provision an issuer")
	require.NotEmpty(t, *updated.UserSessionIssuerID)
}

// TestUpdateMcpServer_PreservesIssuerOnOmit: an update that omits the issuer
// (the full-record-replace footgun) keeps the existing one — it is neither
// nulled nor replaced with a fresh row.
func TestUpdateMcpServer_PreservesIssuerOnOmit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:              "preserve on omit",
		RemoteMcpServerID: &remoteID,
		Visibility:        types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.UserSessionIssuerID)
	original := *created.UserSessionIssuerID

	newName := "preserve on omit renamed"
	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID:                created.ID,
		Name:              &newName,
		RemoteMcpServerID: &remoteID,
		Visibility:        types.McpServerVisibility("private"),
		// UserSessionIssuerID intentionally omitted.
	})
	require.NoError(t, err)
	require.NotNil(t, updated.UserSessionIssuerID, "omitting the issuer must not null it")
	require.Equal(t, original, *updated.UserSessionIssuerID, "omitting the issuer must not replace it")
}
