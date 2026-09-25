package plugins_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func rotateObservabilityPayload(fate string) *gen.RotateObservabilityCredentialPayload {
	return &gen.RotateObservabilityCredentialPayload{
		PreviousKeyFate:  fate,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	}
}

// listHooksKeys returns the project's live (unexpired) observability hooks keys,
// newest first -- the set a rotation retires.
func listHooksKeys(t *testing.T, ctx context.Context, conn *pgxpool.Pool) []keysrepo.ApiKey {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	all, err := keysrepo.New(conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)

	var keys []keysrepo.ApiKey
	for _, key := range all {
		if !key.ProjectID.Valid || key.ProjectID.UUID != *authCtx.ProjectID {
			continue
		}
		if !strings.HasPrefix(key.Name, "plugins-hooks-") {
			continue
		}
		if key.ExpiresAt.Valid && !key.ExpiresAt.Time.After(time.Now()) {
			continue
		}
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b keysrepo.ApiKey) int {
		return b.CreatedAt.Time.Compare(a.CreatedAt.Time)
	})

	return keys
}

func setKeyExpiry(t *testing.T, ctx context.Context, conn *pgxpool.Pool, keyHash string, expiresAt time.Time) {
	t.Helper()

	require.NoError(t, testrepo.New(conn).SetAPIKeyExpiresAtFixture(ctx, testrepo.SetAPIKeyExpiresAtFixtureParams{
		ExpiresAt: conv.ToPGTimestamptz(expiresAt),
		KeyHash:   keyHash,
	}))
}

func TestRotateObservabilityCredential_RevokeImmediately(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, _, err := ti.service.DownloadObservabilityPlugin(ctx, &gen.DownloadObservabilityPluginPayload{Platform: "claude"})
	require.NoError(t, err)

	before := listHooksKeys(t, ctx, ti.conn)
	require.Len(t, before, 1)

	createBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyCreate)
	require.NoError(t, err)
	revokeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.NoError(t, err)
	require.NotEmpty(t, result.Key)
	require.True(t, strings.HasPrefix(result.Key, result.KeyPrefix))
	require.Equal(t, "revoke_immediately", result.PreviousKeyFate)
	require.Len(t, result.PreviousKeys, 1)
	require.Equal(t, before[0].ID.String(), result.PreviousKeys[0].ID)
	require.Nil(t, result.PreviousKeysExpireAt)
	require.False(t, result.MarketplaceRepublished)
	require.NotNil(t, result.MarketplaceUpdateDeferred)
	require.False(t, *result.MarketplaceUpdateDeferred)

	after := listHooksKeys(t, ctx, ti.conn)
	require.Len(t, after, 1)
	require.Equal(t, result.KeyPrefix, after[0].KeyPrefix)
	require.NotEqual(t, before[0].ID, after[0].ID)

	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, before[0].KeyHash)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the revoked key must stop authenticating")

	createAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyCreate)
	require.NoError(t, err)
	revokeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)
	require.Equal(t, createBefore+1, createAfter)
	require.Equal(t, revokeBefore+1, revokeAfter)
}

func TestRotateObservabilityCredential_GraceExpiresPreviousKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, _, err := ti.service.DownloadObservabilityPlugin(ctx, &gen.DownloadObservabilityPluginPayload{Platform: "claude"})
	require.NoError(t, err)

	before := listHooksKeys(t, ctx, ti.conn)
	require.Len(t, before, 1)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.Equal(t, "grace", result.PreviousKeyFate)
	require.Len(t, result.PreviousKeys, 1)
	require.NotNil(t, result.PreviousKeysExpireAt)
	expiresAt, err := time.Parse(time.RFC3339, *result.PreviousKeysExpireAt)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().UTC().Add(7*24*time.Hour), expiresAt, time.Minute)

	// A scoped key carrying an expiry must keep authenticating until that expiry;
	// this is the whole point of the grace window.
	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, before[0].KeyHash)
	require.NoError(t, err, "the previous key must stay valid during the grace window")

	setKeyExpiry(t, ctx, ti.conn, before[0].KeyHash, time.Now().UTC().Add(-time.Second))

	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, before[0].KeyHash)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the key must stop authenticating once the grace window closes")
}

