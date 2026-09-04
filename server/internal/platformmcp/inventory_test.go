package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
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

	platform := mcpFromInventory(mcpID, projectID, "Project", "project", "Reviewed", "reviewed", "private", "dashboard_managed", MCPBackendRemote, registrationID, "catalog", "registry", "reviewed/server", "registered", complete, complete, complete, complete, "ready", "checked", "expires", map[uuid.UUID][]MCPDistribution{registrationID: {{PluginID: "plugin", State: "attached", PublicationState: "published"}}})
	require.Equal(t, "platform_managed", platform.Model)
	require.Equal(t, MCPBackendRemote, platform.BackendKind)
	require.Equal(t, "reviewed_catalogue", platform.Source.Kind)
	require.Equal(t, "ready", platform.Readiness.State)
	require.Len(t, platform.Distributions, 1)
	require.True(t, platform.Registration.ComponentsComplete)
	require.Equal(t, []string{"read", "dashboard_setup", "update_mcp_metadata", "disable_mcp"}, platform.Operations)

	disabled := mcpFromInventory(mcpID, projectID, "Project", "project", "Reviewed", "reviewed", "disabled", "dashboard_managed", MCPBackendRemote, registrationID, "catalog", "registry", "reviewed/server", "registered", complete, complete, complete, complete, "ready", "checked", "expires", nil)
	require.False(t, disabled.EffectiveEnabled)
	require.Equal(t, "unknown", disabled.Readiness.State, "disabled Platform-managed MCPs do not expose stale readiness as effective")
	require.Empty(t, disabled.Readiness.CheckedAt)
	require.Equal(t, []string{"read", "dashboard_setup", "update_mcp_metadata", "enable_mcp"}, disabled.Operations)

	incomplete := mcpFromInventory(mcpID, projectID, "Project", "project", "Incomplete", "incomplete", "private", "dashboard_managed", MCPBackendRemote, registrationID, "catalog", "registry", "reviewed/incomplete", "pending", uuid.NullUUID{UUID: uuid.Nil, Valid: true}, complete, complete, complete, "", "", "", nil)
	require.False(t, incomplete.Registration.ComponentsComplete, "zero UUID sentinels represent missing persisted components")
	require.NotNil(t, incomplete.Distributions, "registered rows without distributions retain the stable empty array")
	require.Empty(t, incomplete.Distributions)

	dashboard := mcpFromInventory(mcpID, projectID, "Project", "project", "Dashboard", "dashboard", "private", "dashboard_managed", MCPBackendTunneled, uuid.Nil, "", "", "", "", uuid.NullUUID{}, uuid.NullUUID{}, uuid.NullUUID{}, uuid.NullUUID{}, "", "", "", nil)
	require.Equal(t, "dashboard_managed", dashboard.Model)
	require.Equal(t, MCPBackendTunneled, dashboard.BackendKind)
	require.Equal(t, "dashboard_source", dashboard.Source.Kind, "the compatibility source kind remains unchanged")
	require.Equal(t, "unsupported", dashboard.Readiness.State)
	require.Equal(t, []string{"read", "dashboard_setup"}, dashboard.Operations)

	legacy := mcpFromInventory(mcpID, projectID, "Project", "project", "Legacy", "legacy", "disabled", "legacy", MCPBackendLegacy, uuid.Nil, "", "", "", "", uuid.NullUUID{}, uuid.NullUUID{}, uuid.NullUUID{}, uuid.NullUUID{}, "ready", "checked", "expires", nil)
	require.Equal(t, "legacy", legacy.Model)
	require.Equal(t, "unsupported", legacy.Readiness.State, "legacy rows never use readiness evidence or trigger egress")
	require.False(t, legacy.EffectiveEnabled)
}

func TestLifecycleMetadataVersionChangesOnlyWithMutableInventoryState(t *testing.T) {
	t.Parallel()

	key := lifecycleMetadataVersionKey("test-key")
	baseline := lifecycleMetadataVersion(key, "mcp", "project", "Example", "example", "private")
	require.NotEmpty(t, baseline)
	require.Equal(t, baseline, lifecycleMetadataVersion(key, "mcp", "project", "Example", "example", "private"))
	require.NotEqual(t, baseline, lifecycleMetadataVersion(key, "mcp", "project", "Renamed", "example", "private"))
	require.NotEqual(t, baseline, lifecycleMetadataVersion(key, "mcp", "project", "Example", "example", "disabled"))
	require.NotEqual(t, baseline, lifecycleMetadataVersion(lifecycleMetadataVersionKey("other-key"), "mcp", "project", "Example", "example", "private"))
}

