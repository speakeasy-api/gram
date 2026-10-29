package plugins_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// autoSyncFixture is a project whose marketplace was published by hand with
// two plugins, each carrying one server: paused, whose auto sync the test turns
// off, and active, which keeps syncing automatically.
type autoSyncFixture struct {
	ti        *testInstance
	mock      *mockGitHubPublisher
	projectID uuid.UUID
	userID    string
	paused    *gen.Plugin
	active    *gen.Plugin
}

func newAutoSyncFixture(t *testing.T, name string) (context.Context, *autoSyncFixture) {
	t.Helper()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	f := newAutoSyncFixtureFor(t, ctx, ti, mock)

	paused, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: name + " Paused", Description: conv.PtrEmpty("Published description")})
	require.NoError(t, err)
	f.paused = paused
	f.addServer(t, ctx, paused, name+"-paused-one", "Paused One", 0)

	active, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: name + " Active"})
	require.NoError(t, err)
	f.active = active
	f.addServer(t, ctx, active, name+"-active-one", "Active One", 0)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	return ctx, f
}

func newAutoSyncFixtureFor(t *testing.T, ctx context.Context, ti *testInstance, mock *mockGitHubPublisher) *autoSyncFixture {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return &autoSyncFixture{ti: ti, mock: mock, projectID: *authCtx.ProjectID, userID: authCtx.UserID, paused: nil, active: nil}
}

func (f *autoSyncFixture) addServer(t *testing.T, ctx context.Context, plugin *gen.Plugin, toolsetName, displayName string, sortOrder int32) {
	t.Helper()
	toolset := createTestToolset(t, ctx, f.ti.conn, toolsetName)
	_, err := f.ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty(displayName),
		Policy:      "required",
		SortOrder:   sortOrder,
	})
	require.NoError(t, err)
}

// setAutoSync flips a plugin's auto sync the way the dashboard switch does:
// every other field is sent back unchanged.
func (f *autoSyncFixture) setAutoSync(ctx context.Context, plugin *gen.Plugin, enabled bool) (*gen.Plugin, error) {
	current, err := f.ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	if err != nil {
		return nil, fmt.Errorf("get plugin: %w", err)
	}
	updated, err := f.ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{
		ID:              current.ID,
		Name:            current.Name,
		Slug:            current.Slug,
		Description:     current.Description,
		AutoSyncEnabled: &enabled,
	})
	if err != nil {
		return nil, fmt.Errorf("update plugin: %w", err)
	}
	return updated, nil
}

func (f *autoSyncFixture) automaticPublish(t *testing.T, ctx context.Context) *plugins.PublishProjectResult {
	t.Helper()
	result, err := f.ti.service.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID:       f.projectID,
		CreatedByUserID: f.userID,
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	})
	require.NoError(t, err)
	return result
}

func (f *autoSyncFixture) storedFingerprints(t *testing.T, ctx context.Context) map[string]string {
	t.Helper()
	conn, err := pluginsrepo.New(f.ti.conn).GetGitHubConnection(ctx, f.projectID)
	require.NoError(t, err)
	stored := make(map[string]string)
	require.NoError(t, json.Unmarshal(conn.PublishedMcpFingerprints, &stored))
	return stored
}

// pluginFilesOf returns every pushed file in one plugin's package directories.
func pluginFilesOf(files map[string][]byte, slug string) map[string]string {
	out := make(map[string]string)
	for _, prefix := range []string{
		slug + "/",
		"cursor-plugins/" + slug + "-cursor/",
		slug + "-codex/",
		"opencode-plugins/" + slug + "/",
		"agent-plugins/" + slug + "/",
	} {
		maps.Copy(out, filesWithPrefix(files, prefix))
	}
	return out
}

type claudeMarketplaceEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

