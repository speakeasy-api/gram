package plugins_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
)

func downloadClaudeWebPlugin(t *testing.T, ctx context.Context, ti *testInstance) (map[string][]byte, string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	archive, err := ti.service.ClaudeWebObservabilityPlugin(ctx, authCtx.ActiveOrganizationID, authCtx.OrganizationSlug, *authCtx.ProjectID, *authCtx.ProjectSlug, authCtx.UserID)
	require.NoError(t, err)

	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)
	files := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)
		body, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		files[f.Name] = body
	}

	var cfg struct {
		HooksAPIKey string `json:"hooks_api_key"`
	}
	require.NoError(t, json.Unmarshal(files["speakeasy.json"], &cfg))
	require.NotEmpty(t, cfg.HooksAPIKey)
	return files, cfg.HooksAPIKey
}

func TestClaudeWebObservabilityPlugin_FlatPluginWithMemberOwnedHooksKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	files, fullKey := downloadClaudeWebPlugin(t, ctx, ti)

	for _, name := range []string{".claude-plugin/plugin.json", "hooks/hooks.json", "hooks/bootstrap.sh", "speakeasy.json"} {
		require.Contains(t, files, name, "the plugin sits at the archive root")
	}

	hash, err := auth.GetAPIKeyHash(fullKey)
	require.NoError(t, err)
	key, err := keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, hash)
	require.NoError(t, err)
	require.Equal(t, []string{"hooks"}, key.Scopes)
	require.Equal(t, authCtx.UserID, key.CreatedByUserID, "ingest attributes the key's events to its owner")
	require.Equal(t, *authCtx.ProjectID, key.ProjectID.UUID)
	require.False(t, auth.IsOrgWidePluginHooksAPIKey(key.Name, fullKey, key.KeyPrefix), "a member's key must not be treated as the shared org key")
}

func TestClaudeWebObservabilityPlugin_RemintRevokesPreviousKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)

	_, first := downloadClaudeWebPlugin(t, ctx, ti)
	_, second := downloadClaudeWebPlugin(t, ctx, ti)

	firstHash, err := auth.GetAPIKeyHash(first)
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, firstHash)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the earlier download's key is revoked")

	secondHash, err := auth.GetAPIKeyHash(second)
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, secondHash)
	require.NoError(t, err, "the latest download's key stays live")
}

// liveClaudeWebKeys counts the member's unrevoked Claude web hooks keys.
func liveClaudeWebKeys(t *testing.T, ctx context.Context, ti *testInstance) int {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	var n int
	err := ti.conn.QueryRow(ctx, `SELECT count(*) FROM api_keys
		WHERE organization_id = $1 AND created_by_user_id = $2 AND deleted IS FALSE
		  AND name LIKE 'plugins-claude-web-hooks-%'`, authCtx.ActiveOrganizationID, authCtx.UserID).Scan(&n)
	require.NoError(t, err)
	return n
}

// Racing downloads used to each miss the other's uncommitted key and both
// stay live; the per-member lock leaves exactly one.
func TestClaudeWebObservabilityPlugin_ConcurrentDownloadsLeaveOneLiveKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	const n = 8
	errs := make(chan error, n)
	for range n {
		go func() {
			_, err := ti.service.ClaudeWebObservabilityPlugin(ctx, authCtx.ActiveOrganizationID, authCtx.OrganizationSlug, *authCtx.ProjectID, *authCtx.ProjectSlug, authCtx.UserID)
			errs <- err
		}()
	}
	for range n {
		require.NoError(t, <-errs)
	}
	require.Equal(t, 1, liveClaudeWebKeys(t, ctx, ti))
}

// A key whose name merely looks minted, without the matching key_prefix, was
// not created by this flow and must survive a re-mint.
func TestClaudeWebObservabilityPlugin_RemintSparesLookalikeNames(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	lookalike, err := keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		CreatedByUserID: authCtx.UserID,
		Name:            "plugins-claude-web-hooks-20261102-120102-abc123",
		KeyPrefix:       "gram_live_zzzzz",
		KeyHash:         "lookalike-hash",
		Scopes:          []string{"hooks"},
	})
	require.NoError(t, err)

	downloadClaudeWebPlugin(t, ctx, ti)

	var deleted bool
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT deleted FROM api_keys WHERE id = $1`, lookalike.ID).Scan(&deleted))
	require.False(t, deleted, "a user-chosen name is never revoked")
}
