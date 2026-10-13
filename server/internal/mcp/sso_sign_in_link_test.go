package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/ssosignins"
	ssosigninsrepo "github.com/speakeasy-api/gram/server/internal/ssosignins/repo"
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
	ssosignins.EnableForTests(ti.features)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mock.exchangeResult.OrganizationID = workOSOrganizationID(t, ctx, ti, toolset.OrganizationID)

	rotated := runSSOLinkIDPCallback(t, ctx, ti, toolset)

	signIn, err := ssosignins.LiveSignInForSession(ctx, ti.conn, toolset.OrganizationID, gramUserID, workosSessionID)
	require.NoError(t, err)
	require.True(t, signIn.Valid, "the callback records the sign-in in the endpoint's organization")
	require.NotNil(t, rotated.SSOSignInID)
	require.Equal(t, signIn.UUID, *rotated.SSOSignInID)
}

func TestHandleIDPCallback_EnforcementOffRecordsButLeavesTheGrantUnlinked(t *testing.T) {
	t.Parallel()

	gramUserID := "user-" + uuid.New().String()[:8]
	workosSessionID := "session_" + uuid.NewString()
	mock := &mockIdentityResolver{
		exchangeResult: &identity.IDPUserInfo{
			Sub:                  "workos-user-flag-off",
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
	mock.exchangeResult.OrganizationID = workOSOrganizationID(t, ctx, ti, toolset.OrganizationID)

	rotated := runSSOLinkIDPCallback(t, ctx, ti, toolset)

	signIn, err := ssosignins.LiveSignInForSession(ctx, ti.conn, toolset.OrganizationID, gramUserID, workosSessionID)
	require.NoError(t, err)
	require.True(t, signIn.Valid, "recording stays on")
	require.Nil(t, rotated.SSOSignInID, "issuance is unchanged while enforcement is off")
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
	require.True(t, signIn.ID.Valid)

	tokens := exchangeLinkedCode(t, ctx, ti, toolset, client, userID, &signIn.ID.UUID)
	session := sessionForAccessToken(t, ctx, ti, toolset, tokens.AccessToken)
	require.Equal(t, signIn.ID, session.SsoSignInID, "the authorization code links the session")

	refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, tokens.RefreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
	var rotated linkedTokens
	require.NoError(t, json.Unmarshal([]byte(refreshed.body), &rotated))
	refreshedSession := sessionForAccessToken(t, ctx, ti, toolset, rotated.AccessToken)
	require.NotEqual(t, session.ID, refreshedSession.ID)
	require.Equal(t, signIn.ID, refreshedSession.SsoSignInID, "refresh rotation carries the link forward")
}

func TestHandleToken_LegacyGrantStaysUnlinkedAndWorks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)

	tokens := exchangeLinkedCode(t, ctx, ti, toolset, client, "legacy-user-"+uuid.NewString(), nil)
	session := sessionForAccessToken(t, ctx, ti, toolset, tokens.AccessToken)
	require.False(t, session.SsoSignInID.Valid)

	result, err := ssosignins.CheckSignInIdentity(ctx, ti.conn, toolset.OrganizationID, session.ID, time.Now(), 0)
	require.NoError(t, err)
	require.Equal(t, ssosignins.ReasonNotLinked, result.Reason)

	refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, tokens.RefreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
	var rotated linkedTokens
	require.NoError(t, json.Unmarshal([]byte(refreshed.body), &rotated))
	rotatedSession := sessionForAccessToken(t, ctx, ti, toolset, rotated.AccessToken)
	require.False(t, rotatedSession.SsoSignInID.Valid, "refresh never attaches a sign-in to a legacy grant")
	again, err := ssosignins.CheckSignInIdentity(ctx, ti.conn, toolset.OrganizationID, rotatedSession.ID, time.Now(), 0)
	require.NoError(t, err)
	require.Equal(t, ssosignins.ReasonNotLinked, again.Reason)
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

	w := redeemLinkedCode(t, ti, toolset, client, code, verifier)
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

func redeemLinkedCode(t *testing.T, ti *testInstance, toolset toolsets_repo.Toolset, client usersessions_repo.UserSessionClient, code, verifier string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", client.RedirectUris[0])
	form.Set("client_id", client.ClientID)
	form.Set("code_verifier", verifier)
	return postForm(t, ti, toolset.McpSlug.String, "token", form)
}

func storeLinkedGrant(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, client usersessions_repo.UserSessionClient, userID string, signInID *uuid.UUID) (code, verifier string) {
	t.Helper()

	verifier = "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	code = "code-" + uuid.NewString()
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
	return code, verifier
}

func workOSOrganizationID(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) string {
	t.Helper()

	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, organizationID)
	require.NoError(t, err)
	require.True(t, org.WorkosID.Valid)
	return org.WorkosID.String
}

