package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// TestServePublicOAuth_ProxyNoSecurityDefs_NoToken_Returns401 is the exact
// regression scenario from the Mar 31 – Apr 2 incident. A public toolset with
// OAuth proxy configured but no per-tool security annotations must return 401
// with WWW-Authenticate when no token is provided.
func TestServePublicOAuth_ProxyNoSecurityDefs_NoToken_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:     "proxy-nosec",
		IsPublic: true,
	})

	mcpSlug := result.Toolset.McpSlug.String
	_, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized", "should return unauthorized when no token provided for OAuth-configured server")
}

// TestServePublicOAuth_ExternalNoSecurityDefs_NoToken_Returns401 verifies the
// same behavior for external OAuth servers (ExternalOauthServerID).
func TestServePublicOAuth_ExternalNoSecurityDefs_NoToken_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "ext-nosec",
		IsPublic: true,
	})

	mcpSlug := result.Toolset.McpSlug.String
	_, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized", "should return unauthorized when no token provided for external OAuth server")
}

// TestServePublicOAuth_ProxyNoSecurityDefs_ValidToken_Succeeds verifies that a
// public toolset with OAuth proxy allows access when a valid token is provided.
func TestServePublicOAuth_ProxyNoSecurityDefs_ValidToken_Succeeds(t *testing.T) {
	t.Parallel()

	mockOAuth := &mockOAuthService{
		validateFunc: func(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*oauth.Token, error) {
			expiresAt := time.Now().Add(24 * time.Hour)
			return &oauth.Token{
				ToolsetID:   toolsetId,
				AccessToken: accessToken,
				ExternalSecrets: []oauth.ExternalSecret{{
					SecurityKeys: []string{},
					Token:        "upstream-token",
					RefreshToken: "",
					ExpiresAt:    &expiresAt,
				}},
				RefreshToken: "",
				TokenType:    "",
				Scope:        "",
				CreatedAt:    time.Time{},
				ExpiresAt:    time.Time{},
			}, nil
		},
		refreshFunc: nil,
	}

	ctx, ti := newTestMCPServiceWithOAuth(t, mockOAuth)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:     "proxy-nosec-tok",
		IsPublic: true,
	})

	mcpSlug := result.Toolset.McpSlug.String
	w, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "valid-oauth-token", nil)
	// The request may fail later (e.g. no active deployment), but it must NOT
	// fail with "unauthorized" — the security check should pass.
	if err != nil {
		require.NotContains(t, err.Error(), "unauthorized", "should not return unauthorized when valid token provided")
	}
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "should not send WWW-Authenticate when valid token provided")
}

// TestServePublicOAuth_ExternalNoSecurityDefs_ValidToken_Succeeds verifies that
// a public toolset with external OAuth allows access when a token is provided.
func TestServePublicOAuth_ExternalNoSecurityDefs_ValidToken_Succeeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "ext-nosec-tok",
		IsPublic: true,
	})

	mcpSlug := result.Toolset.McpSlug.String
	// External OAuth flow passes the bearer token through without validation
	// via mockOAuthService — it's collected as-is in tokenInputs.
	w, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "some-external-token", nil)
	require.NoError(t, err)
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "should not send WWW-Authenticate when token provided")
}

// TestServePublicOAuth_NoOAuth_NoSecurityDefs_Succeeds verifies that a public
// toolset without OAuth and without security annotations succeeds without any
// credentials (baseline behavior).
func TestServePublicOAuth_NoOAuth_NoSecurityDefs_Succeeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "no-oauth-nosec-"+uuid.New().String()[:8])

	mcpSlug := toolset.McpSlug.String
	_, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err, "public MCP without OAuth should succeed without credentials")
}
