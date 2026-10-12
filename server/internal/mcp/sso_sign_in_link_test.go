package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/ssosignins"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestHandleIDPCallback_RecordsSSOSignInOnChallenge(t *testing.T) {
	t.Parallel()

	gramUserID := "user-" + uuid.New().String()[:8]
	workosSessionID := "session_" + uuid.NewString()
	mock := &mockIdentityResolver{
		exchangeResult: &identity.IDPUserInfo{
			Sub:                  "workos-user-sso",
			Email:                "test@example.com",
			Name:                 "Test User",
			WorkOSSessionID:      workosSessionID,
			AuthenticationMethod: ssosignins.MethodSSO,
		},
		upsertResult: gramUserID,
		hasAccessOK:  true,
	}
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	rotated := runSSOLinkIDPCallback(t, ctx, ti, toolset)

	signIn, err := ssosignins.LiveSignInForSession(ctx, ti.conn, toolset.OrganizationID, gramUserID, workosSessionID)
	require.NoError(t, err)
	require.True(t, signIn.Valid, "the callback records the sign-in in the endpoint's organization")
	require.NotNil(t, rotated.SSOSignInID)
	require.Equal(t, signIn.UUID, *rotated.SSOSignInID)
}

func TestHandleIDPCallback_NoWorkOSSessionLeavesChallengeUnlinked(t *testing.T) {
	t.Parallel()

	mock := &mockIdentityResolver{
		exchangeResult: &identity.IDPUserInfo{Sub: "workos-user-nosid", Email: "test@example.com", Name: "Test User"},
		upsertResult:   "user-" + uuid.New().String()[:8],
		hasAccessOK:    true,
	}
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	rotated := runSSOLinkIDPCallback(t, ctx, ti, toolset)
	require.Nil(t, rotated.SSOSignInID)
}

func TestHandleToken_LinksSessionToSSOSignInAndRefreshCarriesIt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	userID := "linked-user-" + uuid.NewString()
	signIn, err := ssosignins.Record(ctx, ti.conn, ssosignins.RecordParams{
		OrganizationID:       toolset.OrganizationID,
		UserID:               userID,
		WorkOSSessionID:      "session_" + uuid.NewString(),
		AuthenticationMethod: ssosignins.MethodSSO,
		AuthenticatedAt:      time.Now(),
	})
	require.NoError(t, err)
	require.True(t, signIn.Valid)

	tokens := exchangeLinkedCode(t, ctx, ti, toolset, client, userID, &signIn.UUID)
	session := sessionForAccessToken(t, ctx, ti, toolset, tokens.AccessToken)
	require.Equal(t, signIn, session.SsoSignInID, "the authorization code links the session")

	refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, tokens.RefreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
	var rotated linkedTokens
	require.NoError(t, json.Unmarshal([]byte(refreshed.body), &rotated))
	refreshedSession := sessionForAccessToken(t, ctx, ti, toolset, rotated.AccessToken)
	require.NotEqual(t, session.ID, refreshedSession.ID)
	require.Equal(t, signIn, refreshedSession.SsoSignInID, "refresh rotation carries the link forward")
}

func TestHandleToken_LegacyGrantStaysUnlinkedAndWorks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)

	tokens := exchangeLinkedCode(t, ctx, ti, toolset, client, "legacy-user-"+uuid.NewString(), nil)
	session := sessionForAccessToken(t, ctx, ti, toolset, tokens.AccessToken)
	require.False(t, session.SsoSignInID.Valid)

	result, err := ssosignins.CheckSignInIdentity(ctx, ti.conn, toolset.OrganizationID, session.ID, time.Now())
	require.NoError(t, err)
	require.Equal(t, ssosignins.ReasonNotLinked, result.Reason)

	refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, tokens.RefreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
}

type linkedTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func runSSOLinkIDPCallback(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset) mcp.AuthnChallengeState {
	t.Helper()

	challengeID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint:            mcp.EndpointRef{McpSlug: toolset.McpSlug.String, CustomDomainID: toolset.CustomDomainID},
		ClientID:            "test-client",
		RedirectURI:         "http://localhost:3000/callback",
		State:               "client-state",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		CreatedAt:           time.Now(),
	}))

	mcpSlug := toolset.McpSlug.String
	q := url.Values{"state": {challengeID}, "code": {"idp-auth-code-" + uuid.NewString()}}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/idp_callback?"+q.Encode(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleIDPCallback(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	consentURL, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	rotated, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+consentURL.Query().Get("state"))
	require.NoError(t, err)
	return rotated
}

func exchangeLinkedCode(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, client usersessions_repo.UserSessionClient, userID string, signInID *uuid.UUID) linkedTokens {
	t.Helper()

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	code := "code-" + uuid.NewString()
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
		Code:                        code,
		FlowID:                      "",
		UserSessionIssuerID:         toolset.UserSessionIssuerID.UUID,
		UserSessionClientID:         client.ID,
		ClientID:                    client.ClientID,
		RedirectURI:                 client.RedirectUris[0],
		CodeChallenge:               base64.RawURLEncoding.EncodeToString(sum[:]),
		CodeChallengeMethod:         "S256",
		Subject:                     urn.NewUserSubject(userID),
		DesiredSessionDurationHours: 0,
		ToolSelection:               nil,
		SSOSignInID:                 signInID,
		CreatedAt:                   time.Now(),
	}))

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", client.RedirectUris[0])
	form.Set("client_id", client.ClientID)
	form.Set("code_verifier", verifier)
	w := postForm(t, ti, toolset.McpSlug.String, "token", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var tokens linkedTokens
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tokens))
	require.NotEmpty(t, tokens.RefreshToken)
	return tokens
}

func sessionForAccessToken(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, accessToken string) usersessions_repo.UserSession {
	t.Helper()

	claims, err := sessiontokens.NewSigner("test-jwt-secret").Validate(accessToken, urn.NewToolset(toolset.ID).String())
	require.NoError(t, err)
	session, err := usersessions_repo.New(ti.conn).GetUserSessionByJTI(ctx, usersessions_repo.GetUserSessionByJTIParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Jti:                 claims.ID,
	})
	require.NoError(t, err)
	require.Equal(t, conv.ToPGText(toolset.OrganizationID), session.OrganizationID)
	return session
}
