package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func routedTo(ctx context.Context, slug string) context.Context {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}

func consentForm(ctx context.Context, slug string, form url.Values) *http.Request {
	req := httptest.NewRequestWithContext(routedTo(ctx, slug), http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

type attemptGateway struct {
	ctx    context.Context //nolint:containedctx // immutable authenticated request fixture shared by this test
	ti     *testInstance
	rec    *mcpconnectionattemptstest.Recording
	slug   string
	state  string
	csrf   string
	client usersessions_repo.UserSessionClient
}

// newAttemptGateway is a gateway with one working member and a live consent state.
func newAttemptGateway(t *testing.T) attemptGateway {
	t.Helper()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "gateway-codes-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer.ID)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Docs", "docs-"+uuid.NewString()[:8], 1, newRecordingUpstream(t, "search_docs").url)
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	state.FlowID = uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	return attemptGateway{ctx: ctx, ti: ti, rec: rec, slug: slug, state: stateID, csrf: csrf, client: client}
}

// A form or link only our own page builds is not the app's fault: nothing tells the person to update it.
func TestConnectionOutcome_BadConsentRequestsAreOurs(t *testing.T) {
	t.Parallel()

	requireOurs := func(t *testing.T, rec *mcpconnectionattemptstest.Recording, detail string) {
		t.Helper()
		row := rec.RequireStep(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeInternalError)
		require.Equal(t, detail, row.SubReason)
		rec.RequireNotEnded()
	}

	for name, tc := range map[string]struct {
		form   url.Values
		detail string
		wants  string
	}{
		"agent action mismatch": {url.Values{"action": {"approve"}, "agent_id": {uuid.NewString()}}, "agent_action_mismatch", "agent approval action and selection do not match"},
		"tool selection":        {url.Values{"action": {"approve"}, "tool_filtering": {"bogus"}}, "tool_selection_invalid", "invalid tool selection"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			rec := recordConnections(t, ti)
			toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, "v")
			tc.form.Set("state", stateID)
			tc.form.Set("csrf_token", "csrf-token")
			require.ErrorContains(t, ti.service.HandleConsent(httptest.NewRecorder(), consentForm(ctx, toolset.McpSlug.String, tc.form)), tc.wants)
			requireOurs(t, rec, tc.detail)
		})
	}

	for name, tc := range map[string]struct {
		form   func(tool string) url.Values
		detail string
		wants  string
	}{
		"catalog mode": {func(string) url.Values {
			return url.Values{"tool_filtering": {"off"}, "tool_catalog_mode": {"bogus"}}
		}, "catalog_mode_invalid", "invalid tool catalog mode"},
		"selection on a live catalog": {func(tool string) url.Values {
			return url.Values{"tool_filtering": {"on"}, "tools": {tool}, "tool_catalog_mode": {"live"}}
		}, "selection_needs_cached_catalog", "Specific tools requires a cached catalog"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gw := newAttemptGateway(t)
			inventory := gatewayInventoryFromPage(t, consentGetPageWithContext(t, gw.ctx, gw.ti, gw.slug, gw.state))
			form := tc.form(inventory.Tools[0].Name)
			form.Set("state", gw.state)
			form.Set("csrf_token", gw.csrf)
			form.Set("action", "approve")
			form.Set("tool_inventory_id", inventory.Attempt)
			require.ErrorContains(t, gw.ti.service.HandleConsent(httptest.NewRecorder(), consentForm(gw.ctx, gw.slug, form)), tc.wants)
			requireOurs(t, gw.rec, tc.detail)
		})
	}

	t.Run("server name too long", func(t *testing.T) {
		t.Parallel()
		gw := newAttemptGateway(t)
		req := httptest.NewRequestWithContext(routedTo(gw.ctx, gw.slug), http.MethodGet, "/mcp/"+gw.slug+"/connect?state="+gw.state+"&server="+strings.Repeat("x", 300), nil)
		require.ErrorContains(t, gw.ti.service.HandleConsent(httptest.NewRecorder(), req), "server is too long")
		requireOurs(t, gw.rec, "focus_server_too_long")
	})
}

