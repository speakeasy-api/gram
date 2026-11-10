package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestDirectWorkloadReconnect(t *testing.T) {
	t.Parallel()
	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)
	ti.service.manual = true
	fx := newWorkloadConsentFixture(t, ctx, ti)
	selector, err := authz.NewSelector(authz.ScopeProjectRead, fx.target.ProjectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: fx.orgID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, fx.userID), Scope: string(authz.ScopeProjectRead), Selectors: selector})
	require.NoError(t, err)
	upstream := newRecordingUpstream(t, "ping")
	slug := "workload-reconnect-" + uuid.NewString()[:8]
	server, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, fx.target.ProjectID, upstream.url, slug, "private", fx.target.UserSessionIssuerID)
	fx.target.MCPResourceID = server.ID
	agent := createConsentAgent(t, ctx, ti, fx, "Reconnect workload")
	grantID := seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), server.ID)
	issuer := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignment := assignAgentToWorkload(t, ctx, ti, fx.orgID, issuer, workloadSessionSubject, agent.ID)
	subject := urn.NewWorkloadSubject(issuer, workloadSessionSubject)
	session := seedWorkloadSession(t, ctx, ti, fx, subject)
	client := createConsentRemoteClient(t, ctx, ti.conn, fx.target.ProjectID, fx.orgID, "workload-reconnect", "", []uuid.UUID{fx.target.UserSessionIssuerID})
	selection := selectionFixture{ti: ti, projectID: fx.target.ProjectID, orgID: fx.orgID}
	selection.associate(t, ctx, server.ID, client)
	policy, err := usersessionsrepo.New(ti.conn).GetUserSessionPolicyByJTI(ctx, usersessionsrepo.GetUserSessionPolicyByJTIParams{UserSessionIssuerID: fx.target.UserSessionIssuerID, Jti: session.Jti})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).RecordUpstreamClientSelections(ctx, usersessionsrepo.RecordUpstreamClientSelectionsParams{UserSessionIssuerID: fx.target.UserSessionIssuerID, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: policy.CredentialID, Source: upstreamclients.SelectionSourceIssuance, PickedMcpServerIds: []uuid.UUID{server.ID}, PickedClientIds: []uuid.UUID{client}})
	require.NoError(t, err)
	token, _, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{Subject: subject, Audience: urn.NewUserSessionIssuer(fx.target.UserSessionIssuerID).String(), Issuer: ti.serverURL.JoinPath("mcp", slug).String(), ExpiresAt: &session.ExpiresAt.Time, ClientID: fx.client.ClientID, JTI: session.Jti})
	require.NoError(t, err)

	var link string
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":"call","method":"tools/call","params":{"name":"ping","arguments":{}}}`,
	} {
		w, err := servePublicHTTP(t, context.Background(), ti, slug, []byte(body), token, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Empty(t, w.Header().Get("WWW-Authenticate"))
		var result struct {
			ID    string `json:"id"`
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Data    struct {
					Elicitations []struct {
						Mode string `json:"mode"`
						URL  string `json:"url"`
						ID   string `json:"elicitationId"`
					} `json:"elicitations"`
				} `json:"data"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		var request struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &request))
		require.Equal(t, request.ID, result.ID)
		require.Equal(t, -32042, result.Error.Code)
		require.Len(t, result.Error.Data.Elicitations, 1)
		link = result.Error.Data.Elicitations[0].URL
		require.Equal(t, "url", result.Error.Data.Elicitations[0].Mode)
		require.Contains(t, result.Error.Message, link)
		require.Contains(t, result.Error.Message, "explicitly retry")
		require.Contains(t, link, "/mcp/"+slug+"/connect/first-party?workload_reconnect=")
		require.NotContains(t, link, "oauth-protected-resource")
		require.NotContains(t, link, token)
	}

	_, err = accessrepo.New(ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{ID: grantID, OrganizationID: fx.orgID})
	require.NoError(t, err)
	toolSelector := authz.NewSelector(authz.ScopeMCPConnect, server.ID.String())
	toolSelector[authz.SelectorKeyTool] = "ping"
	toolSelectorJSON, err := toolSelector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: fx.orgID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), Scope: string(authz.ScopeMCPConnect), Selectors: toolSelectorJSON})
	require.NoError(t, err)
	denied, err := callTool(t, ti, slug, token, "not-allowed")
	require.Error(t, err)
	require.NotContains(t, denied.Body.String(), "workload_reconnect")
	parsed, err := url.Parse(link)
	require.NoError(t, err)
	ticketID := parsed.Query().Get("workload_reconnect")
	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+ticketID)
	require.NoError(t, err)
	require.Equal(t, fx.userID, stored.WorkloadReconnect.OwnerUserID)
	require.Equal(t, server.ID, stored.Endpoint.McpServerID.UUID)
	require.True(t, stored.WorkloadReconnect.Ticket)

	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", slug)
	browser := httptest.NewRequest(http.MethodGet, link, nil).WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
	response := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleFirstPartyConnect(response, browser))
	require.Equal(t, http.StatusFound, response.Code)
	redirect, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	challenge, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+redirect.Query().Get("state"))
	require.NoError(t, err)
	require.True(t, challenge.FirstParty)
	require.False(t, challenge.WorkloadReconnect.Ticket)
	require.Equal(t, fx.userID, challenge.WorkloadReconnect.OwnerUserID)
	require.Equal(t, stored.WorkloadReconnect.ExpiresAt, challenge.WorkloadReconnect.ExpiresAt)
	require.Error(t, ti.service.HandleFirstPartyConnect(httptest.NewRecorder(), browser), "entry ticket is single-use")

	// The existing owner credential path, not a new workload grant, repairs the
	// next explicit invocation. The original failing calls never ran upstream.
	upstream.requireNeverCarried(t, "Bearer owner-token", "no upstream request before connection")
	insertQualifiedRemoteSessionToken(t, ctx, ti, fx.target.UserSessionIssuerID, client, urn.NewUserSubject(fx.userID), "owner-token", upstream.url)
	w, err := callTool(t, ti, slug, token, "ping")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	upstream.requireToolCallAuth(t, "Bearer owner-token")

	insertRemoteSessionAccessToken(t, ctx, ti, fx.target.UserSessionIssuerID, client, urn.NewUserSubject(fx.userID), "expired-owner-token", time.Now().Add(-time.Minute))
	expired, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, expired.Code)
	require.Empty(t, expired.Header().Get("WWW-Authenticate"))
	require.Contains(t, expired.Body.String(), "-32042")
	require.Contains(t, expired.Body.String(), "workload_reconnect")
	w, err = servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "invalid-workload-token", nil)
	require.Error(t, err)
	require.Contains(t, w.Header().Get("WWW-Authenticate"), "resource_metadata")
	unassignWorkloadAgent(t, ctx, ti, assignment)
	w, err = servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), token, nil)
	require.Error(t, err)
	require.NotContains(t, w.Body.String(), "workload_reconnect")
}

func TestFirstPartyWorkloadReconnectRejectsWrongResourceAndExpiry(t *testing.T) {
	t.Parallel()
	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)
	fx := newWorkloadConsentFixture(t, ctx, ti)
	for _, expired := range []bool{false, true} {
		id := uuid.NewString()
		state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
		require.NoError(t, err)
		state.ID = id
		state.WorkloadReconnect = &mcp.WorkloadReconnectBinding{OwnerUserID: fx.userID, ExpiresAt: time.Now().Add(time.Minute), Ticket: true}
		if expired {
			state.WorkloadReconnect.ExpiresAt = time.Now().Add(-time.Minute)
		} else {
			state.UserSessionIssuerID = uuid.New()
		}
		require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
		route := chi.NewRouteContext()
		route.URLParams.Add("mcpSlug", fx.toolset.McpSlug.String)
		req := httptest.NewRequest(http.MethodGet, "/mcp/"+fx.toolset.McpSlug.String+"/connect/first-party?workload_reconnect="+id, nil).WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
		w := httptest.NewRecorder()
		require.Error(t, ti.service.HandleFirstPartyConnect(w, req))
		require.NotContains(t, w.Header().Get("Location"), "state=")
	}
}

func TestWorkloadReconnectIDPCallbackOwner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                string
		otherOwner, expired bool
	}{
		{name: "owner"}, {name: "another owner", otherOwner: true}, {name: "expired", expired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := memberMock()
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
			id := seedIDPCallbackChallenge(t, ctx, ti)
			state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+id)
			require.NoError(t, err)
			owner := mock.upsertResult
			if tc.otherOwner {
				owner = "different-test-owner"
			}
			expires := time.Now().Add(time.Minute)
			if tc.expired {
				expires = time.Now().Add(-time.Minute)
			}
			state.FirstParty = true
			state.ClientID = ""
			state.RedirectURI = ""
			state.WorkloadReconnect = &mcp.WorkloadReconnectBinding{OwnerUserID: owner, ClientID: uuid.New(), ExpiresAt: expires}
			require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
			req := httptest.NewRequest(http.MethodGet, "/mcp/idp_callback?state="+id+"&code=idp-auth-code", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			err = ti.service.HandleIDPCallback(w, req)
			if tc.otherOwner || tc.expired {
				require.Error(t, err)
				require.Empty(t, w.Header().Get("Location"))
				require.Empty(t, mock.memberChecks, "owner rejection must precede sign-in recording and credential handoff")
			} else {
				require.NoError(t, err)
				require.Equal(t, http.StatusFound, w.Code)
				next, err := url.Parse(w.Header().Get("Location"))
				require.NoError(t, err)
				bound, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+next.Query().Get("state"))
				require.NoError(t, err)
				require.Equal(t, owner, bound.Subject.ID)
				require.Equal(t, owner, bound.WorkloadReconnect.OwnerUserID)
			}
		})
	}
}

func TestWorkloadReconnectRejectsBeforeFederatedCredentialHandoff(t *testing.T) {
	t.Parallel()
	ctx, f := newFederationLoginFixture(t, true)
	consumed := false
	f.ti.service.SetFederatedLoginConsumer(federatedLoginConsumerFunc(func(context.Context, mcp.AuthorizedFederatedLogin) error {
		consumed = true
		return nil
	}))
	_, id, nonce, challenge, state, cookie := f.begin(t, ctx, true)
	state.WorkloadReconnect = &mcp.WorkloadReconnectBinding{OwnerUserID: "different-test-owner", ClientID: f.clientID, ExpiresAt: time.Now().Add(time.Minute)}
	require.NoError(t, f.ti.authnChallengeCache.Store(ctx, state))
	f.provider.issueCode(t, "one-use-code", federationToken{challenge: challenge, nonce: nonce, email: mockidp.MockUserEmail, issuer: f.provider.URL, verified: true, secret: "selected-secret"})
	query := url.Values{"state": {id}, "code": {"one-use-code"}, "iss": {f.provider.URL}}
	request := httptest.NewRequest(http.MethodGet, f.ti.serverURL.String()+f.callbackPath()+"?"+query.Encode(), nil).WithContext(ctx)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	require.Error(t, f.ti.service.HandleIDPCallback(response, routeIDPCallback(request)))
	require.Equal(t, 1, f.provider.exchangeCount(), "the real signed identity exchange must be exercised")
	require.False(t, consumed, "a non-owner must never reach the federated credential consumer")
	require.Empty(t, f.resolver.memberChecks, "owner rejection must precede sign-in recording")
	require.Empty(t, response.Header().Get("Location"))
}
