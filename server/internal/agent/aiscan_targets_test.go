package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func aiScanEnvelope(t *testing.T, poll *gen.GetPluginsResult) map[string]any {
	t.Helper()
	require.NotNil(t, poll.Configuration)
	envelope, ok := poll.Configuration.Config["ai_scan"].(map[string]any)
	require.True(t, ok, "plugin poll must carry the ai_scan envelope")
	return envelope
}

func TestGetPluginsServesAIScanCatalogToUnconfiguredOrgs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	poll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)

	require.False(t, poll.Configuration.IsConfigured)
	envelope := aiScanEnvelope(t, poll)
	require.EqualValues(t, aitargets.SchemaVersion, envelope["schema_version"])
	require.EqualValues(t, len(aitargets.Defaults()), envelope["list_version"])
	require.NotEmpty(t, envelope["etag"])
	targets, ok := envelope["targets"].([]any)
	require.True(t, ok)
	require.Len(t, targets, len(aitargets.Defaults()))
	first, ok := targets[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "aider", first["id"], "targets are served ordered by id")
	signatures, ok := first["signatures"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{"aider"}, signatures["binaries"])
}

func TestGetPluginsServesAIScanCatalogAlongsideAdminSettings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	_, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{
		Config: map[string]any{"ai_scan_interval_seconds": 21600},
	})
	require.NoError(t, err)

	poll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.True(t, poll.Configuration.IsConfigured)
	require.EqualValues(t, 21600, poll.Configuration.Config["ai_scan_interval_seconds"])
	aiScanEnvelope(t, poll)

	stored, err := ti.service.GetConfiguration(ctx, &gen.GetConfigurationPayload{})
	require.NoError(t, err)
	require.NotContains(t, stored.Config, "ai_scan")
}

func TestGetPluginsEtagMovesWithCatalogRevision(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	before, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)

	queries := repo.New(ti.conn)
	added := aitargets.Target{
		ID:          "chatgpt-classic",
		DisplayName: "ChatGPT Classic",
		Category:    aitargets.CategoryHarness,
		Signatures:  aitargets.Signatures{BundleIDs: []string{"com.openai.chat"}, Binaries: []string{}, ConfigDirs: []string{}, ProcessNames: []string{}},
		VersionHint: nil,
		Enabled:     true,
	}
	_, err = queries.UpsertAIScanTarget(ctx, aitargets.UpsertParams(added))
	require.NoError(t, err)
	version, err := aitargets.RecordRevision(ctx, queries, aitargets.Revision{
		TargetID: added.ID,
		Action:   aitargets.ActionUpsert,
		Actor:    aitargets.Actor{UserID: "user", Email: "admin@example.com"},
		Reason:   "test",
		Before:   nil,
		After:    &added,
	})
	require.NoError(t, err)
	ti.catalog.Invalidate()

	after, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.NotEqual(t, before.Etag, after.Etag, "a catalog change must move the poll etag")
	require.NotEqual(t, before.Configuration.Etag, after.Configuration.Etag)
	envelope := aiScanEnvelope(t, after)
	require.EqualValues(t, version, envelope["list_version"])
}

func TestUpdateConfigurationRejectsAIScanKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))
	ctx = withPlatformAdmin(t, ctx)

	_, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{
		Config: map[string]any{"ai_scan": map[string]any{"targets": []any{}}},
	})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeInvalid, shareableErr.Code)
}