// Tools too big for the catalog name that, not a member that failed to list them.
func TestConnectionOutcome_CatalogTooLarge(t *testing.T) {
	t.Parallel()

	gw := newAttemptGateway(t)
	inventory := gatewayInventoryFromPage(t, consentGetPageWithContext(t, gw.ctx, gw.ti, gw.slug, gw.state))
	endpoint, err := gw.ti.service.LoadResolvedMcpEndpointBySlug(gw.ctx, gw.ti.logger, gw.slug, "mcp")
	require.NoError(t, err)
	require.NoError(t, gw.ti.service.InflateConsentInventory(gw.ctx, endpoint, gw.state, inventory.Attempt))

	form := url.Values{"state": {gw.state}, "csrf_token": {gw.csrf}, "action": {"approve"}, "tool_inventory_id": {inventory.Attempt}, "tool_filtering": {"off"}, "tool_catalog_mode": {"cached"}}
	require.ErrorContains(t, gw.ti.service.HandleConsent(httptest.NewRecorder(), consentForm(gw.ctx, gw.slug, form)), "too large to approve")

	row := gw.rec.RequireStep(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeCatalogTooLarge)
	require.Empty(t, row.SubReason)
	gw.rec.RequireNotEnded()
}

// /token reads the moved codes on a code it then burns.
func TestConnectionOutcome_TokenCodes(t *testing.T) {
	t.Parallel()

	t.Run("code from another client", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
		rec := recordConnections(t, ti)
		toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		verifier := "verifier-" + uuid.NewString()
		stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, verifier)
		loc := postConsentAction(t, ctx, ti, toolset.McpSlug.String, stateID, "approve")
		other, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
			UserSessionIssuerID: issuer.ID, ClientID: "other-client-" + uuid.NewString(), ClientName: "other client", RedirectUris: client.RedirectUris, TokenEndpointAuthMethod: "none",
		})
		require.NoError(t, err)

		w := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(other, loc.Query().Get("code"), verifier))
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "code was issued to a different client")

		row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeClientAuthFailed)
		require.Equal(t, "code_client_mismatch", row.SubReason)
	})

	t.Run("approved tools gone", func(t *testing.T) {
		t.Parallel()
		gw := newAttemptGateway(t)
		inventory := gatewayInventoryFromPage(t, consentGetPageWithContext(t, gw.ctx, gw.ti, gw.slug, gw.state))
		form := url.Values{"state": {gw.state}, "csrf_token": {gw.csrf}, "action": {"approve"}, "tool_inventory_id": {inventory.Attempt}, "tool_filtering": {"off"}, "tool_catalog_mode": {"cached"}}
		approved := httptest.NewRecorder()
		require.NoError(t, gw.ti.service.HandleConsent(approved, consentForm(gw.ctx, gw.slug, form)))
		loc, err := url.Parse(approved.Header().Get("Location"))
		require.NoError(t, err)
		authCtx, ok := contextvalues.GetAuthContext(gw.ctx)
		require.True(t, ok)
		grants := cache.NewTypedObjectCache[mcp.UserSessionGrant](gw.ti.logger, gw.ti.cacheAdapter, cache.SuffixNone)
		endpoint, err := gw.ti.service.LoadResolvedMcpEndpointBySlug(gw.ctx, gw.ti.logger, gw.slug, "mcp")
		require.NoError(t, err)
		grant, err := grants.Get(gw.ctx, "gatewayCatalogGrant:"+endpoint.UserSessionIssuerID.String()+":"+loc.Query().Get("code"))
		require.NoError(t, err)
		require.NoError(t, gw.ti.cacheAdapter.Delete(gw.ctx, mcp.ConnectionCatalogKey(authCtx.ActiveOrganizationID, grant.GatewayPolicy.Catalog.ID)))

		w := postForm(t, gw.ti, gw.slug, "token", codeGrantForm(gw.client, loc.Query().Get("code"), gatewayConsentVerifier))
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "approved tools expired")

		row := gw.rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeConnectionCatalogExpired)
		require.Equal(t, "approved_tools_expired", row.SubReason)
	})
}

