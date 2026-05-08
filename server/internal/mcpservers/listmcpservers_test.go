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
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
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
			SessionToken:          nil,
			ApikeyToken:           nil,
			ProjectSlugInput:      nil,
			EnvironmentID:         nil,
			ExternalOauthServerID: nil,
			OauthProxyServerID:    nil,
			RemoteMcpServerID:     &sid,
			ToolsetID:             nil,
			Visibility:            types.McpServerVisibility("disabled"),
		})
		require.NoError(t, err)
	}

	result, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpServers, 2)
}

func TestListMcpServers_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
