package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestDashboardInspectOAuthClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	// Use the normal public-server consent flow to exercise all legs without
	// involving an external identity provider.
	testenv.SetHostedMCP(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, testenv.HostedMCP{Visibility: "public"})

	const clientID = "speakeasy-dashboard-inspect"
	redirectURI := ti.serverURL.ResolveReference(&url.URL{Path: "/inspect/oauth/callback"}).String()
	verifier := pkceVerifier(t)
	params := usersessions_repo.GetUserSessionClientByClientIDParams{UserSessionIssuerID: issuer.ID, ClientID: clientID}
	queries := usersessions_repo.New(ti.conn)

	mcpSlug := toolset.McpSlug.String
	route := func(method, suffix, body string) *http.Request {
		req := httptest.NewRequest(method, "/mcp/"+mcpSlug+suffix, strings.NewReader(body))
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", mcpSlug)
		return req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	}
	authorize := func(callback string) *httptest.ResponseRecorder {
		q := url.Values{
			"client_id": {clientID}, "redirect_uri": {callback}, "response_type": {"code"},
			"state": {"dashboard-state"}, "code_challenge_method": {"S256"}, "code_challenge": {pkceChallenge(verifier)},
		}
		w := httptest.NewRecorder()
		require.NoError(t, ti.service.HandleAuthorize(w, route(http.MethodGet, "/authorize?"+q.Encode(), "")))
		return w
	}

	for _, bad := range []string{
		"https://untrusted.example/inspect/oauth/callback",
		redirectURI + "?next=https://untrusted.example",
		redirectURI + "#",
		strings.TrimSuffix(redirectURI, "/callback") + "/other",
	} {
		w := authorize(bad)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Empty(t, w.Header().Get("Location"))
	}
	_, err := queries.GetUserSessionClientByClientID(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows, "invalid redirects must not provision a client")

	w := authorize(redirectURI)
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/mcp/"+mcpSlug+"/connect", loc.Path)
	stateID := loc.Query().Get("state")
	require.NotEmpty(t, stateID)
	client, err := queries.GetUserSessionClientByClientID(ctx, params)
	require.NoError(t, err)
	require.Contains(t, client.RedirectUris, redirectURI)
	require.Equal(t, oauthwire.AuthMethodNone, client.TokenEndpointAuthMethod.String)
	require.False(t, client.ClientSecretHash.Valid)

	w = httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(w, route(http.MethodGet, "/connect?state="+stateID, "")))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "Speakeasy Dashboard Inspect")

	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	require.False(t, stored.FirstParty, "the dashboard client uses OAuth consent, not the first-party connect shortcut")
	form := url.Values{"state": {stateID}, "csrf_token": {stored.CSRFToken}, "action": {"approve"}}
	w = httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(w, route(http.MethodPost, "/connect", form.Encode())))
	require.Equal(t, http.StatusSeeOther, w.Code)
	loc, err = url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)
	require.Equal(t, "dashboard-state", loc.Query().Get("state"))
	loc.RawQuery = ""
	require.Equal(t, redirectURI, loc.String())

	form = url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI},
		"client_id": {clientID}, "code_verifier": {verifier},
	}
	w = httptest.NewRecorder()
	require.NoError(t, ti.service.HandleToken(w, route(http.MethodPost, "/token", form.Encode())))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var tokenBody struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tokenBody))
	require.NotEmpty(t, tokenBody.AccessToken)
}