func claudeMarketplaceEntries(t *testing.T, files map[string][]byte) map[string]claudeMarketplaceEntry {
	t.Helper()
	var manifest struct {
		Plugins []claudeMarketplaceEntry `json:"plugins"`
	}
	require.NoError(t, json.Unmarshal(files[".claude-plugin/marketplace.json"], &manifest))
	out := make(map[string]claudeMarketplaceEntry, len(manifest.Plugins))
	for _, entry := range manifest.Plugins {
		out[entry.Name] = entry
	}
	return out
}

// codexMarketplaceAuthentication maps each Codex marketplace entry to its
// authentication policy.
func codexMarketplaceAuthentication(t *testing.T, files map[string][]byte) map[string]string {
	t.Helper()
	var manifest struct {
		Plugins []struct {
			Name   string `json:"name"`
			Policy struct {
				Authentication string `json:"authentication"`
			} `json:"policy"`
		} `json:"plugins"`
	}
	require.NoError(t, json.Unmarshal(files[".agents/plugins/marketplace.json"], &manifest))
	out := make(map[string]string, len(manifest.Plugins))
	for _, entry := range manifest.Plugins {
		out[entry.Name] = entry.Policy.Authentication
	}
	return out
}

func requireFailedPrecondition(t *testing.T, err error, message string) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeFailedPrecondition, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), message)
}

func TestAutoSync_CreatePluginDefaultsOnAndAcceptsOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	created, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Syncs By Default"})
	require.NoError(t, err)
	require.True(t, created.AutoSyncEnabled)

	off := false
	paused, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Created Paused", AutoSyncEnabled: &off})
	require.NoError(t, err)
	require.False(t, paused.AutoSyncEnabled)

	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: paused.ID})
	require.NoError(t, err)
	require.False(t, got.AutoSyncEnabled)

	listed, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	bySlug := make(map[string]bool)
	for _, p := range listed.Plugins {
		bySlug[p.Slug] = p.AutoSyncEnabled
	}
	require.Contains(t, bySlug, created.Slug)
	require.Contains(t, bySlug, paused.Slug, "the list must include a plugin created with auto sync off")
	require.True(t, bySlug[created.Slug])
	require.False(t, bySlug[paused.Slug])
}

// An automatic publish keeps a paused plugin exactly as it was last published
// (files, manifest entry, and stored fingerprint) while another plugin's change
// goes out, and does not keep republishing because of the held changes.
func TestAutoSync_AutomaticPublishHoldsPausedPluginChanges(t *testing.T) {
	t.Parallel()

	ctx, f := newAutoSyncFixture(t, "hold")
	publishedPaused := pluginFilesOf(f.mock.lastPushedFiles, f.paused.Slug)
	require.NotEmpty(t, publishedPaused)
	storedBefore := f.storedFingerprints(t, ctx)

	_, err := f.setAutoSync(ctx, f.paused, false)
	require.NoError(t, err)

	// Both plugins change: the paused one gains a server and a new
	// description, the active one gains a server.
	f.addServer(t, ctx, f.paused, "hold-paused-two", "Paused Two", 1)
	_, err = f.ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{
		ID: f.paused.ID, Name: f.paused.Name, Slug: f.paused.Slug, Description: conv.PtrEmpty("Unsynced description"),
	})
	require.NoError(t, err)
	f.addServer(t, ctx, f.active, "hold-active-two", "Active Two", 1)

	result := f.automaticPublish(t, ctx)
	require.False(t, result.Skipped, "the active plugin's change must publish")

	pushed := f.mock.lastPushedFiles
	require.Equal(t, publishedPaused, pluginFilesOf(pushed, f.paused.Slug), "a paused plugin's published files must be copied verbatim")
	require.NotContains(t, string(pushed[f.paused.Slug+"/.mcp.json"]), "Paused Two")
	require.Contains(t, string(pushed[f.active.Slug+"/.mcp.json"]), "Active Two")

	entries := claudeMarketplaceEntries(t, pushed)
	require.Contains(t, entries, f.paused.Slug, "the marketplace must keep listing the paused plugin")
	require.Equal(t, "Published description", entries[f.paused.Slug].Description)
	require.Equal(t, f.paused.Name, entries[f.paused.Slug].DisplayName)
	require.Contains(t, string(pushed["README.md"]), "Published description")
	require.NotContains(t, string(pushed["README.md"]), "Unsynced description")

	storedAfter := f.storedFingerprints(t, ctx)
	require.Equal(t, storedBefore[f.paused.Slug], storedAfter[f.paused.Slug], "a held plugin keeps its published fingerprint")
	require.NotEqual(t, storedBefore[f.active.Slug], storedAfter[f.active.Slug])

	// Nothing else changed, so the held changes alone must not trigger
	// another publish.
	f.mock.pushFilesCalled = false
	again := f.automaticPublish(t, ctx)
	require.True(t, again.Skipped, "held changes must not republish on their own")
	require.False(t, f.mock.pushFilesCalled)

	// A manual sync would still publish them, so the marketplace reads stale.
	status, err := f.ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.NotNil(t, status.UpToDate)
	require.False(t, *status.UpToDate)
}