func seedWorkOSOrganization(t *testing.T, ctx context.Context, ti *testInstance) (organizationID, workosID string) {
	t.Helper()

	organizationID = "org-" + uuid.NewString()
	workosID = "org_workos_" + uuid.NewString()
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:             organizationID,
		Name:           "Other org",
		Slug:           "other-" + uuid.NewString()[:8],
		WorkosID:       conv.ToPGText(workosID),
		Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
		CreationSource: pgtype.Text{String: "", Valid: false},
		DefaultHost:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return organizationID, workosID
}

func TestHandleIDPCallback_OrganizationAttributionFailsClosed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		orgFn func(t *testing.T, ctx context.Context, ti *testInstance) (workosOrgID string, recordedIn string)
	}{
		{name: "WorkOS selected another organization", orgFn: func(t *testing.T, ctx context.Context, ti *testInstance) (string, string) {
			t.Helper()
			other, workosID := seedWorkOSOrganization(t, ctx, ti)
			return workosID, other
		}},
		{name: "WorkOS organization unknown here", orgFn: func(t *testing.T, ctx context.Context, ti *testInstance) (string, string) {
			t.Helper()
			return "org_unknown_" + uuid.NewString(), ""
		}},
		{name: "no WorkOS organization", orgFn: func(t *testing.T, ctx context.Context, ti *testInstance) (string, string) {
			t.Helper()
			return "", ""
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gramUserID := "user-" + uuid.New().String()[:8]
			workosSessionID := "session_" + uuid.NewString()
			mock := &mockIdentityResolver{
				exchangeResult: &identity.IDPUserInfo{Sub: "workos-user-x", Email: "test@example.com", Name: "Test User", WorkOSSessionID: workosSessionID, AuthenticationMethod: ssosignins.MethodSSO},
				upsertResult:   gramUserID,
				hasAccessOK:    true,
			}
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
			toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
			workosOrgID, otherOrg := tc.orgFn(t, ctx, ti)
			mock.exchangeResult.OrganizationID = workosOrgID

			rotated := runSSOLinkIDPCallback(t, ctx, ti, toolset)
			require.Nil(t, rotated.SSOSignInID, "the grant stays unlinked")
			for _, org := range []string{toolset.OrganizationID, otherOrg} {
				if org == "" {
					continue
				}
				recorded, err := ssosignins.LiveSignInForSession(ctx, ti.conn, org, gramUserID, workosSessionID)
				require.NoError(t, err)
				require.False(t, recorded.Valid, "nothing is recorded in %s", org)
			}
		})
	}
}

func TestHandleConsent_LinksOnlyTheAuthorizersOwnSession(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		sameAsAuthor bool
		wantLinked   bool
	}{
		{name: "authorizer's own session", sameAsAuthor: true, wantLinked: true},
		{name: "state without an authorizer", sameAsAuthor: false, wantLinked: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			subject := urn.NewUserSubject("consent-user-" + uuid.NewString())
			signIn := uuid.New()
			authorizer := ""
			if tc.sameAsAuthor {
				authorizer = subject.ID
			}
			stateID := uuid.NewString()
			csrfToken := "csrf-" + uuid.NewString()
			require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
				ID:                  stateID,
				UserSessionIssuerID: issuer.ID,
				Endpoint:            mcp.EndpointRef{McpSlug: toolset.McpSlug.String, CustomDomainID: toolset.CustomDomainID},
				ClientID:            client.ClientID,
				RedirectURI:         client.RedirectUris[0],
				State:               "client-state",
				CodeChallenge:       "abc",
				CodeChallengeMethod: "S256",
				CSRFToken:           csrfToken,
				Subject:             &subject,
				AuthorizerUserID:    authorizer,
				SSOSignInID:         &signIn,
				CreatedAt:           time.Now(),
			}))
			form := url.Values{"state": {stateID}, "csrf_token": {csrfToken}, "action": {"approve"}}
			req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/connect", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
			req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
			w := httptest.NewRecorder()
			require.NoError(t, ti.service.HandleConsent(w, req))
			require.Equal(t, http.StatusSeeOther, w.Code)
			loc, err := url.Parse(w.Header().Get("Location"))
			require.NoError(t, err)

			grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
			grant, err := grantCache.Get(ctx, "userSessionGrant:"+issuer.ID.String()+":"+loc.Query().Get("code"))
			require.NoError(t, err)
			if tc.wantLinked {
				require.NotNil(t, grant.SSOSignInID)
				require.Equal(t, signIn, *grant.SSOSignInID)
			} else {
				require.Nil(t, grant.SSOSignInID)
			}
		})
	}
}

