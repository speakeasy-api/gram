package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

func TestUpdateMcpServer_PrivateDirectRemoteCannotBecomePublic(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, remoteID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "Private direct remote")
	_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	flags := seedBlockedDirectRemoteDistribution(t, ctx, ti, uuid.MustParse(created.ID))
	flags.SetFlag(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, authCtx.ActiveOrganizationID, false)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("public"),
	})
	require.ErrorIs(t, err, admission.ErrApprovalRequired)
	requireOopsCode(t, err, oops.CodeConflict)
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID: uuid.MustParse(created.ID), ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, "private", server.Visibility)
}

func TestUpdateMcpServer_DisabledDirectRemoteCannotBecomePublic(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, remoteID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "Disabled direct remote")
	flags := seedBlockedDirectRemoteDistribution(t, ctx, ti, uuid.MustParse(created.ID))
	flags.SetFlag(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, authCtx.ActiveOrganizationID, false)

	_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("public"),
	})
	require.ErrorIs(t, err, admission.ErrApprovalRequired)
	requireOopsCode(t, err, oops.CodeConflict)
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID: uuid.MustParse(created.ID), ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, "disabled", server.Visibility)
}

func TestUpdateMcpServer_PublicVisibilityAdmissionUnavailableReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, remoteID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "Unavailable direct remote")
	flags := seedBlockedDirectRemoteDistribution(t, ctx, ti, uuid.MustParse(created.ID))
	flags.SetFlag(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, authCtx.ActiveOrganizationID, false)
	flags.SetFlagPayload(feature.FlagPlatformMCPShadowAudienceEnforcement, authCtx.ActiveOrganizationID, []byte(`{"mode":"unknown"}`))

	_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("public"),
	})

	require.ErrorIs(t, err, admission.ErrUnavailable)
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestUpdateMcpServer_PublicDirectRemoteCanNarrowVisibility(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, remoteID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "Public direct remote")
	_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	seedBlockedDirectRemoteDistribution(t, ctx, ti, uuid.MustParse(created.ID))

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("private"), updated.Visibility)
	updated, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("disabled"), updated.Visibility)
}