// An agent the policy refuses is an access problem. A database outage is ours.
func TestConnectionOutcome_AgentAdmissionAtToken(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		breakIt func(t *testing.T, ctx context.Context, ti *testInstance, fx agentConsentFixture, agent urn.Principal)
		want    mcpconnectionattempts.Code
	}{
		"grant revoked": {func(t *testing.T, ctx context.Context, ti *testInstance, fx agentConsentFixture, agent urn.Principal) {
			t.Helper()
			_, err := ti.conn.Exec(ctx, "DELETE FROM principal_grants WHERE organization_id = $1 AND principal_urn = $2", fx.orgID, agent.String()) //nolint:glint // notestingrawsql: deliberate fixture mutation or database fault to exercise outcome classification
			require.NoError(t, err)
		}, mcpconnectionattempts.CodeNoServerAccess},
		"database outage": {func(t *testing.T, ctx context.Context, ti *testInstance, _ agentConsentFixture, _ urn.Principal) {
			t.Helper()
			_, err := ti.conn.Exec(ctx, "ALTER TABLE principal_grants RENAME TO principal_grants_gone") //nolint:glint // notestingrawsql: deliberate fixture mutation or database fault to exercise outcome classification
			require.NoError(t, err)
		}, mcpconnectionattempts.CodeInternalError},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			rec := recordConnections(t, ti)
			fx := newAgentConsentFixture(t, ctx, ti)
			seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
			agent := createConsentAgent(t, ctx, ti, fx, "Approved agent")
			principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
			seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, principal, fx.target.MCPResourceID)
			verifier := "verifier-" + uuid.NewString()
			sum := sha256.Sum256([]byte(verifier))
			state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
			require.NoError(t, err)
			state.FlowID = uuid.NewString()
			state.CodeChallenge = base64.RawURLEncoding.EncodeToString(sum[:])
			require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
			approved, err := serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
			require.NoError(t, err)
			loc, err := url.Parse(approved.Header().Get("Location"))
			require.NoError(t, err)

			tc.breakIt(t, ctx, ti, fx, principal)

			form := codeGrantForm(fx.client, loc.Query().Get("code"), verifier)
			req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.toolset.McpSlug.String+"/token", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req = req.WithContext(routedTo(ctx, fx.toolset.McpSlug.String))
			w := httptest.NewRecorder()
			oops.OAuthErrHandle(ti.logger, ti.service.HandleToken).ServeHTTP(w, req)
			if name == "database outage" {
				require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
				// Main preserves the usable code across a dependency outage.
				rec.RequireStep(mcpconnectionattempts.StepInboundToken, tc.want)
				rec.RequireNotEnded()
			} else {
				require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "agent authorization is no longer valid")
				rec.RequireEnded(mcpconnectionattempts.StepInboundToken, tc.want)
			}
		})
	}
}

// claimFailureCache lets the page read a ticket, then fails the claim that spends it.
type claimFailureCache struct {
	cache.Cache
	fail         atomic.Pointer[error]
	markingCalls atomic.Int32
}

func (c *claimFailureCache) GetAndDelete(ctx context.Context, key string, value any) error {
	if err := c.fail.Load(); err != nil {
		return *err
	}
	return c.Cache.GetAndDelete(ctx, key, value) //nolint:wrapcheck // test decorator preserves the underlying cache error
}

func (c *claimFailureCache) GetAndDeleteMarking(ctx context.Context, key, marker string, ttl time.Duration, value any) error {
	c.markingCalls.Add(1)
	if err := c.fail.Load(); err != nil {
		return *err
	}
	marking, ok := c.Cache.(cache.MarkingConsumeCache)
	if !ok {
		return errors.New("test delegate does not implement marking consume")
	}
	return marking.GetAndDeleteMarking(ctx, key, marker, ttl, value) //nolint:wrapcheck // preserve the injected cache contract
}

// Only a spent link is expired. A store that can't answer says nothing about the link.
func TestConnectionOutcome_CodeModeTicketClaim(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err  error
		want mcpconnectionattempts.Code
	}{
		"already spent": {redisCache.ErrCacheMiss, mcpconnectionattempts.CodeExpired},
		"store is down": {errors.New("redis: connection refused"), mcpconnectionattempts.CodeInternalError},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var failing *claimFailureCache
			ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{buildAuthURLResult: &url.URL{Scheme: "https", Host: "idp.example.com"}}, mcp.TunnelPublicConfig{}, func(delegate cache.Cache) cache.Cache {
				failing = &claimFailureCache{Cache: delegate}
				return failing
			})
			rec := recordConnections(t, ti)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			_, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
			slug := "claim-ticket-" + uuid.NewString()
			createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
			endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
			require.NoError(t, err)
			ref, err := endpoint.EndpointRef(ctx, ti.conn, ti.serverURL.String())
			require.NoError(t, err)
			ticket := mcp.AuthnChallengeState{ID: uuid.NewString(), FirstParty: true, CreatedAt: time.Now(), Endpoint: ref, UserSessionIssuerID: issuer.ID, CodeReconnect: &mcp.CodeReconnectBinding{UserID: auth.UserID, Server: "tracker", ServerID: uuid.New(), Ticket: true, ExpiresAt: time.Now().Add(time.Hour)}}
			require.NoError(t, ti.authnChallengeCache.Store(ctx, ticket))
			failing.fail.Store(&tc.err)

			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/mcp/"+slug+"/connect/first-party?code_reconnect="+ticket.ID, nil)
			require.Error(t, ti.service.ServeFirstPartyConnect(httptest.NewRecorder(), req, endpoint))
			rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, tc.want)
		})
	}
}

