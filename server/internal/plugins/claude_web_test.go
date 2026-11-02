package plugins_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

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