func TestRotateObservabilityCredential_GraceDoesNotReviveExpiredKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, _, err := ti.service.DownloadObservabilityPlugin(ctx, &gen.DownloadObservabilityPluginPayload{Platform: "claude"})
	require.NoError(t, err)

	keys := listHooksKeys(t, ctx, ti.conn)
	require.Len(t, keys, 1)
	expired := keys[0]
	setKeyExpiry(t, ctx, ti.conn, expired.KeyHash, time.Now().UTC().Add(-time.Hour))

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.Empty(t, result.PreviousKeys, "an already-expired key is not a previous key to retire")
	require.Nil(t, result.PreviousKeysExpireAt)

	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, expired.KeyHash)
	require.ErrorIs(t, err, pgx.ErrNoRows, "a grace rotation must not hand an expired key a new window")
}

func TestRotateObservabilityCredential_ConcurrentRotationsRetireOriginalKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, _, err := ti.service.DownloadObservabilityPlugin(ctx, &gen.DownloadObservabilityPluginPayload{Platform: "claude"})
	require.NoError(t, err)

	original := listHooksKeys(t, ctx, ti.conn)
	require.Len(t, original, 1)

	type rotation struct {
		result *gen.RotateObservabilityCredentialResult
		err    error
	}
	results := make(chan rotation, 2)
	for range 2 {
		go func() {
			result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
			results <- rotation{result: result, err: err}
		}()
	}
	for range 2 {
		got := <-results
		require.NoError(t, got.err)
		require.NotEmpty(t, got.result.Key)
	}

	// Whichever rotation applied its fate last wins the "newest key" race, but
	// the credential that existed before either of them must be gone.
	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, original[0].KeyHash)
	require.ErrorIs(t, err, pgx.ErrNoRows, "overlapping rotations must not leave the pre-rotation key valid")

	remaining := listHooksKeys(t, ctx, ti.conn)
	require.NotEmpty(t, remaining, "a rotation must leave a usable hooks credential behind")
	for _, key := range remaining {
		require.NotEqual(t, original[0].ID, key.ID)
	}
}

func TestRotateObservabilityCredential_ForbiddenWithoutOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	ctx = authz.GrantsToContext(ctx, nil)

	_, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestRotateObservabilityCredential_InvalidFate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("never"))
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestRotateObservabilityCredential_NoPreviousKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.NoError(t, err)
	require.Empty(t, result.PreviousKeys)
	require.NotEmpty(t, result.Key)
	require.NotEmpty(t, result.KeyPrefix)
}

func TestRotateObservabilityCredential_DoesNotTouchConsumerKeys(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	publishTestObservabilityProject(t, ctx, ti, "rotate-consumer")

	keysBefore, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	var mcpKeyID string
	for _, key := range keysBefore {
		if strings.HasPrefix(key.Name, "plugins-mcp-") {
			mcpKeyID = key.ID.String()
		}
	}
	require.NotEmpty(t, mcpKeyID)
	mcpCountBefore := countPluginMCPKeys(keysBefore)

	_, err = ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.NoError(t, err)

	keysAfter, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	var mcpStillPresent bool
	for _, key := range keysAfter {
		if key.ID.String() == mcpKeyID {
			mcpStillPresent = true
		}
	}
	require.True(t, mcpStillPresent, "consumer MCP keys must survive observability rotation")
	require.Equal(t, mcpCountBefore, countPluginMCPKeys(keysAfter), "rotation must not mint a replacement consumer MCP key")
}