func TestConnectionOutcome_CodeModeTicketExpiryRequiresExpiredTicket(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"expired", "not a ticket", "malformed live ticket"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			rec := recordConnections(t, ti)
			toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
			endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, toolset.McpSlug.String, "mcp")
			require.NoError(t, err)
			ref, err := endpoint.EndpointRef(ctx, ti.conn, ti.serverURL.String())
			require.NoError(t, err)
			binding := &mcp.CodeReconnectBinding{UserID: "test-user", Server: "tracker", ServerID: uuid.New(), Ticket: true, ExpiresAt: time.Now().Add(time.Hour)}
			switch kind {
			case "expired":
				binding.ExpiresAt = time.Now().Add(-time.Minute)
			case "not a ticket":
				binding.Ticket = false
			case "malformed live ticket":
				binding.UserID = ""
			}
			ticket := mcp.AuthnChallengeState{ID: uuid.NewString(), FirstParty: true, CreatedAt: time.Now(), Endpoint: ref, UserSessionIssuerID: issuer.ID, CodeReconnect: binding}
			require.NoError(t, ti.authnChallengeCache.Store(ctx, ticket))
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/mcp/"+endpoint.Slug+"/connect/first-party?code_reconnect="+ticket.ID, nil)
			require.Error(t, ti.service.ServeFirstPartyConnect(httptest.NewRecorder(), req, endpoint))
			if kind == "expired" {
				rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeExpired)
			} else {
				rec.RequireNothing()
			}
		})
	}
}

// Building the sign-in link is local work, so failing it can't mean the service is down.
func TestConnectionOutcome_FirstPartySignInLinkFailureIsOurs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{buildAuthURLErr: context.DeadlineExceeded})
	rec := recordConnections(t, ti)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := toolset.McpSlug.String
	req := httptest.NewRequestWithContext(routedTo(ctx, slug), http.MethodGet, "/mcp/"+slug+"/connect/first-party", nil)
	require.Error(t, ti.service.HandleFirstPartyConnect(httptest.NewRecorder(), req))
	rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeInternalError)
}

// A person who declines at the sign-in service is read as that service's refusal, on all three paths.
func TestConnectionOutcome_DeclineAtSignInService(t *testing.T) {
	t.Parallel()

	t.Run("first party", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{hasAccessOK: true})
		rec := recordConnections(t, ti)
		challengeID := seedIDPCallbackChallenge(t, ctx, ti)
		state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+challengeID)
		require.NoError(t, err)
		state.FlowID = uuid.NewString()
		state.FirstParty = true
		require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/mcp/idp_callback?error=access_denied&state="+challengeID, nil)
		require.Error(t, ti.service.HandleIDPCallback(httptest.NewRecorder(), req))
		row := rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeProviderDenied)
		require.Equal(t, string(mcpconnectionattempts.OutcomeDeclined), row.Outcome)
	})
	t.Run("federated", func(t *testing.T) {
		t.Parallel()
		ctx, p := beginPerClientFederation(t, false)
		f := p.f
		rec := recordConnections(t, f.ti)
		query := url.Values{"state": {p.id}, "error": {"access_denied"}}
		require.NoError(t, f.ti.service.HandleIDPCallback(httptest.NewRecorder(), p.callback(ctx, p.callbackPath, f.clientID.String(), query)))
		rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeProviderDenied)
	})
}

