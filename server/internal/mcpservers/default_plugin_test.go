package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAttachToDefaultPlugin_ExcludesPlatformWrapper(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Name: "Platform wrapper", Slug: "platform-wrapper", McpEnabled: true,
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID: toolset.ID, Version: 1,
		ToolUrns:     []urn.Tool{urn.NewTool(urn.ToolKindPlatform, "slack", "send_message")},
		ResourceUrns: []urn.Resource{},
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: *authCtx.ProjectID,
		Name: conv.ToPGText("Platform wrapper"), Slug: conv.ToPGText("platform-wrapper"),
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	seedEndpointFor(t, ctx, ti.conn, *authCtx.ProjectID, server.ID.String())
	tx := testenv.BeginTx(t, ctx, ti.conn)
	attached, pluginCreated, err := mcpservers.AttachToDefaultPlugin(ti.service, ctx, tx, authCtx, server)
	require.NoError(t, err)
	require.False(t, attached, "excluded wrappers must not trigger an initial marketplace publication")
	require.False(t, pluginCreated)
	_, err = pluginsrepo.New(tx).GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "exclusion must not lazily provision a marketplace plugin")
}
