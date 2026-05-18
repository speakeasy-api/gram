package mcpservers_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListMcpServers_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		RemoteMcpServerID: nil,
		ToolsetID:         nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.McpServers)
}

func TestListMcpServers_Multiple(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverA := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	serverB := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	for _, sid := range []string{serverA, serverB} {
		_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
			SessionToken:      nil,
			ApikeyToken:       nil,
			ProjectSlugInput:  nil,
			EnvironmentID:     nil,
			RemoteMcpServerID: &sid,
			ToolsetID:         nil,
			Visibility:        types.McpServerVisibility("disabled"),
		})
		require.NoError(t, err)
	}

	result, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		RemoteMcpServerID: nil,
		ToolsetID:         nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpServers, 2)
}

func TestListMcpServers_FilterByRemoteMcpServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	wantedRemote := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	otherRemote := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	wanted, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &wantedRemote,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	_, err = ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &otherRemote,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	result, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		RemoteMcpServerID: &wantedRemote,
		ToolsetID:         nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpServers, 1)
	require.Equal(t, wanted.ID, result.McpServers[0].ID)
}

func TestListMcpServers_FilterByRemoteMcpServerID_NoMatches(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	existingRemote := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &existingRemote,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	unrelated := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	result, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		RemoteMcpServerID: &unrelated,
		ToolsetID:         nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.McpServers)
}

func TestListMcpServers_FilterByBothBackends(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	remoteID := "00000000-0000-0000-0000-000000000001"
	toolsetID := "00000000-0000-0000-0000-000000000002"

	_, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		RemoteMcpServerID: &remoteID,
		ToolsetID:         &toolsetID,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestListMcpServers_FilterByMalformedRemoteMcpServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bogus := "not-a-uuid"
	_, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		RemoteMcpServerID: &bogus,
		ToolsetID:         nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListMcpServers_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		RemoteMcpServerID: nil,
		ToolsetID:         nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
