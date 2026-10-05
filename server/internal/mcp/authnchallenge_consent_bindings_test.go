package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/registration"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/tunnel/route"
)

func TestConsentAgentBindingActionsUseRealAttachmentService(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	seedPrincipalGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, fx.userID), authz.ScopeProjectRead, fx.target.ProjectID.String())
	agent := createConsentAgent(t, ctx, ti, fx, "Reusable connection agent")
	grantID := seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)
	policy, policyErr := guardian.NewUnsafePolicy(ti.tracerProvider, []string{})
	require.NoError(t, policyErr)
	meterProvider := testenv.NewMeterProvider(t)
	tunnels := tunnelrouting.NewHTTPClient(route.NewRouteTable(), "forward-token", policy, nil)
	refresher := remotesessions.NewRefreshService(ti.logger, meterProvider, ti.conn, ti.enc, policy, tunnels, ti.cacheAdapter)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	env := environments.NewEnvironmentEntries(ti.logger, ti.conn, ti.enc, mcpmetadata_repo.New(ti.conn))
	features := productfeatures.NewClient(ti.logger, ti.tracerProvider, ti.conn, redisClient)
	bindings := remotesessions.NewService(ti.logger, ti.tracerProvider, meterProvider, ti.conn, ti.sessionManager, ti.authzEngine, ti.enc, env, policy, tunnels, ti.audit, ti.serverURL, remotesessions.NewIdentityCommitter(ti.logger, ti.conn, ti.enc, ti.audit, ti.serverURL, policy, tunnels, registration.NewMetrics(testenv.NewLogger(t), testenv.NewMeterProvider(t))), refresher, features)
	bindings.SetBindingAuthorizer(func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
		require.False(t, contextvalues.HasValidatedGramSession(ctx))
		auth, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.Nil(t, auth.SessionID)
		require.Equal(t, fx.userID, auth.UserID)
		require.NotEmpty(t, auth.OrganizationSlug)
		scope, ok := contextvalues.GetConsentBindingAuthorization(ctx)
		require.True(t, ok)
		require.Equal(t, id, scope.AgentID)
		require.Equal(t, fx.target.UserSessionIssuerID, scope.IssuerID)
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
	token := "unrelated-or-expired-dashboard-cookie"
	call := func(action string, cookie bool, extra url.Values) (*httptest.ResponseRecorder, error) {
		form := url.Values{"state": {fx.stateID}, "csrf_token": {fx.csrf}, "agent_id": {agent.ID.String()}, "action": {action}}
		maps.Copy(form, extra)
		// A real consent request has no RPC-prepared authorization grants.
		req := httptest.NewRequest(http.MethodPost, "https://mcp.example.com/mcp/"+endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode())).WithContext(t.Context())
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie {
			req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: token})
		}
		w := httptest.NewRecorder()
		err := ti.service.ServeConsentAction(w, req, endpoint)
		if err != nil {
			return w, fmt.Errorf("serve consent action: %w", err)
		}
		return w, nil
	}
	_, err = call("agent_connections", true, nil)
	require.NoError(t, err, "dashboard cookie must not override resolved consent human")
	// All actions consume the same cached OAuth human and exact endpoint target.
	original, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*mcp.AuthnChallengeState){
		"missing human":    func(state *mcp.AuthnChallengeState) { state.AuthorizerUserID = "" },
		"mismatched human": func(state *mcp.AuthnChallengeState) { state.AuthorizerUserID = "another-human" },
		"nonmember": func(state *mcp.AuthnChallengeState) {
			subject := urn.NewUserSubject("nonmember")
			state.Subject = &subject
			state.AuthorizerUserID = subject.ID
		},
		"missing provenance": func(state *mcp.AuthnChallengeState) { state.AuthorizerImpersonated = nil },
		"impersonated":       func(state *mcp.AuthnChallengeState) { value := true; state.AuthorizerImpersonated = &value },
		"wrong tenant": func(state *mcp.AuthnChallengeState) {
			target := *state.AgentAuthorizationTarget
			target.OrganizationID = "another-org"
			state.AgentAuthorizationTarget = &target
		},
		"wrong project": func(state *mcp.AuthnChallengeState) {
			target := *state.AgentAuthorizationTarget
			target.ProjectID = uuid.New()
			state.AgentAuthorizationTarget = &target
		},
		"wrong issuer": func(state *mcp.AuthnChallengeState) {
			target := *state.AgentAuthorizationTarget
			target.UserSessionIssuerID = uuid.New()
			state.AgentAuthorizationTarget = &target
		},
		"missing browser proof": func(state *mcp.AuthnChallengeState) {
			state.Browser = &mcp.ChallengeBrowserBinding{CookieID: "browser", OriginHash: "required-proof"}
		},
	} {
		state := original
		mutate(&state)
		require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
		for _, action := range []string{"agent_connections", "agent_attach", "agent_detach"} {
			_, err := call(action, false, url.Values{"remote_session_id": {owned.String()}, "binding_id": {uuid.NewString()}})
			var denied *oops.ShareableError
			require.ErrorAs(t, err, &denied, "%s: %s", name, action)
			want := oops.CodeForbidden
			if name == "missing browser proof" {
				want = oops.CodeUnauthorized
			}
			require.Equal(t, want, denied.Code, "%s: %s", name, action)
		}
	}
	require.NoError(t, ti.authnChallengeCache.Store(ctx, original))
	for _, id := range []string{"invalid", uuid.NewString()} {
		_, err := call("agent_connections", false, url.Values{"agent_id": {id}})
		require.Error(t, err)
	}
	// A live delegated authorizer can manage another owner's agent. The same
	// request must fail as soon as the exact agent:authorize grant is removed.
	otherOwner := seedConsentMember(t, ctx, ti, fx.orgID)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, otherOwner, fx.target.MCPResourceID.String())
	_, err = agentsrepo.New(ti.conn).TransferAgent(ctx, agentsrepo.TransferAgentParams{OwnerUserID: otherOwner, OrganizationID: fx.orgID, ID: agent.ID})
	require.NoError(t, err)
	_, err = call("agent_connections", false, nil)
	require.Error(t, err)
	delegated := seedPrincipalGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, fx.userID), authz.ScopeAgentAuthorize, agent.ID.String())
	_, err = call("agent_connections", false, nil)
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{ID: delegated, OrganizationID: fx.orgID})
	require.NoError(t, err)
	_, err = call("agent_connections", false, nil)
	require.Error(t, err)
	_, err = agentsrepo.New(ti.conn).TransferAgent(ctx, agentsrepo.TransferAgentParams{OwnerUserID: fx.userID, OrganizationID: fx.orgID, ID: agent.ID})
	require.NoError(t, err)
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
	w, err = serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)
	require.Contains(t, w.Header().Get("Location"), "code=agent-v1.")
}