// A broken sign-in setup at the start of a sign-in is a setup problem, not a sign-in that drifted.
func TestConnectionOutcome_BrokenFederationAtAuthorize(t *testing.T) {
	t.Parallel()

	ctx, f := newFederationLoginFixture(t, true)
	rec := recordConnections(t, f.ti)
	_, err := f.ti.conn.Exec(ctx, "UPDATE remote_session_clients SET deleted_at = now() WHERE id = $1", f.clientID) //nolint:glint // notestingrawsql: deliberate fixture mutation or database fault to exercise outcome classification
	require.NoError(t, err)

	query := url.Values{"response_type": {"code"}, "client_id": {f.downstreamClientID}, "redirect_uri": {"http://127.0.0.1/callback"}, "state": {"s"}, "code_challenge": {"c"}, "code_challenge_method": {"S256"}}
	req := httptest.NewRequestWithContext(routedTo(ctx, f.toolsetSlug), http.MethodGet, "/mcp/"+f.toolsetSlug+"/authorize?"+query.Encode(), nil)
	_ = f.ti.service.HandleAuthorize(httptest.NewRecorder(), req)
	rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeConfigurationRequired)
}

// A member that answers with no verdict on a direct path reads as the service's trouble.
func TestConnectionOutcome_PostSignInCheckGotNoAnswer(t *testing.T) {
	t.Parallel()

	ctx, fx := seedStandaloneValidationFixture(t, "attempt-probe-"+uuid.NewString()[:6])
	rec := recordConnections(t, fx.ti)
	state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	state.FlowID = uuid.NewString()
	state.CreatedAt = time.Now()
	require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))
	fx.member.set(memberErrors)

	fx.ti.service.VerifyRemoteGrantOn(ctx, fx.endpoint, remotesessions.RemoteGrant{ParentChallengeID: fx.stateID, UserSessionIssuerID: fx.endpoint.UserSessionIssuerID, RemoteSessionClientID: fx.clientID, Subject: fx.subject})

	rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderUnavailable)
	rec.RequireNotEnded()
}

// A first-time member reads not_connected on the project gateway. It has no choice to be stale.
func TestConnectionOutcome_FirstTimeMemberIsNotConnected(t *testing.T) {
	t.Parallel()

	t.Run("project gateway", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
		rec := recordConnections(t, ti)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		slug := "first-time-" + uuid.NewString()
		gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer.ID)
		deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(deny.Close)
		memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Tracker", "tracker-"+uuid.NewString()[:8], 1, deny.URL)
		remote := createConsentRemoteClient(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "first-"+uuid.NewString()[:8], "", []uuid.UUID{issuer.ID})
		_, err := upstreamclientsrepo.New(ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, McpServerID: memberID, RemoteSessionClientID: remote})
		require.NoError(t, err)
		stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
		state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
		require.NoError(t, err)
		state.FlowID = uuid.NewString()
		require.NoError(t, ti.authnChallengeCache.Store(ctx, state))

		consentGetPageWithContext(t, ctx, ti, slug, stateID)
		require.NoError(t, ti.service.DrainConnectionEndings(ctx))

		row := rec.RequireStep(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeNeedsSignIn)
		require.Equal(t, "not_connected", row.SubReason)
		rec.RequireStep(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeNeedsSignIn)
	})
}

// A project gateway with nothing on it has no server to use. Nothing failed to list.
func TestConnectionOutcome_EmptyGatewayHasNoServerToUse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "empty-gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer.ID)
	stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	state.FlowID = uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))

	consentGetPageWithContext(t, ctx, ti, slug, stateID)

	rec.RequireStep(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeResourceNotAvailable)
}

// An unknown client at /token writes no row but must show up in the refusal count.
func TestConnectionOutcome_TokenUnknownClientIsCounted(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	rec := recordConnections(t, ti)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {"client_missing"}, "code": {"c"}, "redirect_uri": {"http://localhost/cb"}, "code_verifier": {"v"}}
	require.Equal(t, http.StatusBadRequest, postForm(t, ti, toolset.McpSlug.String, "token", form).Code)

	rec.RequireNothing()
	points := collectCounterPoints(t, reader, "mcp.connection.refusals_without_row")
	require.Equal(t, int64(1), points[attribute.NewSet(attr.OAuthFailureReason("token_unknown_client_id"))], points)
}

