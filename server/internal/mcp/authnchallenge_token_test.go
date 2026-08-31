package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TestHandleTokenCode_ResourceIndicator covers the RFC 8707 check on the
// authorization_code grant: a resource naming another server is invalid_target,
// the advertised identifier is accepted, and an absent parameter stays legal
// for clients predating MCP 2026-07-28.
func TestHandleTokenCode_ResourceIndicator(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mcpSlug := toolset.McpSlug.String

	advertisedResource, _ := fetchAdvertisedIssuer(t, ctx, ti, mcpSlug)

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	redirectURI := "http://127.0.0.1:51423/callback"

	// Codes are single-use, so each redemption gets a freshly stored grant.
	redeem := func(resource string) *httptest.ResponseRecorder {
		code := "code-" + uuid.NewString()
		require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
			Code:                        code,
			FlowID:                      "",
			UserSessionIssuerID:         toolset.UserSessionIssuerID.UUID,
			UserSessionClientID:         client.ID,
			ClientID:                    client.ClientID,
			RedirectURI:                 redirectURI,
			CodeChallenge:               base64.RawURLEncoding.EncodeToString(sum[:]),
			CodeChallengeMethod:         "S256",
			Subject:                     urn.NewUserSubject("code-user-" + uuid.NewString()),
			DesiredSessionDurationHours: 0,
			ToolSelection:               nil,
			CreatedAt:                   time.Now(),
		}))

		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", code)
		form.Set("redirect_uri", redirectURI)
		form.Set("client_id", client.ClientID)
		form.Set("code_verifier", verifier)
		if resource != "" {
			form.Set("resource", resource)
		}
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug+"/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", mcpSlug)
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		require.NoError(t, ti.service.HandleToken(w, req))
		return w
	}

	w := redeem("https://someone-else.example.com/mcp/" + mcpSlug)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid_target")

	// Same server, wrong route base: /mcp and /x/mcp mint different audiences,
	// so they are never interchangeable resource identifiers.
	w = redeem(strings.Replace(advertisedResource, "/mcp/", "/x/mcp/", 1))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid_target")

	w = redeem(advertisedResource + "/")
	require.Equal(t, http.StatusBadRequest, w.Code, "a trailing slash is not the advertised identifier")

	w = redeem(advertisedResource)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")

	w = redeem("")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")
}

func TestHandleTokenAuthorizationCodeIsSingleUse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)

	first := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, verifier))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	second := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, verifier))
	require.Equal(t, http.StatusBadRequest, second.Code)
	require.Contains(t, second.Body.String(), "invalid_grant")
}

func TestHandleTokenWrongEndpointDoesNotBurnAuthorizationCode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	correct, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, toolset.McpSlug.String, "mcp")
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	other := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "sibling-"+uuid.NewString()[:8])
	other, err = toolsets_repo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true}, Slug: other.Slug, ProjectID: other.ProjectID,
	})
	require.NoError(t, err)

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	code := "code-" + uuid.NewString()
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	ref, err := correct.EndpointRef(ctx, ti.conn, ti.serverURL.String())
	require.NoError(t, err)
	require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
		Code: code, UserSessionIssuerID: issuer.ID, UserSessionClientID: client.ID,
		ClientID: client.ClientID, RedirectURI: client.RedirectUris[0],
		CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]), CodeChallengeMethod: "S256",
		Subject: urn.NewUserSubject("endpoint-user-" + uuid.NewString()), Endpoint: &ref, CreatedAt: time.Now(),
	}))

	wrong := postForm(t, ti, other.McpSlug.String, "token", codeGrantForm(client, code, verifier))
	require.Equal(t, http.StatusBadRequest, wrong.Code)
	require.Contains(t, wrong.Body.String(), "invalid_grant")
	legitimate := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, verifier))
	require.Equal(t, http.StatusOK, legitimate.Code, legitimate.Body.String())
}

func TestHandleTokenBadPKCEBurnsAuthorizationCode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)

	bad := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, "wrong-verifier"))
	require.Equal(t, http.StatusBadRequest, bad.Code)
	require.Contains(t, bad.Body.String(), "invalid_grant")
	legitimate := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, verifier))
	require.Equal(t, http.StatusBadRequest, legitimate.Code)
	require.Contains(t, legitimate.Body.String(), "invalid_grant")
}

// TestHandleTokenRefresh_ResourceIndicator asserts the same check on the
// refresh grant, which MCP 2026-07-28 clients also send `resource` on.
func TestHandleTokenRefresh_ResourceIndicator(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mcpSlug := toolset.McpSlug.String

	advertisedResource, _ := fetchAdvertisedIssuer(t, ctx, ti, mcpSlug)

	w := postRefreshGrant(t, ti, mcpSlug, client.ClientID,
		seedUserSessionWithSelection(t, ctx, ti, issuer.ID, client.ID, nil),
		"https://someone-else.example.com/mcp/"+mcpSlug)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid_target")

	w = postRefreshGrant(t, ti, mcpSlug, client.ClientID,
		seedUserSessionWithSelection(t, ctx, ti, issuer.ID, client.ID, nil),
		advertisedResource)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")

	w = postRefreshGrant(t, ti, mcpSlug, client.ClientID,
		seedUserSessionWithSelection(t, ctx, ti, issuer.ID, client.ID, nil), "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")
}