func TestHandleToken_RefusesCodeFromSignInRevokedAfterConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	userID := "revoked-user-" + uuid.NewString()
	signIn, err := ssosignins.Record(ctx, ti.conn, ssosignins.RecordParams{OrganizationID: toolset.OrganizationID, UserID: userID, WorkOSSessionID: "session_" + uuid.NewString(), AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: time.Now()})
	require.NoError(t, err)
	code, verifier := storeLinkedGrant(t, ctx, ti, toolset, client, userID, &signIn.ID.UUID)

	_, err = ssosignins.RevokeUser(ctx, ti.conn, []string{toolset.OrganizationID}, userID, ssosignins.RevokeReasonAdmin)
	require.NoError(t, err)

	w := redeemLinkedCode(t, ti, toolset, client, code, verifier)
	require.Equal(t, http.StatusBadRequest, w.Code, "a code consented before an admin revocation is never redeemed")
	require.Contains(t, w.Body.String(), "invalid_grant")
}

func TestHandleToken_RefusesCodeAfterSignOutThenBroadRevocation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	userID := "upgrade-user-" + uuid.NewString()
	workosSessionID := "session_" + uuid.NewString()
	signIn, err := ssosignins.Record(ctx, ti.conn, ssosignins.RecordParams{OrganizationID: toolset.OrganizationID, UserID: userID, WorkOSSessionID: workosSessionID, AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: time.Now()})
	require.NoError(t, err)
	code, verifier := storeLinkedGrant(t, ctx, ti, toolset, client, userID, &signIn.ID.UUID)

	_, err = ssosignins.RevokeSession(ctx, ti.conn, []string{toolset.OrganizationID}, userID, workosSessionID)
	require.NoError(t, err)
	_, err = ssosignins.RevokeUser(ctx, ti.conn, []string{toolset.OrganizationID}, userID, ssosignins.RevokeReasonSignOutEverywhere)
	require.NoError(t, err)

	w := redeemLinkedCode(t, ti, toolset, client, code, verifier)
	require.Equal(t, http.StatusBadRequest, w.Code, "a broad revocation also covers a sign-in already signed out")
	require.Contains(t, w.Body.String(), "invalid_grant")
}

func TestHandleToken_RefreshRefusesARevokedSignIn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	userID := "refresh-user-" + uuid.NewString()
	signIn, err := ssosignins.Record(ctx, ti.conn, ssosignins.RecordParams{OrganizationID: toolset.OrganizationID, UserID: userID, WorkOSSessionID: "session_" + uuid.NewString(), AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: time.Now()})
	require.NoError(t, err)
	tokens := exchangeLinkedCode(t, ctx, ti, toolset, client, userID, &signIn.ID.UUID)

	// The sign-in revocation committed but the grant survived, as a refresh
	// racing the revocation would see it.
	_, err = ssosigninsrepo.New(ti.conn).RevokeUserSSOSignIns(ctx, ssosigninsrepo.RevokeUserSSOSignInsParams{
		RevokeReason:    conv.ToPGText(string(ssosignins.RevokeReasonAdmin)),
		OrganizationIds: []string{toolset.OrganizationID},
		UserID:          userID,
	})
	require.NoError(t, err)

	refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, tokens.RefreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusBadRequest, refreshed.code, refreshed.body)
	require.Contains(t, refreshed.body, "invalid_grant")
}

func TestHandleToken_RefreshAfterSingleSignOutKeepsWorking(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	userID := "signout-user-" + uuid.NewString()
	workosSessionID := "session_" + uuid.NewString()
	signIn, err := ssosignins.Record(ctx, ti.conn, ssosignins.RecordParams{OrganizationID: toolset.OrganizationID, UserID: userID, WorkOSSessionID: workosSessionID, AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: time.Now()})
	require.NoError(t, err)
	tokens := exchangeLinkedCode(t, ctx, ti, toolset, client, userID, &signIn.ID.UUID)

	_, err = ssosignins.RevokeSession(ctx, ti.conn, []string{toolset.OrganizationID}, userID, workosSessionID)
	require.NoError(t, err)

	refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, tokens.RefreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
	var rotated linkedTokens
	require.NoError(t, json.Unmarshal([]byte(refreshed.body), &rotated))
	session := sessionForAccessToken(t, ctx, ti, toolset, rotated.AccessToken)
	require.Equal(t, signIn.ID, session.SsoSignInID)
	result, err := ssosignins.CheckSignInIdentity(ctx, ti.conn, toolset.OrganizationID, session.ID, time.Now(), 0)
	require.NoError(t, err)
	require.Equal(t, ssosignins.ReasonSignInRevoked, result.Reason, "the grant works but is never vouched for")
}