func TestInventoryModelRecognizesEveryDashboardBackend(t *testing.T) {
	t.Parallel()

	backend := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	require.Equal(t, "dashboard_managed", inventoryModel(backend, uuid.NullUUID{}, uuid.NullUUID{}))
	require.Equal(t, "dashboard_managed", inventoryModel(uuid.NullUUID{}, backend, uuid.NullUUID{}))
	require.Equal(t, "dashboard_managed", inventoryModel(uuid.NullUUID{}, uuid.NullUUID{}, backend))
	require.Equal(t, "legacy", inventoryModel(uuid.NullUUID{}, uuid.NullUUID{}, uuid.NullUUID{}))
}

func TestInventoryRowProjectsHostedBackendWithLegacyOwnership(t *testing.T) {
	t.Parallel()

	backendID := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	mcp := mcpFromInventoryRow(platformrepo.ListPlatformMCPInventoryRow{
		McpServerID: uuid.New(),
		ProjectID:   uuid.New(),
		Visibility:  "private",
		ToolsetID:   backendID,
	}, nil)

	require.Equal(t, MCPBackendHosted, mcp.BackendKind)
	require.Equal(t, "legacy", mcp.Model, "toolset-backed hosted servers retain their existing ownership model")
	require.Equal(t, "legacy", mcp.Source.Kind)
	require.Equal(t, []string{"read"}, mcp.Operations)
}

func TestInventoryBackendKindRecognizesEveryConfiguredBackend(t *testing.T) {
	t.Parallel()

	backend := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	empty := uuid.NullUUID{}
	require.Equal(t, MCPBackendRemote, inventoryBackendKind(backend, empty, empty, empty))
	require.Equal(t, MCPBackendTunneled, inventoryBackendKind(empty, backend, empty, empty))
	require.Equal(t, MCPBackendHosted, inventoryBackendKind(empty, empty, backend, empty))
	require.Equal(t, MCPBackendUnproxied, inventoryBackendKind(empty, empty, empty, backend))
	require.Equal(t, MCPBackendLegacy, inventoryBackendKind(empty, empty, empty, empty))
	require.Equal(t, MCPBackendLegacy, inventoryBackendKind(uuid.NullUUID{UUID: uuid.Nil, Valid: true}, empty, empty, empty))
}

func TestMCPInventoryOutputProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	output := FindMCPOutput{MCPs: []MCP{{
		ID:               "mcp",
		ProjectID:        "project",
		ProjectName:      "Project",
		ProjectSlug:      "project",
		Name:             "Example",
		Slug:             "example",
		Version:          "version",
		Visibility:       "private",
		EffectiveEnabled: true,
		Model:            "platform_managed",
		BackendKind:      MCPBackendRemote,
		Source:           MCPSource{Kind: "reviewed_catalogue", Provider: "provider", Reference: "reference"},
		Registration:     &MCPRegistration{ID: "registration", Status: "registered", ComponentsComplete: true},
		Readiness:        MCPReadiness{State: "ready", CheckedAt: "checked", ExpiresAt: "expires"},
		Distributions:    []MCPDistribution{{PluginID: "plugin", State: "attached", PublicationState: "published"}},
		Operations:       []string{"read"},
		DashboardPath:    "dashboard_mcp_settings",
	}}, NextCursor: "cursor"}

	require.ElementsMatch(t, []string{
		"mcps", "next_cursor",
		"id", "project_id", "project_name", "project_slug", "name", "slug", "version", "visibility", "effective_enabled", "model", "backend_kind",
		"source", "kind", "provider", "reference",
		"registration", "id", "status", "components_complete",
		"readiness", "state", "checked_at", "expires_at",
		"distributions", "plugin_id", "state", "publication_state",
		"operations", "dashboard_path",
	}, decodeKeys(t, output))
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
