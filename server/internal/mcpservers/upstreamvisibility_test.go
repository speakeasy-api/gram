package mcpservers_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// visibility = 'upstream' forwards the inbound bearer to the upstream as a
// token input, which only the hosted (toolset) serve path does. The management
// API refuses the other backends rather than leaving them to fail closed at
// dispatch, where the operator would see a 500 with no explanation.
//
// These call the service directly, so they reach the check without going
// through the design enum — which deliberately does not accept 'upstream' yet.

func TestCreateMcpServer_UpstreamRejectsRemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "upstream on remote",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("upstream"),
	})
	// oops redacts the cause, so the reason is asserted in the unit test on
	// verifyUpstreamAuthorization; here the code is what the API contract owns.
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpServer_UpstreamRejectsTunneledBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tunneledID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	// Grant the anonymous-serving consent a public tunnel would need, so the
	// rejection can only be coming from the upstream rule.
	enableTunneledPublicConsent(t, ctx, ti.conn, *authCtx.ProjectID, tunneledID)
	tunneled := tunneledID.String()

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "upstream on tunnel",
		EnvironmentID:       nil,
		TunneledMcpServerID: &tunneled,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("upstream"),
	})
	// oops redacts the cause, so the reason is asserted in the unit test on
	// verifyUpstreamAuthorization; here the code is what the API contract owns.
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpServer_UpstreamAllowsToolsetBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetID := seedToolsetBackend(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             "upstream on toolset",
		EnvironmentID:    nil,
		ToolsetID:        &toolsetID,
		Visibility:       types.McpServerVisibility("upstream"),
	})
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("upstream"), created.Visibility)
	// Create never writes a user session issuer, and the upstream rule depends
	// on that staying true: a non-NULL value here would let
	// ResyncMCPServerRemoteSessionIssuers reassign the server's issuer.
	require.Nil(t, created.UserSessionIssuerID)
}

// Repointing an upstream server onto a proxied backend is the same violation
// arriving by a different route, and the update path checks the post-update row
// because the query COALESCEs unset references.
func TestUpdateMcpServer_UpstreamRejectsBackendSwitchToRemote(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetID := seedToolsetBackend(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             "upstream on toolset",
		EnvironmentID:    nil,
		ToolsetID:        &toolsetID,
		Visibility:       types.McpServerVisibility("upstream"),
	})
	require.NoError(t, err)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("upstream"),
	})
	// oops redacts the cause, so the reason is asserted in the unit test on
	// verifyUpstreamAuthorization; here the code is what the API contract owns.
	requireOopsCode(t, err, oops.CodeInvalid)
}

// A toolset-backed server may be moved into and back out of upstream freely;
// the rule constrains the backend and the issuer, not the transition.
func TestUpdateMcpServer_UpstreamOnToolsetBackendRoundTrips(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetID := seedToolsetBackend(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             "toolset server",
		EnvironmentID:    nil,
		ToolsetID:        &toolsetID,
		Visibility:       types.McpServerVisibility("public"),
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		EnvironmentID:    nil,
		ToolsetID:        &toolsetID,
		Visibility:       types.McpServerVisibility("upstream"),
	})
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("upstream"), updated.Visibility)

	reverted, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		EnvironmentID:    nil,
		ToolsetID:        &toolsetID,
		Visibility:       types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("public"), reverted.Visibility)
}