// Past its time a workload link is expired, whoever opens it. Within it, the wrong person is still refused.
func TestConnectionOutcome_WorkloadLinkExpiry(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		expires time.Duration
		want    mcpconnectionattempts.Code
	}{
		"past its time":  {-time.Minute, mcpconnectionattempts.CodeExpired},
		"another person": {time.Hour, mcpconnectionattempts.CodeSessionChanged},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, memberMock())
			rec := recordConnections(t, ti)
			challengeID := seedIDPCallbackChallenge(t, ctx, ti)
			state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+challengeID)
			require.NoError(t, err)
			state.FlowID = uuid.NewString()
			state.WorkloadReconnect = &mcp.WorkloadReconnectBinding{OwnerUserID: "someone-else", ClientID: uuid.New(), ExpiresAt: time.Now().Add(tc.expires)}
			require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/mcp/idp_callback?code=c&state="+challengeID, nil)
			require.Error(t, ti.service.HandleIDPCallback(httptest.NewRecorder(), req))
			rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, tc.want)
		})
	}
}

// A recovery link refused on the callback: someone else's browser is session_changed, an outage is ours.
func TestConnectionOutcome_RecoveryCallbackRefusal(t *testing.T) {
	t.Parallel()

	newFixture := func(t *testing.T) (*connectionRecoveryFixture, *mcpconnectionattemptstest.Recording) {
		t.Helper()
		f := newConnectionRecoveryFixture(t)
		rec := mcpconnectionattemptstest.New(t)
		f.ti.service.SetConnectionRecorder(rec.Recorder)
		remotesessions.WithConnectionRecorder(rec.Recorder)(f.ti.service.RemoteChallengeManager())
		return f, rec
	}
	login := func(t *testing.T, f *connectionRecoveryFixture) (string, []*http.Cookie) {
		t.Helper()
		path, err := f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
		require.NoError(t, err)
		location, cookies := f.start(t, path, f.userID)
		return location.Query().Get("state"), cookies
	}
	callback := func(f *connectionRecoveryFixture, state string, cookies []*http.Cookie) *http.Request {
		req := f.callback("", cookies)
		req.URL.RawQuery = url.Values{"code": {"c"}, "state": {state}}.Encode()
		return req
	}

	t.Run("another browser", func(t *testing.T) {
		t.Parallel()
		f, rec := newFixture(t)
		state, _ := login(t, f)
		require.Error(t, f.ti.service.HandleRemoteLoginCallback(httptest.NewRecorder(), callback(f, state, nil)))
		rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeSessionChanged)
	})
	t.Run("membership check down", func(t *testing.T) {
		t.Parallel()
		f, rec := newFixture(t)
		state, cookies := login(t, f)
		_, err := f.ti.conn.Exec(f.ctx, "ALTER TABLE organization_user_relationships RENAME TO organization_user_relationships_gone") //nolint:glint // notestingrawsql: deliberate fixture mutation or database fault to exercise outcome classification
		require.NoError(t, err)
		require.Error(t, f.ti.service.HandleRemoteLoginCallback(httptest.NewRecorder(), callback(f, state, cookies)))
		rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeInternalError)
	})
}

func TestConnectionOutcome_TokenConsumeOutageIsNotLostRace(t *testing.T) {
	t.Parallel()
	for _, wrongProof := range []bool{false, true} {
		t.Run(fmt.Sprint(wrongProof), func(t *testing.T) {
			t.Parallel()
			var failing *claimFailureCache
			ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{}, mcp.TunnelPublicConfig{}, func(delegate cache.Cache) cache.Cache {
				failing = &claimFailureCache{Cache: delegate}
				return failing
			})
			rec := recordConnections(t, ti)
			toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			verifier := "verifier-" + uuid.NewString()
			stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, verifier)
			loc := postConsentAction(t, ctx, ti, toolset.McpSlug.String, stateID, "approve")
			outage := errors.New("cache connection unavailable")
			failing.fail.Store(&outage)
			if wrongProof {
				verifier = "wrong-proof"
			}
			w := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, loc.Query().Get("code"), verifier))
			require.Equal(t, http.StatusBadRequest, w.Code)
			rec.RequireStep(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeInternalError)
			rec.RequireNotEnded()
		})
	}
}

