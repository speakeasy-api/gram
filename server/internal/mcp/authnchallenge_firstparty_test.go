package mcp_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Dashboard-started connects for remote servers must stay on the /mcp surface.
func TestHandleFirstPartyConnect_RemoteMcpEndpointStaysOnMcpSurface(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "first-party-remote-" + uuid.NewString()[:8]
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "private", issuerID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect/first-party", nil)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleFirstPartyConnect(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	callback, err := url.Parse(loc.Query().Get("redirect_uri"))
	require.NoError(t, err)
	require.Equal(t, "/mcp/idp_callback", callback.Path)

	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+loc.Query().Get("state"))
	require.NoError(t, err)
	require.True(t, stored.FirstParty)
	require.Equal(t, mcproute.MCP, stored.Endpoint.RouteBase)
	require.Equal(t, slug, stored.Endpoint.McpSlug)
	require.True(t, stored.Endpoint.McpServerID.Valid)
	require.Equal(t, mcpServer.ID, stored.Endpoint.McpServerID.UUID)
	require.Equal(t, issuerID, stored.UserSessionIssuerID)
}

// A gateway tool call links to a server's connect page through first-party
// connect, which carries the server through sign-in to the consent page.
func TestHandleFirstPartyConnect_CarriesGatewayServer(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "first-party-focus-" + uuid.NewString()[:8]
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "private", issuerID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect/first-party?server=eng.github", nil)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleFirstPartyConnect(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+loc.Query().Get("state"))
	require.NoError(t, err)
	require.True(t, stored.FirstParty)
	require.Equal(t, "eng.github", stored.FocusServer)
}

