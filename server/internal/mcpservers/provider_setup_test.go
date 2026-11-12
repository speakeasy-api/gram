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

func testPendingSourceCannotBeCreatedEnabled(t *testing.T, marker string) {
	t.Helper()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	source := remotemcptest.SeedServer(t, ctx, ti.conn, remoterepo.CreateServerParams{
		ProjectID: *auth.ProjectID, TransportType: "streamable-http",
		Url: marker,
	})
	sourceID := source.ID.String()
	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name: "Provider setup", RemoteMcpServerID: &sourceID, Visibility: types.McpServerVisibility("private"),
	})
	require.ErrorContains(t, err, "finish connection setup")
}

func testPendingSourceStaysDisabledUntilConfigured(t *testing.T, marker string) {
	t.Helper()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	source := remotemcptest.SeedServer(t, ctx, ti.conn, remoterepo.CreateServerParams{
		ProjectID: *auth.ProjectID, TransportType: "streamable-http",
		Url: marker,
	})
	sourceID := source.ID.String()
	server, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name: "Provider setup", RemoteMcpServerID: &sourceID, Visibility: types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("disabled"), server.Visibility)
	update := &gen.UpdateMcpServerPayload{ID: server.ID, RemoteMcpServerID: &sourceID, Visibility: types.McpServerVisibility("private")}
	_, err = ti.service.UpdateMcpServer(ctx, update)
	require.ErrorContains(t, err, "finish connection setup")

	_, err = remoterepo.New(ti.conn).UpdateServer(ctx, remoterepo.UpdateServerParams{
		Name: conv.ToPGText("Provider setup"), Slug: conv.ToPGText("provider-setup"),
		TransportType: "streamable-http", ID: uuid.MustParse(sourceID), ProjectID: *auth.ProjectID,
		Url: "https://example.snowflakecomputing.com/api/v2/databases/APP/schemas/PUBLIC/mcp-servers/MCP",
	})
	require.NoError(t, err)
	enabled, err := ti.service.UpdateMcpServer(ctx, update)
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("private"), enabled.Visibility)
}

func TestProviderPendingSourceCannotBeCreatedEnabled(t *testing.T) {
	t.Parallel()
	for _, marker := range []string{"https://app.snowflake.com/speakeasy-mcp-setup", "https://www.okta.com/speakeasy-mcp-setup"} {
		t.Run(marker, func(t *testing.T) {
			t.Parallel()
			testPendingSourceCannotBeCreatedEnabled(t, marker)
		})
	}
}

func TestProviderPendingSourceStaysDisabledUntilConfigured(t *testing.T) {
	t.Parallel()
	for _, marker := range []string{"https://app.snowflake.com/speakeasy-mcp-setup", "https://www.okta.com/speakeasy-mcp-setup"} {
		t.Run(marker, func(t *testing.T) {
			t.Parallel()
			testPendingSourceStaysDisabledUntilConfigured(t, marker)
		})
	}
}
