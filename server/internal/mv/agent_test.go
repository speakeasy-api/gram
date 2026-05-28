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

func TestBuildAgentPluginsView_GroupsRowsByPlugin(t *testing.T) {
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

	result := mv.BuildAgentPluginsView("https://app.getgram.ai/", rows)

	require.NotEmpty(t, result.Etag)
	require.NotEqual(t, "empty", result.Etag)
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

	result := mv.BuildAgentPluginsView("https://app.getgram.ai", nil)

	require.Equal(t, "empty", result.Etag)
	require.Empty(t, result.Plugins)
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

	result := mv.BuildAgentPluginsView("https://app.getgram.ai", rows)

	require.Empty(t, result.Plugins, "row with missing mcp_slug should be filtered out")
}

func TestBuildAgentPluginsView_ETagStableAcrossCalls(t *testing.T) {
	t.Parallel()

	pluginID := uuid.New()
	serverID := uuid.New()
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	row := repo.GetAssignedPluginsWithServersRow{
		PluginID:          pluginID,
		PluginName:        "P",
		PluginSlug:        "p",
		PluginUpdatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
		ServerID:          serverID,
		ServerDisplayName: "s",
		ServerPolicy:      "required",
		ServerUpdatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
		ToolsetMcpSlug:    pgtype.Text{String: "s", Valid: true},
	}

	first := mv.BuildAgentPluginsView("https://app.getgram.ai", []repo.GetAssignedPluginsWithServersRow{row}).Etag
	second := mv.BuildAgentPluginsView("https://app.getgram.ai", []repo.GetAssignedPluginsWithServersRow{row}).Etag

	require.Equal(t, first, second)
}

func TestBuildAgentPluginsView_ETagChangesWhenPluginUpdated(t *testing.T) {
	t.Parallel()

	pluginID := uuid.New()
	serverID := uuid.New()

	row := func(updatedAt time.Time) repo.GetAssignedPluginsWithServersRow {
		return repo.GetAssignedPluginsWithServersRow{
			PluginID:          pluginID,
			PluginName:        "P",
			PluginSlug:        "p",
			PluginUpdatedAt:   pgtype.Timestamptz{Time: updatedAt, Valid: true},
			ServerID:          serverID,
			ServerDisplayName: "s",
			ServerPolicy:      "required",
			ServerUpdatedAt:   pgtype.Timestamptz{Time: updatedAt, Valid: true},
			ToolsetMcpSlug:    pgtype.Text{String: "s", Valid: true},
		}
	}

	t0 := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)

	before := mv.BuildAgentPluginsView("https://app.getgram.ai", []repo.GetAssignedPluginsWithServersRow{row(t0)}).Etag
	after := mv.BuildAgentPluginsView("https://app.getgram.ai", []repo.GetAssignedPluginsWithServersRow{row(t1)}).Etag

	require.NotEqual(t, before, after)
}