func TestConnectionOutcome_ConsentConsumeOutageIsNotLostRace(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"approve", "deny"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			var failing *claimFailureCache
			ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{}, mcp.TunnelPublicConfig{}, func(delegate cache.Cache) cache.Cache {
				failing = &claimFailureCache{Cache: delegate}
				return failing
			})
			rec := recordConnections(t, ti)
			toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, "verifier-"+uuid.NewString())
			outage := errors.New("cache connection unavailable")
			failing.fail.Store(&outage)
			form := url.Values{"state": {stateID}, "csrf_token": {"csrf-token"}, "action": {action}}
			req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/connect", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			err := ti.service.HandleConsent(httptest.NewRecorder(), req.WithContext(routedTo(ctx, toolset.McpSlug.String)))
			var failure *oops.ShareableError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, oops.CodeUnavailable, failure.Code)
			require.EqualValues(t, 1, failing.markingCalls.Load())
			rec.RequireStep(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeInternalError)
			rec.RequireNotEnded()
			failing.fail.Store(nil)
			postConsentAction(t, ctx, ti, toolset.McpSlug.String, stateID, action)
		})
	}
}

type challengeStoreFailureCache struct{ cache.Cache }

func (c *challengeStoreFailureCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if strings.HasPrefix(key, "authnChallenge:") {
		return errors.New("challenge store unavailable")
	}
	return c.Cache.Set(ctx, key, value, ttl) //nolint:wrapcheck // test decorator preserves the cache contract
}

func TestConnectionOutcome_FirstPartyStoreFailure(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{}, mcp.TunnelPublicConfig{}, func(delegate cache.Cache) cache.Cache {
		return &challengeStoreFailureCache{Cache: delegate}
	})
	rec := recordConnections(t, ti)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	req := httptest.NewRequestWithContext(routedTo(ctx, toolset.McpSlug.String), http.MethodGet, "/mcp/"+toolset.McpSlug.String+"/connect/first-party", nil)
	require.Error(t, ti.service.HandleFirstPartyConnect(httptest.NewRecorder(), req))
	rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeInternalError)
}

type registrationDisconnectWriter struct {
	http.ResponseWriter
	cancel context.CancelFunc
}

func (w registrationDisconnectWriter) Write([]byte) (int, error) {
	w.cancel()
	return 0, context.Canceled
}

func TestConnectionOutcome_RegistrationDisconnectIsNotOutage(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	body := `{"client_name":"test client","redirect_uris":["http://localhost:9876/callback"],"token_endpoint_auth_method":"none"}`
	req := httptest.NewRequestWithContext(routedTo(ctx, toolset.McpSlug.String), http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	require.Error(t, ti.service.HandleRegister(registrationDisconnectWriter{httptest.NewRecorder(), cancel}, req))
	rec.RequireNothing()
}

type registrationPanicWriter struct{ http.ResponseWriter }

func (w registrationPanicWriter) Write([]byte) (int, error) { panic("response writer panicked") }

func TestConnectionOutcome_RegistrationPanicIsInternal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	body := `{"client_name":"test client","redirect_uris":["http://localhost:9876/callback"],"token_endpoint_auth_method":"none"}`
	req := httptest.NewRequestWithContext(routedTo(ctx, toolset.McpSlug.String), http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/register", strings.NewReader(body))
	require.Panics(t, func() { _ = ti.service.HandleRegister(registrationPanicWriter{httptest.NewRecorder()}, req) })
	rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeInternalError)
}

type registrationFailingWriter struct{ http.ResponseWriter }

func (w registrationFailingWriter) Write([]byte) (int, error) {
	return 0, errors.New("response unavailable")
}

func TestConnectionOutcome_RegistrationOperationalFailure(t *testing.T) {
	t.Parallel()
	for _, parsed := range []bool{false, true} {
		t.Run(fmt.Sprint(parsed), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			rec := recordConnections(t, ti)
			toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
			body := "not json"
			if parsed {
				body = `{"client_name":"test client","redirect_uris":["http://localhost:9876/callback"],"token_endpoint_auth_method":"none"}`
			}
			req := httptest.NewRequestWithContext(routedTo(ctx, toolset.McpSlug.String), http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/register", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			err := ti.service.HandleRegister(registrationFailingWriter{httptest.NewRecorder()}, req)
			require.Error(t, err)
			if parsed {
				rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeInternalError)
			} else {
				rec.RequireNothing()
			}
		})
	}
}