// A manual sync is the one publish that sends a paused plugin's pending
// changes.
func TestAutoSync_ManualSyncPublishesHeldChanges(t *testing.T) {
	t.Parallel()

	ctx, f := newAutoSyncFixture(t, "manual")
	_, err := f.setAutoSync(ctx, f.paused, false)
	require.NoError(t, err)
	f.addServer(t, ctx, f.paused, "manual-paused-two", "Paused Two", 1)

	held := f.automaticPublish(t, ctx)
	require.True(t, held.Skipped, "only the paused plugin changed, so nothing publishes automatically")

	_, err = f.ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.Contains(t, string(f.mock.lastPushedFiles[f.paused.Slug+"/.mcp.json"]), "Paused Two", "a manual sync publishes the paused plugin's pending changes")

	status, err := f.ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.NotNil(t, status.UpToDate)
	require.True(t, *status.UpToDate)

	// The plugin is still paused, but nothing is pending, so the next
	// automatic publish has nothing to do.
	again := f.automaticPublish(t, ctx)
	require.True(t, again.Skipped)
}

// A publish requested as a manual sync from outside the dashboard, such as the
// Platform MCP's republish, sends a paused plugin's pending changes the way
// Sync changes does, and stays fingerprint-gated.
func TestAutoSync_RequestedManualSyncPublishesHeldChanges(t *testing.T) {
	t.Parallel()

	ctx, f := newAutoSyncFixture(t, "requested")
	_, err := f.setAutoSync(ctx, f.paused, false)
	require.NoError(t, err)
	f.addServer(t, ctx, f.paused, "requested-paused-two", "Paused Two", 1)
	require.True(t, f.automaticPublish(t, ctx).Skipped, "only the paused plugin changed, so nothing publishes automatically")

	sync := func() *plugins.PublishProjectResult {
		t.Helper()
		result, err := f.ti.service.PublishProject(ctx, plugins.PublishProjectInput{
			ProjectID: f.projectID, CreatedByUserID: f.userID, CommitMessage: "Update plugin packages",
			SkipIfUnchanged: true, IncludePausedPlugins: true,
		})
		require.NoError(t, err)
		return result
	}
	require.False(t, sync().Skipped, "the paused plugin's pending change makes the sync publish")
	require.Contains(t, string(f.mock.lastPushedFiles[f.paused.Slug+"/.mcp.json"]), "Paused Two")

	status, err := f.ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.NotNil(t, status.UpToDate)
	require.True(t, *status.UpToDate)
	require.True(t, sync().Skipped, "with nothing pending, a requested sync stays fingerprint-gated")
}

