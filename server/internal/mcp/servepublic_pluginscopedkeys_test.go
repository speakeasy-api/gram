// servepublic_pluginscopedkeys_test.go verifies that API keys bound to a
// specific toolset (rfc-plugin-scoped-keys.md) are accepted for that
// toolset's MCP endpoint and rejected for any other. Scoping is enforced
// via an mcp:connect grant on the api_key principal — the same RBAC
// engine call that gates session-authenticated requests.
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

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// createPluginScopedAPIKey inserts a system-managed API key plus an
// mcp:connect principal_grants row binding it to a single toolset.
// Returns the plaintext bearer token. Mirrors what the plugin publish
// path does (plugins/impl.go: persistPluginAPIKeys).
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

	var keyID uuid.UUID
	err = ti.conn.QueryRow(ctx, `
		INSERT INTO api_keys (
			organization_id, project_id, created_by_user_id,
			name, key_prefix, key_hash, scopes, system_managed
		) VALUES ($1, $2, $3, $4, $5, $6, $7, true)
		RETURNING id
	`,
		authCtx.ActiveOrganizationID,
		*authCtx.ProjectID,
		authCtx.UserID,
		"plugin:test:"+toolsetID.String()[:8],
		"gram_local_"+token[:5],
		keyHash,
		[]string{"consumer"},
	).Scan(&keyID)
	require.NoError(t, err)

	selector := authz.NewSelector(authz.ScopeMCPConnect, toolsetID.String())
	selectorJSON, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = ti.conn.Exec(ctx, `
		INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
		VALUES ($1, $2, $3, $4::jsonb)
	`,
		authCtx.ActiveOrganizationID,
		urn.NewPrincipal(urn.PrincipalTypeAPIKey, keyID.String()).String(),
		string(authz.ScopeMCPConnect),
		selectorJSON,
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
