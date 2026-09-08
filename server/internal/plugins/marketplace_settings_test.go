package plugins_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestPluginsService_GetMarketplaceSettings_DefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	expectedDefault := defaultMarketplaceNameForTest(t, ctx, ti)

	result, err := ti.service.GetMarketplaceSettings(ctx, &gen.GetMarketplaceSettingsPayload{})
	require.NoError(t, err)
	require.Nil(t, result.MarketplaceName)
	require.Equal(t, expectedDefault, result.DefaultName)
	require.Equal(t, expectedDefault, result.EffectiveName)
	require.True(t, result.ObservabilityEnabled, "observability is on until a project turns it off")
	require.True(t, strings.HasSuffix(expectedDefault, "-speakeasy"),
		"default %q must carry the -speakeasy suffix so two orgs at default don't collide", expectedDefault)
}

func TestPluginsService_UpdateMarketplaceSettings_SetsOverrideWithoutRepublish(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	name := "acme-custom"
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{
		MarketplaceName: &name,
	})
	require.NoError(t, err)
	require.False(t, result.Republished, "no github connection exists, must not republish")
	require.NotNil(t, result.Settings.MarketplaceName)
	require.Equal(t, "acme-custom", *result.Settings.MarketplaceName)
	require.Equal(t, "acme-custom", result.Settings.EffectiveName)
	require.True(t, result.Settings.ObservabilityEnabled)

	// Round-trips through Get.
	got, err := ti.service.GetMarketplaceSettings(ctx, &gen.GetMarketplaceSettingsPayload{})
	require.NoError(t, err)
	require.NotNil(t, got.MarketplaceName)
	require.Equal(t, "acme-custom", *got.MarketplaceName)
	require.Equal(t, "acme-custom", got.EffectiveName)
}

func TestPluginsService_UpdateMarketplaceSettings_EmptyClearsOverride(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	// Seed an override first.
	name := "old-name"
	_, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &name})
	require.NoError(t, err)

	// Clear by sending empty string.
	empty := ""
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &empty})
	require.NoError(t, err)
	require.Nil(t, result.Settings.MarketplaceName, "empty input must clear the override")
	require.Equal(t, defaultMarketplaceNameForTest(t, ctx, ti), result.Settings.EffectiveName)
}

// defaultMarketplaceNameForTest computes the expected default marketplace name
// for the org used by a given test, mirroring the impl's resolution path.
func defaultMarketplaceNameForTest(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgName, err := pluginsrepo.New(ti.conn).GetOrganizationName(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		orgName = authCtx.OrganizationSlug
	}
	// The test instance's project is its org's only (hence default) project, so
	// the default name is the bare org-derived one — the slug is ignored.
	return plugins.DefaultMarketplaceName(orgName, "", true)
}

func TestPluginsService_UpdateMarketplaceSettings_RejectsInvalidName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	for _, bad := range []string{"-leading", "trailing-", "Has Spaces", "UPPERCASE", "underscore_name"} {
		_, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &bad})
		require.Error(t, err, "expected rejection for %q", bad)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeBadRequest, oopsErr.Code, "input %q", bad)
	}
}

func TestPluginsService_UpdateMarketplaceSettings_ForbiddenWithoutOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	ctx = authz.GrantsToContext(ctx, nil)

	name := "acme-custom"
	_, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &name})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestPluginsService_UpdateMarketplaceSettings_AutoRepublishesWhenConnected(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	// Establish a github connection by publishing once.
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Mkt Test"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "mkt-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	// Reset the mock so the next push is observably the republish-on-save.
	mock.pushFilesCalled = false
	mock.lastPushedFiles = nil

	name := "renamed-marketplace"
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &name})
	require.NoError(t, err)
	require.True(t, result.Republished, "expected republish when a github connection exists")
	require.True(t, mock.pushFilesCalled, "expected GitHub PushFiles to be called on republish")

	// The pushed marketplace.json should carry the new name.
	require.NotEmpty(t, mock.lastPushedFiles)
	raw, ok := mock.lastPushedFiles[".claude-plugin/marketplace.json"]
	require.True(t, ok, "missing claude marketplace.json in pushed files")
	var manifest struct {
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	require.Equal(t, "renamed-marketplace", manifest.Name)
}