func TestHandleFirstPartyConnect_BindsAgentRefreshAttemptToOAuthState(t *testing.T) {
	t.Parallel()
	mock := memberMock()
	mock.buildAuthURLResult = &url.URL{Scheme: "https", Host: "idp.example", Path: "/authorize"}
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	mock.upsertResult = auth.UserID
	ctx = contextvalues.WithValidatedGramSession(ctx, auth, false)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	slug := "refresh-popup-" + uuid.NewString()[:8]
	createRemoteMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, "https://upstream.invalid/mcp", slug, "private", issuerID)
	policy, err := guardian.NewUnsafePolicy(ti.tracerProvider, []string{})
	require.NoError(t, err)
	meter := testenv.NewMeterProvider(t)
	refresher := remotesessions.NewRefreshService(ti.logger, meter, ti.conn, ti.enc, policy, nil, ti.cacheAdapter)
	bindings := remotesessions.NewService(ti.logger, ti.tracerProvider, meter, ti.conn, ti.sessionManager, ti.authzEngine, ti.enc, nil, policy, nil, ti.audit, ti.serverURL, remotesessions.NewIdentityCommitter(ti.logger, ti.conn, ti.enc, ti.audit, ti.serverURL, policy, nil, nil), refresher, nil, nil)
	bindings.SetBindingAuthorizer(func(ctx context.Context, tx pgx.Tx, id uuid.UUID, _ bool) error {
		_, _, err := agentmanagement.NewAuthorizer(ti.authzEngine).RequireAgentOwnerForUpdate(ctx, tx, id, agentmanagement.OwnedAgentSetup)
		if err != nil {
			return fmt.Errorf("authorize refresh test: %w", err)
		}
		return nil
	})
	attempts := remotesessions.NewBindingRefreshAttempts(ti.logger, ti.cacheAdapter)
	bindings.SetBindingRefreshAttempts(attempts)
	ti.service.SetBindingRefreshAttempts(attempts)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: auth.ActiveOrganizationID, OwnerUserID: auth.UserID, Name: "Exact popup agent"})
	require.NoError(t, err)
	client := createConsentRemoteClient(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "refresh-popup-client", "", []uuid.UUID{issuerID})
	source, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{SubjectUrn: urn.NewUserSubject(auth.UserID), UserSessionIssuerID: issuerID, RemoteSessionClientID: client, AccessTokenEncrypted: "fixture-token", AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{}})
	require.NoError(t, err)
	binding, err := bindings.AttachBinding(ctx, &gen.AttachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: issuerID.String(), RemoteSessionID: source.ID.String()})
	require.NoError(t, err)
	attempt, err := bindings.BeginBindingRefresh(ctx, &gen.BeginBindingRefreshPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: issuerID.String(), BindingID: binding.ID})
	require.NoError(t, err)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect/first-party?binding_refresh_attempt="+attempt.AttemptID+"&reauthenticate_client_id="+client.String(), nil)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	invalid := req.Clone(req.Context())
	invalidURL := *req.URL
	invalid.URL = &invalidURL
	query := invalid.URL.Query()
	query.Set("reauthenticate_client_id", "not-a-uuid")
	invalid.URL.RawQuery = query.Encode()
	require.Error(t, ti.service.HandleFirstPartyConnect(httptest.NewRecorder(), invalid))
	mock.buildAuthURLErr = errors.New("temporary identity provider failure")
	require.Error(t, ti.service.HandleFirstPartyConnect(httptest.NewRecorder(), req))
	mock.buildAuthURLErr = nil
	response := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleFirstPartyConnect(response, req), "failed setup must leave the same refresh attempt retryable")
	initialID := mock.buildAuthURLParams.State
	require.NotEmpty(t, initialID)
	challenge, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+initialID)
	require.NoError(t, err)
	require.Equal(t, attempt.AttemptID, challenge.BindingRefreshAttemptID)
	callback := httptest.NewRequest(http.MethodGet, "/mcp/idp_callback?state="+initialID+"&code=idp-code", nil).WithContext(ctx)
	returned := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleIDPCallback(returned, callback))
	location, err := url.Parse(returned.Header().Get("Location"))
	require.NoError(t, err)
	rotatedID := location.Query().Get("state")
	require.NotEqual(t, initialID, rotatedID)
	rotated, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+rotatedID)
	require.NoError(t, err)
	require.Equal(t, attempt.AttemptID, rotated.BindingRefreshAttemptID)
	require.Equal(t, client, rotated.ReauthenticateClientID)
	renewed, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{SubjectUrn: source.SubjectUrn, UserSessionIssuerID: issuerID, RemoteSessionClientID: client, AccessTokenEncrypted: "renewed-fixture", AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{}})
	require.NoError(t, err)
	grant := remotesessions.RemoteGrant{ConnectionPopup: true, ParentChallengeID: rotatedID, UserSessionIssuerID: issuerID, RemoteSessionClientID: client, Subject: source.SubjectUrn, RemoteSessionID: renewed.ID, RemoteSessionUpdatedAt: renewed.UpdatedAt.Time, GrantGeneration: renewed.GrantGeneration}
	wrongClient := grant
	wrongClient.RemoteSessionClientID = uuid.New()
	require.NoError(t, ti.service.RecordConnectionPopupGrant(ctx, wrongClient))
	stillPending, err := attempts.PendingClient(ctx, attempt.AttemptID, source.SubjectUrn)
	require.NoError(t, err)
	require.Equal(t, client, stillPending)
	require.NoError(t, ti.service.RecordConnectionPopupGrant(ctx, grant))
	completedState, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+rotatedID)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, completedState.ReauthenticateClientID)
	result, err := bindings.CompleteBindingRefresh(ctx, &gen.CompleteBindingRefreshPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: issuerID.String(), AttemptID: attempt.AttemptID})
	require.NoError(t, err)
	require.True(t, result.Completed, "rotated OAuth parent must still identify the exact attachment receipt")
	require.Error(t, ti.service.HandleFirstPartyConnect(httptest.NewRecorder(), req), "attempt cannot be replayed to start another OAuth flow")
}

func TestConnectionPopupGrant_DefaultFlowNeedsNoAttemptStore(t *testing.T) {
	t.Parallel()
	service := &mcp.Service{}
	require.NoError(t, service.RecordConnectionPopupGrant(t.Context(), remotesessions.RemoteGrant{}))
}