// RequireAutoSyncCanTurnOff is UpdatePlugin's check for callers that change
// only auto sync, and its refusal is recognisable outside the management API.
func TestAutoSync_RequireAutoSyncCanTurnOffMatchesUpdatePlugin(t *testing.T) {
	t.Parallel()

	ctx, f := newAutoSyncFixture(t, "guard")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	pluginID := uuid.MustParse(f.paused.ID)
	check := func() error {
		return f.ti.service.RequireAutoSyncCanTurnOff(ctx, authCtx.ActiveOrganizationID, f.projectID, pluginID)
	}
	require.NoError(t, check(), "nothing is unsynced right after a publish")

	f.addServer(t, ctx, f.paused, "guard-paused-two", "Paused Two", 1)
	err := check()
	require.ErrorIs(t, err, plugins.ErrPluginHasUnsyncedChanges)
	requireFailedPrecondition(t, err, "This plugin has changes that haven't been synced yet.")
	_, err = f.setAutoSync(ctx, f.paused, false)
	require.ErrorIs(t, err, plugins.ErrPluginHasUnsyncedChanges, "the dashboard refuses for the same reason")

	_, err = f.ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.NoError(t, check())
	_, err = f.setAutoSync(ctx, f.paused, false)
	require.NoError(t, err)

	// Already off, it needs no check, whatever is pending.
	f.addServer(t, ctx, f.paused, "guard-paused-three", "Paused Three", 2)
	require.NoError(t, check())
}

// When nothing in the MCP component changed but the hooks regenerate, the MCP
// carry must still keep the held plugin's published files, which are copied by
// directory rather than by the paths its live state would generate.
func TestAutoSync_HooksOnlyPublishCarriesHeldPlugin(t *testing.T) {
	t.Parallel()

	ctx, f := newAutoSyncFixture(t, "hooks")
	_, err := f.setAutoSync(ctx, f.paused, false)
	require.NoError(t, err)
	f.addServer(t, ctx, f.paused, "hooks-paused-two", "Paused Two", 1)
	publishedMCP := filesWithoutSubstring(f.mock.lastPushedFiles, "-observability")
	for _, shared := range sharedMarketplaceFiles {
		delete(publishedMCP, shared)
	}
	orgID := publishOrgID(t, ctx, f.ti.conn, f.projectID)
	consumerKeysBefore := countPluginMCPKeysForOrg(t, ctx, f.ti, orgID)

	// A pending hooks generator bump for an org the rollout has cleared.
	rewindPublishedHooksVersion(t, ctx, f.ti.conn, f.projectID, "0")
	features := &feature.InMemory{}
	features.SetFlagPayload(feature.FlagHooksRollout, orgID, []byte(`{"version": 9999}`))
	publisher := newTestPluginPublisher(t, f.ti, f.mock, features)

	result, err := publisher.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID: f.projectID, CreatedByUserID: f.userID, CommitMessage: "hooks only", SkipIfUnchanged: true,
	})
	require.NoError(t, err)
	require.False(t, result.Skipped, "the hooks bump must publish")

	pushed := f.mock.lastPushedFiles
	for path, content := range publishedMCP {
		require.Equal(t, content, string(pushed[path]), "MCP file %s must be carried verbatim", path)
	}
	require.NotContains(t, string(pushed[f.paused.Slug+"/.mcp.json"]), "Paused Two")
	require.Equal(t, consumerKeysBefore, countPluginMCPKeysForOrg(t, ctx, f.ti, orgID), "carrying MCP must not mint a consumer key")
}

