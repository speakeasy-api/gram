package plugins_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// marketplaceRenameFixture publishes a project once (non-phased, so it lands on
// the current hooks version + config) and returns the org id used for key counts.
func marketplaceRenameFixture(t *testing.T, ctx context.Context, ti *testInstance, name string) string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: name})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, name+"-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty(name + " Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	return authCtx.ActiveOrganizationID
}

// A marketplace rename changes the hook-output config (the resolved name), so an
// org already on the current hooks version must regenerate its hooks subtree —
// minting a fresh hooks key — when it is eligible for the current hooks version.
func TestPluginsService_UpdateMarketplaceSettings_RenameRegeneratesHooksWhenEligible(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	features := &feature.InMemory{}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, features)

	orgID := marketplaceRenameFixture(t, ctx, ti, "Rename Eligible")

	// Clear the org for the current hooks version (pin above any real version).
	features.SetFlagPayload(feature.FlagHooksRollout, orgID, []byte(`{"version": 9999}`))

	hooksKeysBefore := countPluginHooksKeys(t, ctx, ti.conn, orgID)

	name := "renamed-eligible"
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &name})
	require.NoError(t, err)
	require.True(t, result.Republished)
	require.False(t, conv.PtrValOr(result.HooksUpdateDeferred, false), "an eligible org applies the rename to hooks immediately")
	require.Equal(t, hooksKeysBefore+1, countPluginHooksKeys(t, ctx, ti.conn, orgID),
		"a rename must regenerate the hooks subtree (fresh key) for an eligible org, not carry stale hook commands")
}

// The same rename for an org NOT cleared for the current hooks version reaches the
// MCP plugins and marketplace manifests but defers the observability hooks: the
// stored hooks are carried (no new key) and the result flags the deferral.
func TestPluginsService_UpdateMarketplaceSettings_RenameDefersHooksWhenNotEligible(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	// Empty provider → no clearance payload, and the test org is not a canary.
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, &feature.InMemory{})

	orgID := marketplaceRenameFixture(t, ctx, ti, "Rename Deferred")
	hooksKeysBefore := countPluginHooksKeys(t, ctx, ti.conn, orgID)

	// Keep lastPushedFiles so GetRepoFiles returns the baseline (carrying hooks
	// requires the existing repo). PushFiles overwrites it with the new push,
	// which the marketplace.json assertion below reads.
	mock.pushFilesCalled = false

	name := "renamed-deferred"
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &name})
	require.NoError(t, err)
	require.True(t, result.Republished)
	require.True(t, conv.PtrValOr(result.HooksUpdateDeferred, false), "a non-eligible org must defer the hooks update")

	// The new name still reaches the shared marketplace manifest and MCP plugins.
	require.True(t, mock.pushFilesCalled)
	raw, ok := mock.lastPushedFiles[".claude-plugin/marketplace.json"]
	require.True(t, ok, "missing claude marketplace.json in pushed files")
	var manifest struct {
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	require.Equal(t, "renamed-deferred", manifest.Name)

	// But the hooks subtree is carried verbatim: no fresh hooks key.
	require.Equal(t, hooksKeysBefore, countPluginHooksKeys(t, ctx, ti.conn, orgID),
		"a deferred rename must not regenerate the hooks subtree or mint a hooks key")
}
