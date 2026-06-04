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
	return plugins.DefaultMarketplaceName(orgName)
}

func TestPluginsService_UpdateMarketplaceSettings_RejectsInvalidName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	for _, bad := range []string{"-leading", "trailing-", "Has Spaces", "UPPERCASE", "underscore_name"} {
		bad := bad
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
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Server",
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