// A paused plugin that was never published stays out of automatic publishes,
// including the first one, until a manual sync includes it.
func TestAutoSync_NeverPublishedPausedPluginIsOmitted(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	f := newAutoSyncFixtureFor(t, ctx, ti, mock)

	off := false
	paused, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Never Published", AutoSyncEnabled: &off})
	require.NoError(t, err)
	f.addServer(t, ctx, paused, "omit-paused", "Omitted Server", 0)
	active, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Published Normally"})
	require.NoError(t, err)
	f.addServer(t, ctx, active, "omit-active", "Active Server", 0)

	// The first publish is automatic and forced, as for a new marketplace.
	first, err := ti.service.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID: f.projectID, CreatedByUserID: f.userID, CommitMessage: "Initial marketplace publish", SkipIfUnchanged: false,
	})
	require.NoError(t, err)
	require.False(t, first.Skipped)
	require.Empty(t, pluginFilesOf(mock.lastPushedFiles, paused.Slug), "a never-published paused plugin must not be published automatically")
	require.NotEmpty(t, pluginFilesOf(mock.lastPushedFiles, active.Slug))
	require.NotContains(t, claudeMarketplaceEntries(t, mock.lastPushedFiles), paused.Slug)
	require.NotContains(t, f.storedFingerprints(t, ctx), paused.Slug)

	// Later automatic publishes keep leaving it out.
	f.addServer(t, ctx, active, "omit-active-two", "Active Server Two", 1)
	second := f.automaticPublish(t, ctx)
	require.False(t, second.Skipped)
	require.Empty(t, pluginFilesOf(mock.lastPushedFiles, paused.Slug))

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.Contains(t, string(mock.lastPushedFiles[paused.Slug+"/.mcp.json"]), "Omitted Server", "a manual sync includes the paused plugin")
	require.Contains(t, claudeMarketplaceEntries(t, mock.lastPushedFiles), paused.Slug)
	require.Contains(t, f.storedFingerprints(t, ctx), paused.Slug)
}

// When a held plugin's published files can't be read back from the repo, the
// publish falls back to the plugin's live state rather than dropping it from
// the marketplace, or listing a package that can't install.
func TestAutoSync_UnreadablePublishedFilesFallBackToLiveState(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		missing string
	}{
		{name: "claude plugin.json", missing: "%s/.claude-plugin/plugin.json"},
		{name: "cursor mcp.json", missing: "cursor-plugins/%s-cursor/mcp.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, f := newAutoSyncFixture(t, "fallback")
			_, err := f.setAutoSync(ctx, f.paused, false)
			require.NoError(t, err)
			f.addServer(t, ctx, f.paused, "fallback-paused-two", "Paused Two", 1)
			f.addServer(t, ctx, f.active, "fallback-active-two", "Active Two", 1)

			missing := fmt.Sprintf(tc.missing, f.paused.Slug)
			repo := maps.Clone(f.mock.lastPushedFiles)
			require.Contains(t, repo, missing)
			delete(repo, missing)
			f.mock.repoFiles = repo

			result := f.automaticPublish(t, ctx)
			require.False(t, result.Skipped)
			pushed := f.mock.lastPushedFiles
			require.Contains(t, string(pushed[f.paused.Slug+"/.mcp.json"]), "Paused Two", "an unreadable held plugin publishes its live state")
			require.NotEmpty(t, pushed[missing], "the live state restores the missing file")
			require.Contains(t, claudeMarketplaceEntries(t, pushed), f.paused.Slug)
		})
	}
}

