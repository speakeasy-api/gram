package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestInventoryCursorBindsProjectAndPrincipal(t *testing.T) {
	t.Parallel()
	codec, err := newInventoryCursorCodec("test-cursor-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", ConnectionID: "connection", Generation: "generation"}
	projectID := uuid.New()
	afterID := uuid.New()
	value, err := codec.Encode(inventoryCursor{
		OrganizationID: principal.OrganizationID,
		Binding:        principalCursorBinding(principal),
		ProjectID:      projectID.String(),
		AfterMCPID:     afterID.String(),
	})
	require.NoError(t, err)

	got, err := codec.Decode(value, principal, projectID, "")
	require.NoError(t, err)
	require.Equal(t, afterID, got)

	_, err = codec.Decode(value, principal, uuid.New(), "")
	require.ErrorIs(t, err, ErrInventoryCursorInvalid)
	_, err = codec.Decode(value, Principal{OrganizationID: principal.OrganizationID, ConnectionID: "connection", Generation: "other"}, projectID, "")
	require.ErrorIs(t, err, ErrInventoryCursorInvalid)
	_, err = codec.Decode(value+"x", principal, projectID, "")
	require.ErrorIs(t, err, ErrInventoryCursorInvalid)
}

func TestMCPFromInventoryLabelsOwnershipAndNeverProbesLegacy(t *testing.T) {
	t.Parallel()
	mcpID, projectID, registrationID := uuid.New(), uuid.New(), uuid.New()
	complete := uuid.NullUUID{UUID: uuid.New(), Valid: true}

	platform := mcpFromInventory(mcpID, projectID, "Project", "project", "Reviewed", "reviewed", "private", "dashboard_managed", registrationID, "catalog", "registry", "reviewed/server", "registered", complete, complete, complete, complete, "ready", "checked", "expires", map[uuid.UUID][]MCPDistribution{registrationID: {{PluginID: "plugin", State: "attached", PublicationState: "published"}}})
	require.Equal(t, "platform_managed", platform.Model)
	require.Equal(t, "reviewed_catalogue", platform.Source.Kind)
	require.Equal(t, "ready", platform.Readiness.State)
	require.Len(t, platform.Distributions, 1)
	require.True(t, platform.Registration.ComponentsComplete)

	incomplete := mcpFromInventory(mcpID, projectID, "Project", "project", "Incomplete", "incomplete", "private", "dashboard_managed", registrationID, "catalog", "registry", "reviewed/incomplete", "pending", uuid.NullUUID{UUID: uuid.Nil, Valid: true}, complete, complete, complete, "", "", "", nil)
	require.False(t, incomplete.Registration.ComponentsComplete, "zero UUID sentinels represent missing persisted components")
	require.NotNil(t, incomplete.Distributions, "registered rows without distributions retain the stable empty array")
	require.Empty(t, incomplete.Distributions)

	dashboard := mcpFromInventory(mcpID, projectID, "Project", "project", "Dashboard", "dashboard", "private", "dashboard_managed", uuid.Nil, "", "", "", "", uuid.NullUUID{}, uuid.NullUUID{}, uuid.NullUUID{}, uuid.NullUUID{}, "", "", "", nil)
	require.Equal(t, "dashboard_managed", dashboard.Model)
	require.Equal(t, "dashboard_source", dashboard.Source.Kind)
	require.Equal(t, "unsupported", dashboard.Readiness.State)
	require.Equal(t, []string{"read"}, dashboard.Operations)

	legacy := mcpFromInventory(mcpID, projectID, "Project", "project", "Legacy", "legacy", "disabled", "legacy", uuid.Nil, "", "", "", "", uuid.NullUUID{}, uuid.NullUUID{}, uuid.NullUUID{}, uuid.NullUUID{}, "ready", "checked", "expires", nil)
	require.Equal(t, "legacy", legacy.Model)
	require.Equal(t, "unsupported", legacy.Readiness.State, "legacy rows never use readiness evidence or trigger egress")
	require.False(t, legacy.EffectiveEnabled)
}

func TestInventoryModelRecognizesEveryDashboardBackend(t *testing.T) {
	t.Parallel()

	for _, backend := range []uuid.NullUUID{
		{UUID: uuid.New(), Valid: true},
	} {
		require.Equal(t, "dashboard_managed", inventoryModel(backend, uuid.NullUUID{}, uuid.NullUUID{}))
		require.Equal(t, "dashboard_managed", inventoryModel(uuid.NullUUID{}, backend, uuid.NullUUID{}))
		require.Equal(t, "dashboard_managed", inventoryModel(uuid.NullUUID{}, uuid.NullUUID{}, backend))
	}
	require.Equal(t, "legacy", inventoryModel(uuid.NullUUID{}, uuid.NullUUID{}, uuid.NullUUID{}))
}

func TestInventoryQueryResultReturnsUniqueExactOrBoundedCandidates(t *testing.T) {
	t.Parallel()
	mcps := make([]MCP, 0, 12)
	for range 12 {
		mcps = append(mcps, MCP{ID: uuid.New().String(), Name: "Example Server", Slug: "example-server"})
	}
	mcps[0].Slug = "unique"

	exact := inventoryQueryResult(mcps, "unique", 10)
	require.Len(t, exact, 1)
	require.Equal(t, "unique", exact[0].Slug)

	candidates := inventoryQueryResult(mcps, "example", 10)
	require.Len(t, candidates, 10)
	require.Len(t, inventoryQueryResult(mcps, "example", 3), 3, "the request limit also bounds query candidates")

	duplicates := inventoryQueryResult([]MCP{{Name: "Duplicate"}, {Name: "Duplicate"}}, "duplicate", 10)
	require.Len(t, duplicates, 2, "an ambiguous exact name remains a candidate list")
}
