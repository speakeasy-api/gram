package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestConsentAgentBindingActionsUseRealAttachmentService(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	// This flow requires secure dashboard origins; the generic fixture uses
	// http://0.0.0.0, which is neither HTTPS nor a literal loopback redirect.
	ti.serverURL.Scheme, ti.serverURL.Host = "https", "app.example.com"
	ti.siteURL.Scheme, ti.siteURL.Host = "https", "app.example.com"
	fx := newAgentConsentFixture(t, ctx, ti)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	seedPrincipalGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, fx.userID), authz.ScopeProjectRead, fx.target.ProjectID.String())
	agent := createConsentAgent(t, ctx, ti, fx, "Reusable connection agent")
	grantID := seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)
	policy, policyErr := guardian.NewUnsafePolicy(ti.tracerProvider, []string{})
	require.NoError(t, policyErr)
	meterProvider := testenv.NewMeterProvider(t)
	refresher := remotesessions.NewRefreshService(ti.logger, meterProvider, ti.conn, ti.enc, policy, nil, ti.cacheAdapter)
	bindings := remotesessions.NewService(ti.logger, ti.tracerProvider, meterProvider, ti.conn, ti.sessionManager, ti.authzEngine, ti.enc, nil, policy, nil, ti.audit, ti.serverURL, remotesessions.NewIdentityCommitter(ti.logger, ti.conn, ti.enc, ti.audit, ti.serverURL, policy, nil, nil), refresher, nil)
	bindings.SetBindingAuthorizer(func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
		require.True(t, contextvalues.HasValidatedGramSession(ctx), "attachment management requires real session provenance")
		auth, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.Equal(t, fx.userID, auth.UserID)
		require.Equal(t, fx.orgID, auth.ActiveOrganizationID)
		require.Equal(t, fx.target.ProjectID, *auth.ProjectID)

		_, _, err := agentmanagement.NewAuthorizer(ti.authzEngine).RequireAgentOwnerForUpdate(ctx, tx, id, agentmanagement.OwnedAgentAuthorize)
		if err != nil {
			return fmt.Errorf("authorize attachment owner: %w", err)
		}
		return nil
	})
	ti.service.SetConsentBindingService(bindings)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, fx.toolset.McpSlug.String, "mcp")
	require.NoError(t, err)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, fx.target.ProjectID, fx.orgID, "consent-reuse", "", []uuid.UUID{fx.target.UserSessionIssuerID})
	cipher, err := ti.enc.Encrypt([]byte("upstream-placeholder"))
	require.NoError(t, err)
	createSession := func(subject urn.SessionSubject) uuid.UUID {
		session, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
			SubjectUrn: subject, UserSessionIssuerID: fx.target.UserSessionIssuerID,
			RemoteSessionClientID: clientID, AccessTokenEncrypted: cipher,
			AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{},
		})
		require.NoError(t, err)
		// Existing human OAuth sessions already store provider identity; the
		// agent consent surface must use that same canonical session view.
		_, err = repo.New(ti.conn).UpdateRemoteSessionIdentity(ctx, repo.UpdateRemoteSessionIdentityParams{
			ID: session.ID, SubjectUrn: subject, RemoteSessionClientID: clientID,
			ExpectedUpdatedAt:   session.UpdatedAt,
			UpstreamEmail:       pgtype.Text{String: "existing-account@example.com", Valid: true},
			UpstreamDisplayName: pgtype.Text{String: "Existing upstream account", Valid: true},
		})
		require.NoError(t, err)
		return session.ID
	}
	owned := createSession(urn.NewUserSubject(fx.userID))
	other := createSession(urn.NewUserSubject("another-subject"))
	token := ti.getSessionToken(ctx, t)
	callWithCookie := func(action string, cookie string, extra url.Values) (*httptest.ResponseRecorder, error) {
		form := url.Values{"state": {fx.stateID}, "csrf_token": {fx.csrf}, "agent_id": {agent.ID.String()}, "action": {action}}
		maps.Copy(form, extra)
		// A real consent request has no RPC-prepared authorization grants.
		req := httptest.NewRequest(http.MethodPost, "https://mcp.example.com/mcp/"+endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode())).WithContext(t.Context())
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: cookie})
		}
		w := httptest.NewRecorder()
		err := ti.service.ServeConsentAction(w, req, endpoint)
		if err != nil {
			return w, fmt.Errorf("serve consent action: %w", err)
		}
		return w, nil
	}

	call := func(action string, cookie bool, extra url.Values) (*httptest.ResponseRecorder, error) {
		localToken := ""
		if cookie {
			localToken = token
		}
		return callWithCookie(action, localToken, extra)
	}
	// Legacy same-origin consent can still use its cookie before confirmation.
	_, err = call("agent_connections", true, nil)
	require.NoError(t, err)

	mintState, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	mintState.Endpoint.BaseURL = "https://mcp.example.com"
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mintState))
	// The MCP origin has no dashboard cookie. Only explicit confirmation on
	// the canonical origin can bind the existing dashboard session to consent.
	_, err = call("agent_connections", false, nil)
	require.Error(t, err)
	start, err := call("agent_session_handoff", false, nil)
	require.NoError(t, err)
	marked, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	require.True(t, marked.ConsentSessionRequired)
	_, err = call("agent_connections", true, nil)
	require.Error(t, err, "once confirmation starts, a local cookie must not bypass it")
	var handoff struct {
		URL string `json:"url"`
	}
	require.NoError(t, json.Unmarshal(start.Body.Bytes(), &handoff))
	handoffURL, err := url.Parse(handoff.URL)
	require.NoError(t, err)
	require.Equal(t, ti.serverURL.Host, handoffURL.Host)
	require.NotContains(t, handoff.URL, token)
	confirm := func(method, target, session, csrf string) (*httptest.ResponseRecorder, error) {
		req := httptest.NewRequest(method, target, strings.NewReader(url.Values{"csrf_token": {csrf}}.Encode())).WithContext(t.Context())
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if session != "" {
			req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: session})
		}
		w := httptest.NewRecorder()
		return w, ti.service.HandleConsentSessionHandoff(w, req)
	}
	loginPage, err := confirm(http.MethodGet, handoff.URL, "", "")
	require.NoError(t, err)
	require.Contains(t, loginPage.Body.String(), "Open Gram to sign in")
	page, err := confirm(http.MethodGet, handoff.URL, token, "")
	require.NoError(t, err)
	require.NotContains(t, page.Body.String(), token)
	require.Equal(t, "no-store", page.Header().Get("Cache-Control"))
	csrfMatch := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	require.Len(t, csrfMatch, 2)
	_, err = call("agent_connections", false, nil)
	require.Error(t, err, "GET must not bind the dashboard session")
	_, err = confirm(http.MethodPost, handoff.URL, token, "wrong")
	require.Error(t, err)
	_, err = confirm(http.MethodPost, handoff.URL, "", csrfMatch[1])
	require.Error(t, err)
	foreignURL := *handoffURL
	foreignURL.Host = "mcp.example.com"
	_, err = confirm(http.MethodPost, foreignURL.String(), token, csrfMatch[1])
	require.Error(t, err, "handoff is canonical-origin only")
	crossOriginRequest := httptest.NewRequest(http.MethodPost, handoff.URL, strings.NewReader(url.Values{"csrf_token": {csrfMatch[1]}}.Encode())).WithContext(t.Context())
	crossOriginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	crossOriginRequest.Header.Set("Origin", "https://untrusted.example.com")
	crossOriginRequest.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: token})
	require.Error(t, ti.service.HandleConsentSessionHandoff(httptest.NewRecorder(), crossOriginRequest))
	originalSession, err := ti.sessionManager.GetSession(ctx, token)
	require.NoError(t, err)
	alternateToken, err := sessions.NewSessionID()
	require.NoError(t, err)
	alternateSession := originalSession
	alternateSession.SessionID = alternateToken
	require.NoError(t, ti.sessionManager.StoreSession(ctx, alternateSession))
	_, err = confirm(http.MethodPost, handoff.URL, alternateToken, csrfMatch[1])
	require.Error(t, err, "CSRF proof must bind the exact dashboard session")
	alternateSession.UserID = seedConsentMember(t, ctx, ti, fx.orgID)
	require.NoError(t, ti.sessionManager.StoreSession(ctx, alternateSession))
	wrongUserPage, err := confirm(http.MethodGet, handoff.URL, alternateToken, "")
	require.NoError(t, err)
	require.Contains(t, wrongUserPage.Body.String(), "Open Gram to sign in")
	_, err = confirm(http.MethodPost, handoff.URL, alternateToken, csrfMatch[1])
	require.Error(t, err, "different Gram user cannot confirm this challenge")
	// These cases mutate one shared integration fixture and must run sequentially.
	for name, mutate := range map[string]func(*sessions.Session){
		"different organization": func(s *sessions.Session) { s.ActiveOrganizationID = "another-organization" },
		"impersonation":          func(s *sessions.Session) { s.ImpersonatorEmail = "support@example.com" },
		"support session": func(s *sessions.Session) {
			s.SupportOrganizationID = fx.orgID
			s.SupportExpiresAt = time.Now().Add(time.Hour)
		},
	} {
		session := originalSession
		mutate(&session)
		require.NoError(t, ti.sessionManager.StoreSession(ctx, session), name)
		_, err := confirm(http.MethodPost, handoff.URL, token, csrfMatch[1])
		require.Error(t, err, "handoff rejects %s", name)
		require.NoError(t, ti.sessionManager.StoreSession(ctx, originalSession), name)
	}
	completed, err := confirm(http.MethodPost, handoff.URL, token, csrfMatch[1])
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, completed.Code)
	require.NotContains(t, completed.Header().Get("Location"), token)
	require.True(t, strings.HasPrefix(completed.Header().Get("Location"), "https://mcp.example.com/mcp/"), "return to the challenged mint origin, not the dashboard origin")
	_, err = confirm(http.MethodPost, handoff.URL, token, csrfMatch[1])
	require.Error(t, err, "confirmation is single use")
	// Expiring/evicting the shorter confirmation record must not silently
	// restore ambient-cookie authentication for this still-live challenge.
	// TypedCacheObject.fullKey appends ":" even for SuffixNone; this is the
	// physical adapter key, unlike the logical key passed to the typed cache.
	require.NoError(t, ti.cacheAdapter.Delete(ctx, "consentSession:"+fx.stateID+":"))
	_, err = call("agent_connections", true, nil)
	require.Error(t, err, "missing confirmation cannot fall back to a valid local cookie")
	_, err = call("agent_connections", false, nil)
	require.Error(t, err)
	required, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	require.True(t, required.ConsentSessionRequired)
	// A new explicit handoff can recover without weakening the original marker.
	renewed, err := call("agent_session_handoff", true, nil)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(renewed.Body.Bytes(), &handoff))
	newPage, err := confirm(http.MethodGet, handoff.URL, token, "")
	require.NoError(t, err)
	newCSRF := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(newPage.Body.String())
	require.Len(t, newCSRF, 2)
	_, err = confirm(http.MethodPost, handoff.URL, token, newCSRF[1])
	require.NoError(t, err)
	_, err = call("agent_connections", false, nil)
	require.NoError(t, err, "fresh confirmation restores the challenge-bound session")
	// A completed handoff wins over both a stale custom-origin cookie and a
	// valid cookie for a different Gram user.
	for name, localCookie := range map[string]string{
		"expired local cookie":  "expired-dashboard-session",
		"different user cookie": alternateToken,
	} {
		w, err := callWithCookie("agent_connections", localCookie, nil)
		require.NoError(t, err, name)
		require.Contains(t, w.Body.String(), owned.String(), name)
		require.NotContains(t, w.Body.String(), other.String(), name)
	}

	for _, flag := range []feature.Flag{feature.FlagAgentManagement, feature.FlagAgentIdentityCredentials} {
		ti.features.SetFlag(flag, fx.orgID, false)
		_, err := call("agent_connections", false, nil)
		var denied *oops.ShareableError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, oops.CodeNotFound, denied.Code)
		_, err = call("agent_connections", true, nil)
		require.ErrorAs(t, err, &denied)
		require.Equal(t, oops.CodeNotFound, denied.Code)
		ti.features.SetFlag(flag, fx.orgID, true)
	}

	// The cached callback result, not an arbitrary OIDC subject or posted user,
	// supplies the canonical human identity. All operations must fail closed.
	original, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*mcp.AuthnChallengeState){
		"missing authorizer": func(s *mcp.AuthnChallengeState) { s.AuthorizerUserID = "" },
		"foreign subject": func(s *mcp.AuthnChallengeState) {
			subject := urn.NewUserSubject("unmapped-oidc-subject")
			s.Subject = &subject
		},
		"unresolved subject": func(s *mcp.AuthnChallengeState) { s.Subject = nil },
		"nonhuman subject":   func(s *mcp.AuthnChallengeState) { subject := urn.NewAgentSubject(agent.ID); s.Subject = &subject },
		"legacy provenance":  func(s *mcp.AuthnChallengeState) { s.AuthorizerImpersonated = nil },
		"impersonation":      func(s *mcp.AuthnChallengeState) { value := true; s.AuthorizerImpersonated = &value },
		"missing target":     func(s *mcp.AuthnChallengeState) { s.AgentAuthorizationTarget = nil },
		"foreign organization": func(s *mcp.AuthnChallengeState) {
			target := *s.AgentAuthorizationTarget
			target.OrganizationID = "another-organization"
			s.AgentAuthorizationTarget = &target
		},
		"foreign project": func(s *mcp.AuthnChallengeState) {
			target := *s.AgentAuthorizationTarget
			target.ProjectID = uuid.New()
			s.AgentAuthorizationTarget = &target
		},
		"foreign issuer": func(s *mcp.AuthnChallengeState) { s.UserSessionIssuerID = uuid.New() },
		"foreign target issuer": func(s *mcp.AuthnChallengeState) {
			target := *s.AgentAuthorizationTarget
			target.UserSessionIssuerID = uuid.New()
			s.AgentAuthorizationTarget = &target
		},
		"foreign endpoint": func(s *mcp.AuthnChallengeState) { s.Endpoint.McpSlug = "another-endpoint" },
		"missing browser proof": func(s *mcp.AuthnChallengeState) {
			s.Browser = &mcp.ChallengeBrowserBinding{CookieID: "another-browser", OriginHash: "unmatched"}
		},
	} {
		state := original
		mutate(&state)
		require.NoError(t, ti.authnChallengeCache.Store(ctx, state), name)
		for _, action := range []string{"agent_connections", "agent_attach", "agent_detach"} {
			_, err := call(action, false, url.Values{"remote_session_id": {owned.String()}, "binding_id": {uuid.NewString()}})
			require.Error(t, err, "%s: %s", name, action)
		}
		require.NoError(t, ti.authnChallengeCache.Store(ctx, original), name)
	}
	for _, action := range []string{"agent_connections", "agent_attach", "agent_detach"} {
		_, err := call(action, false, url.Values{"state": {uuid.NewString()}})
		require.Error(t, err, action)
		_, err = call(action, false, url.Values{"csrf_token": {"wrong"}})
		require.Error(t, err, action)
	}
	_, err = call("agent_attach", false, url.Values{"csrf_token": {"wrong"}, "remote_session_id": {owned.String()}})
	require.Error(t, err)
	_, err = call("agent_attach", false, url.Values{"remote_session_id": {other.String()}})
	require.Error(t, err)
	w, err := call("agent_connections", false, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), owned.String())
	require.NotContains(t, w.Body.String(), other.String())
	require.NotContains(t, w.Body.String(), "upstream-placeholder")
	require.Contains(t, w.Body.String(), "existing-account@example.com")
	require.Contains(t, w.Body.String(), "Existing upstream account")
	require.Empty(t, w.Header().Get("Location"), "discovery must not start OAuth")
	w, err = call("agent_attach", false, url.Values{"remote_session_id": {owned.String()}})
	require.NoError(t, err)
	var binding struct {
		ID              string `json:"ID"`
		RemoteSessionID string `json:"RemoteSessionID"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &binding))
	require.Equal(t, owned.String(), binding.RemoteSessionID)
	require.Empty(t, w.Header().Get("Location"), "attaching an existing account must not redirect to OAuth")
	_, err = ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{ID: grantID, OrganizationID: fx.orgID})
	require.NoError(t, err)
	_, err = call("agent_detach", false, url.Values{"binding_id": {binding.ID}})
	require.NoError(t, err)
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)
	// Restoring the explicit attachment completes consent without changing
	// the agent's fixed connect policy or borrowing the human subject.
	_, err = call("agent_attach", false, url.Values{"remote_session_id": {owned.String()}})
	require.NoError(t, err)
	session, err := ti.sessionManager.GetSession(ctx, token)
	require.NoError(t, err)
	mismatched := session
	mismatched.UserID = alternateSession.UserID
	require.NoError(t, ti.sessionManager.StoreSession(ctx, mismatched))
	_, err = call("agent_connections", false, nil)
	require.Error(t, err, "stored session must still match the consent authorizer")
	require.NoError(t, ti.sessionManager.StoreSession(ctx, session))
	fallbackToken, err := sessions.NewSessionID()
	require.NoError(t, err)
	fallbackSession := session
	fallbackSession.SessionID = fallbackToken
	require.NoError(t, ti.sessionManager.StoreSession(ctx, fallbackSession))
	require.NoError(t, ti.sessionManager.ClearSession(ctx, session))
	_, err = call("agent_connections", false, nil)
	require.Error(t, err, "revoked dashboard session must not authorize consent")
	_, err = callWithCookie("agent_connections", fallbackToken, nil)
	require.Error(t, err, "a revoked confirmed session must not fall back to a valid local cookie")

	require.NoError(t, ti.sessionManager.StoreSession(ctx, session))
	w, err = serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)
	require.Contains(t, w.Header().Get("Location"), "code=agent-v1.")
}