// A held plugin's Codex marketplace entry keeps asking for authentication on
// install when its published Codex package collects a credential, whichever
// field carries it.
func TestAutoSync_HeldPluginKeepsCodexInstallAuthentication(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		codexMCP string
	}{
		{name: "api key from the environment", codexMCP: `{"mcpServers":{"Paused One":{"url":"https://example.com/mcp","bearer_token_env_var":"SPEAKEASY_AI_API_KEY"}}}`},
		{name: "env headers", codexMCP: `{"mcpServers":{"Paused One":{"url":"https://example.com/mcp","env_http_headers":{"X-Vendor-Key":"VENDOR_KEY"}}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, f := newAutoSyncFixture(t, "codexauth")
			_, err := f.setAutoSync(ctx, f.paused, false)
			require.NoError(t, err)
			f.addServer(t, ctx, f.paused, "codexauth-paused-two", "Paused Two", 1)
			f.addServer(t, ctx, f.active, "codexauth-active-two", "Active Two", 1)

			// Publishes bake the consumer key into the Codex package, so stand
			// in a published package that collects a credential instead.
			codexMCPPath := f.paused.Slug + "-codex/.mcp.json"
			repo := maps.Clone(f.mock.lastPushedFiles)
			repo[codexMCPPath] = []byte(tc.codexMCP)
			f.mock.repoFiles = repo

			result := f.automaticPublish(t, ctx)
			require.False(t, result.Skipped)
			pushed := f.mock.lastPushedFiles
			require.Equal(t, tc.codexMCP, string(pushed[codexMCPPath]), "the held Codex package must be copied verbatim")
			authentication := codexMarketplaceAuthentication(t, pushed)
			require.Equal(t, "ON_INSTALL", authentication[f.paused.Slug+"-codex"])
			require.Equal(t, "ON_USE", authentication[f.active.Slug+"-codex"], "a package with the key baked in installs without prompting")
		})
	}
}

func TestAutoSync_TurnOffRefusedWithUnsyncedChanges(t *testing.T) {
	t.Parallel()

	ctx, f := newAutoSyncFixture(t, "refuse")
	f.addServer(t, ctx, f.paused, "refuse-paused-two", "Paused Two", 1)

	_, err := f.setAutoSync(ctx, f.paused, false)
	requireFailedPrecondition(t, err, "This plugin has changes that haven't been synced yet. Sync changes first, then turn off auto sync.")
	got, err := f.ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: f.paused.ID})
	require.NoError(t, err)
	require.True(t, got.AutoSyncEnabled, "a refused update leaves auto sync on")

	// Turning it off together with an edit would strand that edit too.
	off := false
	_, err = f.ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{
		ID: f.paused.ID, Name: "Renamed While Pausing", Slug: f.paused.Slug, Description: f.paused.Description, AutoSyncEnabled: &off,
	})
	requireFailedPrecondition(t, err, "Turn off auto sync on its own")

	_, err = f.ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	updated, err := f.setAutoSync(ctx, f.paused, false)
	require.NoError(t, err, "a plugin with nothing unsynced can turn auto sync off")
	require.False(t, updated.AutoSyncEnabled)

	// Turning it off again is a no-op, whatever is pending.
	f.addServer(t, ctx, f.paused, "refuse-paused-three", "Paused Three", 2)
	_, err = f.setAutoSync(ctx, f.paused, false)
	require.NoError(t, err)
}

// Turning auto sync off waits for a change to the plugin that is still
// committing, and the check then sees it. Otherwise the change would commit
// unseen between the check and the write, and every automatic publish after
// would hold it with nothing on screen saying so.
func TestAutoSync_TurnOffWaitsForAChangeStillCommitting(t *testing.T) {
	t.Parallel()

	ctx, f := newAutoSyncFixture(t, "inflight")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	plugin, err := f.ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: f.paused.ID})
	require.NoError(t, err)
	require.Len(t, plugin.Servers, 1)
	server := plugin.Servers[0]

	// The change renames the plugin's server the way UpdatePluginServer does,
	// taking no project lock before it requests a publication, and stops
	// short of committing.
	change := testenv.BeginTx(t, ctx, f.ti.conn)
	_, err = pluginsrepo.New(change).UpdatePluginServer(ctx, pluginsrepo.UpdatePluginServerParams{
		ID: uuid.MustParse(server.ID), PluginID: uuid.MustParse(f.paused.ID), DisplayName: "Renamed In Flight", Policy: server.Policy, SortOrder: server.SortOrder,
	})
	require.NoError(t, err)
	require.NoError(t, plugins.PublicationRequests{Enabled: false}.Project(ctx, change, authCtx.ActiveOrganizationID, f.projectID, f.userID))

	turnOff := make(chan error, 1)
	go func() {
		_, err := f.setAutoSync(ctx, f.paused, false)
		turnOff <- err
	}()
	waitRoleSetupBlocked(t, ctx, f.ti, int32(change.Conn().PgConn().PID()))
	require.NoError(t, change.Commit(ctx))

	// Generous next to the check's milliseconds, so only a stuck lock fails it.
	const turnOffDeadline = 10 * time.Second
	select {
	case err := <-turnOff:
		require.ErrorIs(t, err, plugins.ErrPluginHasUnsyncedChanges, "the check must see the change that committed while it waited")
	case <-time.After(turnOffDeadline):
		t.Fatal("turning auto sync off never finished")
	}
	got, err := f.ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: f.paused.ID})
	require.NoError(t, err)
	require.True(t, got.AutoSyncEnabled, "a refused update leaves auto sync on")
}

// A rename the marketplace hasn't picked up yet leaves the published files
// under the old slug. Pausing then would drop the plugin from the marketplace,
// so it counts as an unsynced change.
func TestAutoSync_TurnOffRefusedWithUnsyncedRename(t *testing.T) {
	t.Parallel()

	ctx, f := newAutoSyncFixture(t, "rename")
	renamed, err := f.ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{
		ID: f.paused.ID, Name: f.paused.Name, Slug: "rename-moved", Description: f.paused.Description,
	})
	require.NoError(t, err)

	_, err = f.setAutoSync(ctx, renamed, false)
	requireFailedPrecondition(t, err, "This plugin has changes that haven't been synced yet.")

	_, err = f.ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	_, err = f.setAutoSync(ctx, renamed, false)
	require.NoError(t, err)
}

func TestAutoSync_TurnOffAllowedBeforeFirstPublish(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "No Marketplace Yet"})
	require.NoError(t, err)

	off := false
	updated, err := ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: plugin.Name, Slug: plugin.Slug, AutoSyncEnabled: &off})
	require.NoError(t, err)
	require.False(t, updated.AutoSyncEnabled)
}

func TestAutoSync_TurnOnSignalsPublish(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	off := false
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Resumes Syncing", AutoSyncEnabled: &off})
	require.NoError(t, err)
	signalled := len(ti.publisher.captured())

	on := true
	updated, err := ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: plugin.Name, Slug: plugin.Slug, AutoSyncEnabled: &on})
	require.NoError(t, err)
	require.True(t, updated.AutoSyncEnabled)

	captured := ti.publisher.captured()
	require.Len(t, captured, signalled+1, "turning auto sync on must enqueue a publish for the held changes")
	require.Equal(t, *authCtx.ProjectID, captured[len(captured)-1].projectID)
}

// A held plugin is matched to its published files by slug, so the slug can't
// change while auto sync is off.
func TestAutoSync_SlugChangeRefusedWhileOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	off := false
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Slug Locked", AutoSyncEnabled: &off})
	require.NoError(t, err)

	_, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: plugin.Name, Slug: "slug-moved"})
	requireFailedPrecondition(t, err, "slug can't change while auto sync is off")

	// Renaming without moving the slug is fine.
	renamed, err := ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: "Slug Locked Renamed", Slug: plugin.Slug})
	require.NoError(t, err)
	require.Equal(t, "Slug Locked Renamed", renamed.Name)
	require.False(t, renamed.AutoSyncEnabled, "omitting auto_sync_enabled keeps the current setting")

	// Turning auto sync back on in the same update frees the slug.
	on := true
	moved, err := ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: renamed.Name, Slug: "slug-moved", AutoSyncEnabled: &on})
	require.NoError(t, err)
	require.Equal(t, "slug-moved", moved.Slug)
	require.True(t, moved.AutoSyncEnabled)
}

func countPluginMCPKeysForOrg(t *testing.T, ctx context.Context, ti *testInstance, orgID string) int {
	t.Helper()
	keys, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, orgID)
	require.NoError(t, err)
	return countPluginMCPKeys(keys)
}

// publishStatusBySlug maps each plugin's slug to its reported freshness.
func publishStatusBySlug(t *testing.T, status *gen.PublishStatusResult) map[string]gen.PluginPublishStatus {
	t.Helper()
	out := make(map[string]gen.PluginPublishStatus, len(status.Plugins))
	for _, plugin := range status.Plugins {
		out[plugin.Slug] = *plugin
	}
	return out
}

// Freshness is per plugin. A paused plugin's held change makes only that
// plugin wait for a manual sync: plugins whose auto sync is on still read as
// current, and the marketplace reports no automatic publish pending, on the
// dashboard and through the Platform MCP's evidence alike.
func TestAutoSync_HeldChangeLeavesOtherPluginsCurrent(t *testing.T) {
	t.Parallel()

	ctx, f := newAutoSyncFixture(t, "fresh")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := f.setAutoSync(ctx, f.paused, false)
	require.NoError(t, err)
	f.addServer(t, ctx, f.paused, "fresh-paused-two", "Paused Two", 1)

	status, err := f.ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.Equal(t, new(false), status.UpToDate, "the held change still needs a manual sync")
	require.Equal(t, new(false), status.AutomaticSyncPending, "no automatic publish would change anything")
	require.Equal(t, map[string]gen.PluginPublishStatus{
		f.active.Slug: {Slug: f.active.Slug, UpToDate: true, AwaitingManualSync: false},
		f.paused.Slug: {Slug: f.paused.Slug, UpToDate: false, AwaitingManualSync: true},
	}, publishStatusBySlug(t, status))

	evidence, err := f.ti.service.ResolvePublicationEvidence(ctx, authCtx.ActiveOrganizationID, f.projectID, []string{f.active.Slug, f.paused.Slug})
	require.NoError(t, err)
	fresh := make(map[string]*bool, len(evidence))
	for _, plugin := range evidence {
		fresh[plugin.PluginSlug] = plugin.Fresh
	}
	require.Equal(t, map[string]*bool{f.active.Slug: new(true), f.paused.Slug: new(false)}, fresh)

	// The held plugin's published files only change with a publish, so a
	// repeated read answers from the cache instead of the repo.
	f.mock.getRepoFilesCalled = false
	_, err = f.ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.False(t, f.mock.getRepoFilesCalled)

	// A change to the auto-synced plugin is what an automatic publish sends.
	f.addServer(t, ctx, f.active, "fresh-active-two", "Active Two", 1)
	status, err = f.ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.Equal(t, new(true), status.AutomaticSyncPending)
	require.Equal(t, map[string]gen.PluginPublishStatus{
		f.active.Slug: {Slug: f.active.Slug, UpToDate: false, AwaitingManualSync: false},
		f.paused.Slug: {Slug: f.paused.Slug, UpToDate: false, AwaitingManualSync: true},
	}, publishStatusBySlug(t, status))

	// Once that publish runs, only the paused plugin still waits.
	f.automaticPublish(t, ctx)
	status, err = f.ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.Equal(t, new(false), status.AutomaticSyncPending)
	require.True(t, publishStatusBySlug(t, status)[f.active.Slug].UpToDate)
	require.True(t, publishStatusBySlug(t, status)[f.paused.Slug].AwaitingManualSync)
}

// When the held plugin's published files can't be read, freshness degrades to
// comparing its live state instead of failing the status read.
func TestAutoSync_FreshnessSurvivesAnUnreadableRepo(t *testing.T) {
	t.Parallel()

	ctx, f := newAutoSyncFixture(t, "unreadable")
	_, err := f.setAutoSync(ctx, f.paused, false)
	require.NoError(t, err)
	f.addServer(t, ctx, f.paused, "unreadable-paused-two", "Paused Two", 1)
	f.mock.getRepoFilesErr = errors.New("rate limited")

	status, err := f.ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.Equal(t, new(false), status.UpToDate)
	require.True(t, publishStatusBySlug(t, status)[f.paused.Slug].AwaitingManualSync)
}
