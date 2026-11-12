package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

func TestSnowflakePendingSourceCannotBeCreatedEnabled(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	source := remotemcptest.SeedServer(t, ctx, ti.conn, remoterepo.CreateServerParams{
		ProjectID: *auth.ProjectID, TransportType: "streamable-http",
		Url: "https://app.snowflake.com/speakeasy-mcp-setup",
	})
	sourceID := source.ID.String()
	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name: "Snowflake", RemoteMcpServerID: &sourceID, Visibility: types.McpServerVisibility("private"),
	})
	require.ErrorContains(t, err, "finish Snowflake connection setup")
}

func TestSnowflakePendingSourceStaysDisabledUntilConfigured(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	source := remotemcptest.SeedServer(t, ctx, ti.conn, remoterepo.CreateServerParams{
		ProjectID: *auth.ProjectID, TransportType: "streamable-http",
		Url: "https://app.snowflake.com/speakeasy-mcp-setup",
	})
	sourceID := source.ID.String()
	server, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name: "Snowflake", RemoteMcpServerID: &sourceID, Visibility: types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("disabled"), server.Visibility)
	update := &gen.UpdateMcpServerPayload{ID: server.ID, RemoteMcpServerID: &sourceID, Visibility: types.McpServerVisibility("private")}
	_, err = ti.service.UpdateMcpServer(ctx, update)
	require.ErrorContains(t, err, "finish Snowflake connection setup")

	_, err = remoterepo.New(ti.conn).UpdateServer(ctx, remoterepo.UpdateServerParams{
		Name: conv.ToPGText("Snowflake"), Slug: conv.ToPGText("snowflake"),
		TransportType: "streamable-http", ID: uuid.MustParse(sourceID), ProjectID: *auth.ProjectID,
		Url: "https://example.snowflakecomputing.com/api/v2/databases/APP/schemas/PUBLIC/mcp-servers/MCP",
	})
	require.NoError(t, err)
	enabled, err := ti.service.UpdateMcpServer(ctx, update)
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("private"), enabled.Visibility)
}