func TestRotateObservabilityCredential_RepublishesMarketplaceWhenEligible(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	features := &feature.InMemory{}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, features)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	features.SetFlagPayload(feature.FlagHooksRollout, authCtx.ActiveOrganizationID, []byte(`{"version": 9999}`))

	publishTestObservabilityProject(t, ctx, ti, "rotate-publish")

	claudeObservability, _ := orgObservabilitySlugs(t, ctx, ti)
	hooksBefore := publishedHooksAPIKey(t, mock, claudeObservability)
	require.NotEmpty(t, hooksBefore)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.True(t, result.MarketplaceRepublished)
	require.NotNil(t, result.MarketplaceUpdateDeferred)
	require.False(t, *result.MarketplaceUpdateDeferred)
	require.Contains(t, result.Key, result.KeyPrefix)

	hooksAfter := publishedHooksAPIKey(t, mock, claudeObservability)
	require.Equal(t, result.Key, hooksAfter, "the republished plugin must embed the returned credential")
	require.NotEqual(t, hooksBefore, hooksAfter)
}

func TestRotateObservabilityCredential_DefersMarketplaceWhenGated(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	publishTestObservabilityProject(t, ctx, ti, "rotate-gated")

	claudeObservability, _ := orgObservabilitySlugs(t, ctx, ti)
	hooksBefore := publishedHooksAPIKey(t, mock, claudeObservability)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.False(t, result.MarketplaceRepublished)
	require.NotNil(t, result.MarketplaceUpdateDeferred)
	require.True(t, *result.MarketplaceUpdateDeferred, "a published marketplace that kept the old credential must be reported")
	require.NotEqual(t, hooksBefore, result.Key)

	require.Equal(t, hooksBefore, publishedHooksAPIKey(t, mock, claudeObservability),
		"a gated org's marketplace must keep the previous credential")
}

// A hooks-only rotation carries the MCP component, so an MCP change made since
// the last publish must still be pending afterwards -- recording the live
// fingerprints would let the next publish skip that change.
func TestRotateObservabilityCredential_KeepsPendingMCPChangePending(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	features := &feature.InMemory{}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, features)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	features.SetFlagPayload(feature.FlagHooksRollout, authCtx.ActiveOrganizationID, []byte(`{"version": 9999}`))

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "rotate-pending"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "rotate-pending-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("rotate-pending server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	distributeTestSkill(t, ctx, ti, plugin.ID, "rotate-pending-skill")

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	// Change the plugin set without republishing.
	secondToolset := createTestToolset(t, ctx, ti.conn, "rotate-pending-toolset-2")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(secondToolset.ID.String()),
		DisplayName: conv.PtrEmpty("rotate-pending second server"),
		Policy:      "required",
		SortOrder:   1,
	})
	require.NoError(t, err)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.True(t, result.MarketplaceRepublished)

	status, err := ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.NotNil(t, status.UpToDate)
	require.False(t, *status.UpToDate, "the MCP change made before the rotation must still be pending")
}

// publishTestObservabilityProject gives the project a plugin, a skill, and a
// published marketplace, which is the state a credential rotation acts on.
func publishTestObservabilityProject(t *testing.T, ctx context.Context, ti *testInstance, name string) {
	t.Helper()

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: name})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, name+"-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty(name + " server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	distributeTestSkill(t, ctx, ti, plugin.ID, name+"-skill")

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
}

func publishedHooksAPIKey(t *testing.T, mock *mockGitHubPublisher, observabilitySlug string) string {
	t.Helper()

	raw, ok := mock.lastPushedFiles[observabilitySlug+"/speakeasy.json"]
	require.True(t, ok, "published marketplace must contain the observability plugin config")

	var config struct {
		HooksAPIKey string `json:"hooks_api_key"`
	}
	require.NoError(t, json.Unmarshal(raw, &config))

	return config.HooksAPIKey
}

func countPluginMCPKeys(keys []keysrepo.ApiKey) int {
	count := 0
	for _, key := range keys {
		if strings.HasPrefix(key.Name, "plugins-mcp-") {
			count++
		}
	}

	return count
}
