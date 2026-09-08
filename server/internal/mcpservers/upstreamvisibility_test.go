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
// The API cannot yet produce a servable upstream server: nothing outside tests
// writes remote_session_issuer_id (AIM-27 adds the management surface, and the
// resync that derives it cannot match a row whose user_session_issuer_id is
// NULL, which upstream requires). So every write below is rejected, and that is
// the point — the alternative is accepting a row the runtime answers 404 for,
// with only a log line to explain why a working server went dark.

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

func TestCreateMcpServer_UpstreamRejectedWithoutAnIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetID := seedToolsetBackend(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).String()

	// The backend is right and no user session issuer is written, so the only
	// unsatisfied rule is the issuer the server would advertise.
	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             "upstream on toolset",
		EnvironmentID:    nil,
		ToolsetID:        &toolsetID,
		Visibility:       types.McpServerVisibility("upstream"),
	})
	// oops redacts the cause, so the reason is asserted in the unit test on
	// verifyUpstreamAuthorization; here the code is what the API contract owns.
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpServer_UpstreamRejectedWithoutAnIssuer(t *testing.T) {
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

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		EnvironmentID:    nil,
		ToolsetID:        &toolsetID,
		Visibility:       types.McpServerVisibility("upstream"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)

	// The rejected write must leave the server serving as it was, not half
	// applied: this is an operator flipping a live server, and the failure mode
	// the rule exists to prevent is exactly "it went dark".
	after, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               &created.ID,
		Slug:             nil,
	})
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("public"), after.Visibility)
}

// The update path checks the post-update row rather than the payload, because
// the query COALESCEs unset references, so a single request that both switches
// the backend and sets upstream must be judged on where it lands.
//
// Starting from a public remote-backed server rather than an upstream one: an
// upstream server is not constructible through the API today, and the point
// here is the update-path check, not the starting state.
func TestUpdateMcpServer_UpstreamRejectsRemoteBackendOnTheSameRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "remote server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)

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

	after, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               &created.ID,
		Slug:             nil,
	})
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("public"), after.Visibility, "a rejected write must leave the server serving as it was")
}
