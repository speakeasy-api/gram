// servepublic_pluginscopedkeys_test.go verifies that API keys bound to a
// specific toolset (rfc-plugin-scoped-keys.md) are accepted for that
// toolset's MCP endpoint and rejected for any other.
package mcp_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// createPluginScopedAPIKey inserts a system-managed API key bound to a
// single toolset and returns the plaintext bearer token.
func createPluginScopedAPIKey(t *testing.T, ctx context.Context, ti *testInstance, toolsetID uuid.UUID) string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tokenBytes := make([]byte, 32)
	_, err := rand.Read(tokenBytes)
	require.NoError(t, err)
	token := hex.EncodeToString(tokenBytes)
	fullKey := "gram_local_" + token

	hash := sha256.Sum256([]byte(fullKey))
	keyHash := hex.EncodeToString(hash[:])

	_, err = ti.conn.Exec(ctx, `
		INSERT INTO api_keys (
			organization_id, project_id, created_by_user_id,
			name, key_prefix, key_hash, scopes,
			toolset_id, system_managed
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true)
	`,
		authCtx.ActiveOrganizationID,
		*authCtx.ProjectID,
		authCtx.UserID,
		"plugin:test:"+toolsetID.String()[:8],
		"gram_local_"+token[:5],
		keyHash,
		[]string{"consumer"},
		toolsetID,
	)
	require.NoError(t, err)

	return fullKey
}

func TestServePublic_PluginScopedKey_AllowsMatchingToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "scoped-match-"+uuid.NewString()[:8])
	bearer := createPluginScopedAPIKey(t, ctx, ti, toolset.ID)

	w, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "scoped key against its bound toolset must succeed")
}

func TestServePublic_PluginScopedKey_RejectsMismatchedToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	boundToolset := createPrivateMCPToolset(t, ctx, ti, "scoped-bound-"+uuid.NewString()[:8])
	otherToolset := createPrivateMCPToolset(t, ctx, ti, "scoped-other-"+uuid.NewString()[:8])

	// Key is bound to boundToolset; request hits otherToolset → must reject.
	bearer := createPluginScopedAPIKey(t, ctx, ti, boundToolset.ID)

	_, err := servePublicHTTP(t, context.Background(), ti, otherToolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.Error(t, err)

	// Must surface as Forbidden (not the generic "expired or invalid access
	// token" Unauthorized that the private-MCP wrapper used to mask
	// everything as) so middleware maps it to HTTP 403 and the client can
	// distinguish "your credential is bad" from "your credential is good
	// but pointed at the wrong server".
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code, "scope mismatch must surface as Forbidden")
}
