package mv_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
)

func testMcpURL(slug string) string {
	return "https://app.getgram.ai/mcp/" + slug
}

func TestBuildAgentPluginsView_GroupsContiguousRowsByPlugin(t *testing.T) {
	t.Parallel()

	pluginA := uuid.New()
	pluginB := uuid.New()
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	rows := []repo.GetAssignedPluginsWithServersRow{
		{
			PluginID:           pluginA,
			PluginName:         "Engineering Tools",
			PluginSlug:         "engineering-tools",
			PluginDescription:  pgtype.Text{String: "internal", Valid: true},
			PluginUpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
			ServerID:           uuid.New(),
			ServerDisplayName:  "github-internal",
			ServerPolicy:       "required",
			ServerSortOrder:    0,
			ServerUpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
			ToolsetMcpSlug:     pgtype.Text{String: "github-internal", Valid: true},
			ToolsetMcpIsPublic: false,
		},
		{
			PluginID:           pluginA,
			PluginName:         "Engineering Tools",
			PluginSlug:         "engineering-tools",
			PluginDescription:  pgtype.Text{String: "internal", Valid: true},
			PluginUpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
			ServerID:           uuid.New(),
			ServerDisplayName:  "company-docs",
			ServerPolicy:       "optional",
			ServerSortOrder:    1,
			ServerUpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
			ToolsetMcpSlug:     pgtype.Text{String: "company-docs", Valid: true},
			ToolsetMcpIsPublic: true,
		},
		{
			PluginID:           pluginB,
			PluginName:         "Sales Tools",
			PluginSlug:         "sales-tools",
			PluginDescription:  pgtype.Text{},
			PluginUpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
			ServerID:           uuid.New(),
			ServerDisplayName:  "hubspot",
			ServerPolicy:       "required",
			ServerSortOrder:    0,
			ServerUpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
			ToolsetMcpSlug:     pgtype.Text{String: "hubspot-mcp", Valid: true},
			ToolsetMcpIsPublic: false,
		},
	}

	result := mv.BuildAgentPluginsView(rows, testMcpURL)

	require.NotEmpty(t, result.Etag)
	require.Len(t, result.Plugins, 2)

	require.Equal(t, pluginA.String(), result.Plugins[0].ID)
	require.Equal(t, "engineering-tools", result.Plugins[0].Slug)
	require.Equal(t, "Engineering Tools", result.Plugins[0].Name)
	require.NotNil(t, result.Plugins[0].Description)
	require.Equal(t, "internal", *result.Plugins[0].Description)
	require.Len(t, result.Plugins[0].Servers, 2)

	require.Equal(t, "github-internal", result.Plugins[0].Servers[0].DisplayName)
	require.Equal(t, "required", result.Plugins[0].Servers[0].Policy)
	require.Equal(t, "https://app.getgram.ai/mcp/github-internal", result.Plugins[0].Servers[0].McpURL)
	require.False(t, result.Plugins[0].Servers[0].IsPublic)

	require.Equal(t, "company-docs", result.Plugins[0].Servers[1].DisplayName)
	require.True(t, result.Plugins[0].Servers[1].IsPublic)

	require.Equal(t, pluginB.String(), result.Plugins[1].ID)
	require.Nil(t, result.Plugins[1].Description, "missing description should round-trip as nil")
	require.Len(t, result.Plugins[1].Servers, 1)
}

func TestBuildAgentPluginsView_EmptyRows(t *testing.T) {
	t.Parallel()

	result := mv.BuildAgentPluginsView(nil, testMcpURL)

	require.Empty(t, result.Plugins)
	// sha256 of an empty input — same revision string every time, distinct
	// from any populated set.
	require.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", result.Etag)
}

func TestBuildAgentPluginsView_SkipsRowsWithMissingMcpSlug(t *testing.T) {
	t.Parallel()

	rows := []repo.GetAssignedPluginsWithServersRow{
		{
			PluginID:           uuid.New(),
			PluginName:         "Bare Plugin",
			PluginSlug:         "bare",
			PluginUpdatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
			ServerID:           uuid.New(),
			ServerDisplayName:  "no-toolset",
			ServerPolicy:       "required",
			ServerUpdatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
			ToolsetMcpSlug:     pgtype.Text{Valid: false},
			ToolsetMcpIsPublic: false,
		},
	}

	result := mv.BuildAgentPluginsView(rows, testMcpURL)

	require.Empty(t, result.Plugins, "row with missing mcp_slug should be filtered out")
}

func TestBuildAgentPluginsView_ETagStableAcrossCalls(t *testing.T) {
	t.Parallel()

	row := makeAgentRow(uuid.New(), uuid.New(), time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC), "s")

	first := mv.BuildAgentPluginsView([]repo.GetAssignedPluginsWithServersRow{row}, testMcpURL).Etag
	second := mv.BuildAgentPluginsView([]repo.GetAssignedPluginsWithServersRow{row}, testMcpURL).Etag

	require.Equal(t, first, second)
}

func TestBuildAgentPluginsView_ETagChangesWhenPluginUpdated(t *testing.T) {
	t.Parallel()

	pluginID := uuid.New()
	serverID := uuid.New()
	t0 := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	before := mv.BuildAgentPluginsView([]repo.GetAssignedPluginsWithServersRow{makeAgentRow(pluginID, serverID, t0, "s")}, testMcpURL).Etag
	after := mv.BuildAgentPluginsView([]repo.GetAssignedPluginsWithServersRow{makeAgentRow(pluginID, serverID, t0.Add(time.Second), "s")}, testMcpURL).Etag

	require.NotEqual(t, before, after)
}

func TestBuildAgentPluginsView_ETagIgnoresMcpURLPrefix(t *testing.T) {
	t.Parallel()

	row := makeAgentRow(uuid.New(), uuid.New(), time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC), "s")

	prodURL := func(slug string) string { return "https://app.getgram.ai/mcp/" + slug }
	stagingURL := func(slug string) string { return "https://staging.getgram.ai/mcp/" + slug }

	prod := mv.BuildAgentPluginsView([]repo.GetAssignedPluginsWithServersRow{row}, prodURL).Etag
	staging := mv.BuildAgentPluginsView([]repo.GetAssignedPluginsWithServersRow{row}, stagingURL).Etag

	require.Equal(t, prod, staging, "ETag covers content, not deployment-config like serverURL")
}

func makeAgentRow(pluginID, serverID uuid.UUID, updatedAt time.Time, mcpSlug string) repo.GetAssignedPluginsWithServersRow {
	return repo.GetAssignedPluginsWithServersRow{
		PluginID:          pluginID,
		PluginName:        "P",
		PluginSlug:        "p",
		PluginUpdatedAt:   pgtype.Timestamptz{Time: updatedAt, Valid: true},
		ServerID:          serverID,
		ServerDisplayName: "s",
		ServerPolicy:      "required",
		ServerUpdatedAt:   pgtype.Timestamptz{Time: updatedAt, Valid: true},
		ToolsetMcpSlug:    pgtype.Text{String: mcpSlug, Valid: true},
	}
}