func TestPluginsService_UpdateMarketplaceSettings_DisablesObservabilityWithoutClearingName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	name := "acme-custom"
	_, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{
		MarketplaceName: &name,
	})
	require.NoError(t, err)

	disabled := false
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{
		ObservabilityEnabled: &disabled,
	})
	require.NoError(t, err)
	require.False(t, result.Settings.ObservabilityEnabled)
	require.NotNil(t, result.Settings.MarketplaceName)
	require.Equal(t, "acme-custom", *result.Settings.MarketplaceName)

	got, err := ti.service.GetMarketplaceSettings(ctx, &gen.GetMarketplaceSettingsPayload{})
	require.NoError(t, err)
	require.False(t, got.ObservabilityEnabled)
	require.Equal(t, "acme-custom", *got.MarketplaceName)
}

func TestPluginsService_UpdateMarketplaceSettings_OmitsObservabilityFromRepublish(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Mkt Test"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "mkt-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	require.NotEmpty(t, mock.lastPushedFiles)
	var enabledManifest struct {
		Plugins []struct {
			Name string `json:"name"`
		} `json:"plugins"`
	}
	raw, ok := mock.lastPushedFiles[".claude-plugin/marketplace.json"]
	require.True(t, ok)
	require.NoError(t, json.Unmarshal(raw, &enabledManifest))
	require.GreaterOrEqual(t, len(enabledManifest.Plugins), 2, "observability + feature plugin")
	require.Contains(t, enabledManifest.Plugins[0].Name, "observability")

	disabled := false
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{
		ObservabilityEnabled: &disabled,
	})
	require.NoError(t, err)
	require.True(t, result.Republished)
	require.False(t, result.Settings.ObservabilityEnabled)

	raw, ok = mock.lastPushedFiles[".claude-plugin/marketplace.json"]
	require.True(t, ok)
	var disabledManifest struct {
		Plugins []struct {
			Name string `json:"name"`
		} `json:"plugins"`
	}
	require.NoError(t, json.Unmarshal(raw, &disabledManifest))
	require.Len(t, disabledManifest.Plugins, 1)
	require.NotContains(t, disabledManifest.Plugins[0].Name, "observability")

	for path := range mock.lastPushedFiles {
		require.NotContains(t, path, "observability", "disabled observability files must be omitted from the published tree: %s", path)
	}

	status, err := ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.Nil(t, status.ClaudeObservabilityPlugin)
	require.Nil(t, status.CodexObservabilityPlugin)

	// Re-enabling republishes the subtree: the toggle is reversible, and a
	// disabled project's cleared hooks version makes this look like a first
	// hooks publish rather than an unchanged one that would be skipped.
	enabled := true
	result, err = ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{
		ObservabilityEnabled: &enabled,
	})
	require.NoError(t, err)
	require.True(t, result.Settings.ObservabilityEnabled)

	var reenabledManifest struct {
		Plugins []struct {
			Name string `json:"name"`
		} `json:"plugins"`
	}
	raw, ok = mock.lastPushedFiles[".claude-plugin/marketplace.json"]
	require.True(t, ok)
	require.NoError(t, json.Unmarshal(raw, &reenabledManifest))
	require.Len(t, reenabledManifest.Plugins, 2)
	require.Contains(t, reenabledManifest.Plugins[0].Name, "observability")

	status, err = ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.NotNil(t, status.ClaudeObservabilityPlugin)
	require.NotNil(t, status.CodexObservabilityPlugin)
}

// The name and the observability toggle are written independently, so a
// rename must leave a project's disabled observability off — the mirror of
// TestPluginsService_UpdateMarketplaceSettings_DisablesObservabilityWithoutClearingName.
func TestPluginsService_UpdateMarketplaceSettings_RenameKeepsObservabilityDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	disabled := false
	_, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{
		ObservabilityEnabled: &disabled,
	})
	require.NoError(t, err)

	name := "acme-renamed"
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{
		MarketplaceName: &name,
	})
	require.NoError(t, err)
	require.False(t, result.Settings.ObservabilityEnabled)
	require.Equal(t, "acme-renamed", result.Settings.EffectiveName)

	got, err := ti.service.GetMarketplaceSettings(ctx, &gen.GetMarketplaceSettingsPayload{})
	require.NoError(t, err)
	require.False(t, got.ObservabilityEnabled)
	require.Equal(t, "acme-renamed", *got.MarketplaceName)
}
