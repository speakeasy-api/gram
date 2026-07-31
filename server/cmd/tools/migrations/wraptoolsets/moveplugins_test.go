package wraptoolsets

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/wraptoolsets/repo"
)

func TestRun_MovePluginsDryRunMakesNoWrites(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "move-dry-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	wrapToolset(t, tn, toolset.ID)

	pluginID := tn.newPlugin(t)
	tn.newPluginServer(t, pluginServerSpec{
		pluginID:    pluginID,
		toolsetID:   uuid.NullUUID{UUID: toolset.ID, Valid: true},
		mcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		displayName: "Live Server",
		deletedAt:   time.Time{},
	})
	tn.newPluginServer(t, pluginServerSpec{
		pluginID:    pluginID,
		toolsetID:   uuid.NullUUID{UUID: toolset.ID, Valid: true},
		mcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		displayName: "Old Server",
		deletedAt:   time.Now().UTC().Add(-24 * time.Hour),
	})

	report := runWrap(t, tn, movePluginsDryRunOptions())

	require.Len(t, report.Rows, 1)
	row := report.Rows[0]
	require.Equal(t, OutcomeWouldMovePlugins, row.Outcome)
	require.Equal(t, toolset.ID, row.ToolsetID)
	require.EqualValues(t, 2, row.PluginServersMoved)

	rows, err := tn.queries().ListPluginServerRowsByPluginID(ctx, pluginID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, ps := range rows {
		require.Equal(t, uuid.NullUUID{UUID: toolset.ID, Valid: true}, ps.ToolsetID)
		require.False(t, ps.McpServerID.Valid)
	}
}

func TestRun_MovePluginsMovesHistoryInPlaceAndRerunsClean(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "move-apply-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	wrapperID := wrapToolset(t, tn, toolset.ID)

	pluginID := tn.newPlugin(t)
	live := tn.newPluginServer(t, pluginServerSpec{
		pluginID:    pluginID,
		toolsetID:   uuid.NullUUID{UUID: toolset.ID, Valid: true},
		mcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		displayName: "Live Server",
		deletedAt:   time.Time{},
	})
	tombstoned := tn.newPluginServer(t, pluginServerSpec{
		pluginID:    pluginID,
		toolsetID:   uuid.NullUUID{UUID: toolset.ID, Valid: true},
		mcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		displayName: "Old Server",
		deletedAt:   time.Now().UTC().Add(-24 * time.Hour),
	})

	report := runWrap(t, tn, movePluginsApplyOptions())

	require.Len(t, report.Rows, 1)
	row := report.Rows[0]
	require.Equal(t, OutcomeMovedPlugins, row.Outcome)
	require.Equal(t, toolset.ID, row.ToolsetID)
	require.NotNil(t, row.McpServerID)
	require.Equal(t, wrapperID, *row.McpServerID)
	require.EqualValues(t, 2, row.PluginServersMoved)

	rows, err := tn.queries().ListPluginServerRowsByPluginID(ctx, pluginID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	before := map[uuid.UUID]repo.PluginServer{live.ID: live, tombstoned.ID: tombstoned}
	for _, ps := range rows {
		orig, ok := before[ps.ID]
		require.True(t, ok, "row id must be preserved")
		require.False(t, ps.ToolsetID.Valid)
		require.Equal(t, uuid.NullUUID{UUID: wrapperID, Valid: true}, ps.McpServerID)
		require.Equal(t, orig.DisplayName, ps.DisplayName)
		require.Equal(t, orig.Policy, ps.Policy)
		require.Equal(t, orig.SortOrder, ps.SortOrder)
		require.Equal(t, orig.CreatedAt.Time.UTC(), ps.CreatedAt.Time.UTC())
		require.Equal(t, orig.UpdatedAt.Time.UTC(), ps.UpdatedAt.Time.UTC())
		require.Equal(t, orig.DeletedAt.Valid, ps.DeletedAt.Valid)
		if orig.DeletedAt.Valid {
			require.Equal(t, orig.DeletedAt.Time.UTC(), ps.DeletedAt.Time.UTC())
		}
	}

	// A second apply run finds no toolset-keyed rows left and processes zero
	// candidates.
	rerun := runWrap(t, tn, movePluginsApplyOptions())
	require.Empty(t, rerun.Rows)
}

func TestRun_MovePluginsBlocksDualAttachment(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "move-dual-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	wrapperID := wrapToolset(t, tn, toolset.ID)

	pluginID := tn.newPlugin(t)
	tn.newPluginServer(t, pluginServerSpec{
		pluginID:    pluginID,
		toolsetID:   uuid.NullUUID{UUID: toolset.ID, Valid: true},
		mcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		displayName: "Toolset Keyed",
		deletedAt:   time.Time{},
	})
	tn.newPluginServer(t, pluginServerSpec{
		pluginID:    pluginID,
		toolsetID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		mcpServerID: uuid.NullUUID{UUID: wrapperID, Valid: true},
		displayName: "Wrapper Keyed",
		deletedAt:   time.Time{},
	})

	report := runWrap(t, tn, movePluginsApplyOptions())

	require.Len(t, report.Rows, 1)
	row := report.Rows[0]
	require.Equal(t, OutcomeBlockedDependentConflict, row.Outcome)
	require.Contains(t, row.Reason, "live attachments to both")

	// Nothing was written: the toolset-keyed row keeps its key.
	rows, err := tn.queries().ListPluginServerRowsByPluginID(ctx, pluginID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	toolsetKeyed := 0
	for _, ps := range rows {
		if ps.ToolsetID.Valid {
			toolsetKeyed++
			require.Equal(t, uuid.NullUUID{UUID: toolset.ID, Valid: true}, ps.ToolsetID)
		}
	}
	require.Equal(t, 1, toolsetKeyed)
}

func TestRun_MovePluginsBlocksWithoutWrapper(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	// No wrap run: the toolset has plugin attachments but no wrapper yet.
	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "move-nowrap-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})

	pluginID := tn.newPlugin(t)
	tn.newPluginServer(t, pluginServerSpec{
		pluginID:    pluginID,
		toolsetID:   uuid.NullUUID{UUID: toolset.ID, Valid: true},
		mcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		displayName: "Unwrapped",
		deletedAt:   time.Time{},
	})

	report := runWrap(t, tn, movePluginsApplyOptions())

	require.Len(t, report.Rows, 1)
	row := report.Rows[0]
	require.Equal(t, OutcomeBlockedNoWrapper, row.Outcome)

	rows, err := tn.queries().ListPluginServerRowsByPluginID(ctx, pluginID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, uuid.NullUUID{UUID: toolset.ID, Valid: true}, rows[0].ToolsetID)
	require.False(t, rows[0].McpServerID.Valid)
}
