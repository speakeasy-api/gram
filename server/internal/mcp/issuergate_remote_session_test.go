package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// The texts the issuer gate returns for an accepted bearer whose upstream
// remote session cannot be used. Clients and users see them verbatim.
const (
	reconnectDescription     = "The upstream connection for this MCP server is missing or expired. Reauthorize the MCP server to reconnect it."
	misconfiguredDescription = "The upstream connection for this MCP server is misconfigured. Contact the MCP server administrator."
)

func TestServePublic_IssuerGate_MissingRemoteSessionChallengesInvalidToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-reconnect")

	subject := urn.NewUserSubject("gate-reconnect-user-" + uuid.NewString())
	sessionToken := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

	w, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), sessionToken, nil)
	require.Equal(t, reconnectDescription, requireOopsCode(t, err, oops.CodeUnauthorized))

	challenge := w.Header().Get("WWW-Authenticate")
	require.Contains(t, challenge, "/.well-known/oauth-protected-resource/mcp/"+fixture.Toolset.McpSlug.String)
	require.Contains(t, challenge, `error="invalid_token"`, "a refreshable user session must be told to drop its token")
	require.Contains(t, challenge, `error_description="`+reconnectDescription+`"`)
}

func TestServePublic_IssuerGate_MissingRemoteSessionOmitsInvalidTokenForResourceScopedSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-resource-scoped")

	// An ID-JAG exchange mints its session for the exact resource and without
	// a refresh token, so invalid_token would only send it back through the
	// same exchange into the same rejection.
	subject := urn.NewUserSubject("gate-resource-user-" + uuid.NewString())
	token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: ti.serverURL.JoinPath("mcp", fixture.Toolset.McpSlug.String).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + fixture.Toolset.McpSlug.String,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, fixture.UserSessionIssuer.ID, subject, jti)

	w, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), token, nil)
	requireOopsCode(t, err, oops.CodeUnauthorized)

	challenge := w.Header().Get("WWW-Authenticate")
	require.NotContains(t, challenge, "error=")
	require.Contains(t, challenge, `error_description="`+reconnectDescription+`"`)
}

func TestServePublic_IssuerGate_UnavailableUpstreamAnswersRetryableUnavailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-unavailable")

	subject := urn.NewUserSubject("gate-unavailable-user-" + uuid.NewString())
	upsertExpiredRefreshableRemoteSession(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject)
	pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, fixture.RemoteSessionClient.ID, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	sessionToken := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

	w, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), sessionToken, nil)
	requireOopsCode(t, err, oops.CodeUnavailable)
	require.Equal(t, "30", w.Header().Get("Retry-After"))
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "a temporary upstream failure must not challenge the client to reauthorize")
}

func TestServePublic_IssuerGate_MisconfiguredUpstreamNamesAdministrator(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-misconfigured")

	subject := urn.NewUserSubject("gate-misconfigured-user-" + uuid.NewString())
	upsertExpiredRefreshableRemoteSession(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject)
	pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, fixture.RemoteSessionClient.ID, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"unauthorized_client"}`))
	})
	sessionToken := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

	w, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), sessionToken, nil)
	require.Equal(t, misconfiguredDescription, requireOopsCode(t, err, oops.CodeUnauthorized))

	challenge := w.Header().Get("WWW-Authenticate")
	require.NotContains(t, challenge, "error=", "reauthorizing cannot repair a broken client configuration")
	require.Contains(t, challenge, `error_description="`+misconfiguredDescription+`"`)
}

func TestHandleToken_RefreshRefusedWhileUpstreamNeedsReconnect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	remoteClient := attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuer.ID)

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)
	var tokenErr struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	require.NoError(t, json.Unmarshal([]byte(refused.body), &tokenErr))
	require.Equal(t, "invalid_grant", tokenErr.Error)
	require.Equal(t, reconnectDescription, tokenErr.ErrorDescription)

	// The refusal leaves the refresh token unconsumed, so it works again as
	// soon as the upstream is reconnected.
	insertRemoteSessionAccessToken(t, ctx, ti, issuer.ID, remoteClient.ID, urn.NewUserSubject("refresh-replay-user"), "fresh-upstream-token", time.Now().Add(time.Hour))
	refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
}

func TestHandleToken_RefreshProceedsWhileUpstreamUnavailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	remoteClient := attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuer.ID)
	upsertExpiredRefreshableRemoteSession(t, ctx, ti, issuer.ID, remoteClient.ID, urn.NewUserSubject("refresh-replay-user"))
	var upstreamRefreshes atomic.Int32
	pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, remoteClient.ID, func(w http.ResponseWriter, _ *http.Request) {
		upstreamRefreshes.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})

	// Many clients read a failed refresh as a sign-out, so an upstream blip
	// must not cost the user their Speakeasy session, and Speakeasy's refresh must not
	// wait on the upstream to find out.
	refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
	require.Zero(t, upstreamRefreshes.Load(), "the token endpoint must judge upstream sessions from stored state")
}

// A grant whose only upstream identity chaining serves holds no interactive
// remote session; refresh must not demand a reconnect the runtime never needs.
func TestHandleToken_RefreshFollowsTheChainingDecision(t *testing.T) {
	t.Parallel()

	const upstream = "https://chained-refresh.example.com/mcp"
	cases := []struct {
		name          string
		decision      identitychaining.RefreshDecision
		sibling       bool
		wantReconnect bool
	}{
		{name: "served by chaining", decision: identitychaining.RefreshServed},
		{name: "served while a sibling client has no session", decision: identitychaining.RefreshServed, sibling: true},
		{name: "chaining unconfirmed", decision: identitychaining.RefreshUnconfirmed},
		{name: "chaining needs a fresh sign-in", decision: identitychaining.RefreshReauthenticate, wantReconnect: true},
		{name: "chaining does not serve the upstream", decision: identitychaining.RefreshNotChained, wantReconnect: true},
		{name: "a provider just refused chaining", decision: identitychaining.RefreshRefused, wantReconnect: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx := requireProjectAuthContext(t, ctx)
			_, issuer, client, refreshToken, oldJTI := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
			remoteClient := attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuer.ID)
			if tc.sibling {
				// The server offers both clients; the person connected the
				// first, which their credential therefore chose, so the
				// sibling's missing session is not the one refresh judges.
				upsertExpiredRefreshableRemoteSession(t, ctx, ti, issuer.ID, remoteClient.ID, urn.NewUserSubject("refresh-replay-user"))
				attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuer.ID)
			}
			slug := "chained-refresh-" + uuid.NewString()[:8]
			serverID := attachConsentRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID, issuer.ID, slug, upstream)
			_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
				ProjectID:       *authCtx.ProjectID,
				CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				McpServerID:     conv.ToNullUUID(serverID),
				MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				Slug:            slug,
			})
			require.NoError(t, err)

			g := &consentGovernor{t: t, issuer: remoteClient.RemoteSessionIssuerID, served: map[string]bool{}, governs: false, configured: true, usable: false, refresh: tc.decision, acquire: nil, mu: sync.Mutex{}, requests: nil, acquired: nil, refreshed: nil}
			ti.service.SetIdentityChainer(g)

			res := performRefreshRequest(ctx, ti, slug, client.ClientID, refreshToken)
			require.NoError(t, res.err)
			if tc.wantReconnect {
				require.Equal(t, http.StatusBadRequest, res.code, res.body)
				require.Contains(t, res.body, `"error":"invalid_grant"`)
				require.Contains(t, res.body, reconnectDescription)
			} else {
				require.Equal(t, http.StatusOK, res.code, res.body)
			}

			g.mu.Lock()
			defer g.mu.Unlock()
			require.Len(t, g.refreshed, 1, "refresh asks identity chaining once")
			req := g.refreshed[0]
			require.Equal(t, upstream, req.UpstreamResource)
			require.Equal(t, "refresh-replay-user", req.UserID)
			require.Equal(t, issuer.ID, req.UserSessionIssuerID)
			require.Equal(t, oldJTI, req.UserSessionJTI, "chaining is judged for the grant being refreshed")
			require.False(t, req.RemoteSessionIssuerID.Valid, "a remote upstream selects by resource, as the runtime does")
		})
	}
}

// The runtime and the token endpoint must agree: once a refresh the gate ran
// fails for good and the gate answers invalid_token, the client's Speakeasy
// refresh is refused rather than handing it a token for the same rejection.
func TestIssuerGate_UpstreamInvalidGrantRefusesTheFollowingRefresh(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	remoteClient := attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuer.ID)
	subject := urn.NewUserSubject("refresh-replay-user")
	upsertExpiredRefreshableRemoteSession(t, ctx, ti, issuer.ID, remoteClient.ID, subject)
	pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, remoteClient.ID, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	// Before the gate has tried the upstream, the grant still looks renewable.
	refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
	var tokens tokenResponseFixture
	require.NoError(t, json.Unmarshal([]byte(refreshed.body), &tokens))

	w, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeInitializeBody(), tokens.AccessToken, nil)
	require.Equal(t, reconnectDescription, requireOopsCode(t, err, oops.CodeUnauthorized))
	require.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, tokens.RefreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)
	require.Contains(t, refused.body, `"error":"invalid_grant"`)
}

func TestHandleToken_RefreshChecksStoredUpstreamCredentials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		accessLifetime  time.Duration
		refreshLifetime time.Duration
		corruptAccess   bool
		corruptRefresh  bool
		wantReconnect   bool
	}{
		{name: "expired refresh", accessLifetime: -time.Hour, refreshLifetime: -time.Hour, wantReconnect: true},
		{name: "unreadable refresh", accessLifetime: -time.Hour, refreshLifetime: time.Hour, corruptRefresh: true, wantReconnect: true},
		{name: "unreadable access", accessLifetime: time.Hour, refreshLifetime: time.Hour, corruptAccess: true, wantReconnect: true},
		{name: "live access with expired refresh", accessLifetime: time.Hour, refreshLifetime: -time.Hour},
		{name: "live access with unreadable refresh", accessLifetime: time.Hour, refreshLifetime: time.Hour, corruptRefresh: true},
		{name: "early refresh falls back to live access", accessLifetime: 25 * time.Second, refreshLifetime: time.Hour, corruptRefresh: true},
		{name: "expired unreadable access can refresh", accessLifetime: -time.Hour, refreshLifetime: time.Hour, corruptAccess: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx := requireProjectAuthContext(t, ctx)
			toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
			remoteClient := attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuer.ID)
			subject := urn.NewUserSubject("refresh-replay-user")
			accessCiphertext, err := ti.enc.Encrypt([]byte("upstream-access"))
			require.NoError(t, err)
			refreshCiphertext, err := ti.enc.Encrypt([]byte("upstream-refresh"))
			require.NoError(t, err)
			if tc.corruptAccess {
				accessCiphertext = "invalid-ciphertext"
			}
			if tc.corruptRefresh {
				refreshCiphertext = "invalid-ciphertext"
			}
			now := time.Now()
			_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
				SubjectUrn:            subject,
				UserSessionIssuerID:   issuer.ID,
				RemoteSessionClientID: remoteClient.ID,
				AccessTokenEncrypted:  accessCiphertext,
				AccessExpiresAt:       conv.ToPGTimestamptz(now.Add(tc.accessLifetime)),
				RefreshTokenEncrypted: conv.ToPGText(refreshCiphertext),
				RefreshExpiresAt:      conv.ToPGTimestamptz(now.Add(tc.refreshLifetime)),
				Scopes:                []string{},
			})
			require.NoError(t, err)
			var upstreamRefreshes atomic.Int32
			pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, remoteClient.ID, func(w http.ResponseWriter, _ *http.Request) {
				upstreamRefreshes.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			})

			if tc.wantReconnect {
				sessionToken := mintUserSessionBearerForSubject(t, ti, toolset, subject)
				w, err := servePublicHTTP(t, t.Context(), ti, toolset.McpSlug.String, makeInitializeBody(), sessionToken, nil)
				require.Equal(t, reconnectDescription, requireOopsCode(t, err, oops.CodeUnauthorized))
				require.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
			}

			refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
			require.NoError(t, refreshed.err)
			if tc.wantReconnect {
				require.Equal(t, http.StatusBadRequest, refreshed.code, refreshed.body)
				require.Contains(t, refreshed.body, `"error":"invalid_grant"`)
				require.Contains(t, refreshed.body, reconnectDescription)
			} else {
				require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
			}
			require.Zero(t, upstreamRefreshes.Load(), "stored credential checks must not contact the upstream")
		})
	}
}

// A provider's definitive refusal must not strand a human whose linked
// upstream session needs reconnecting: the gate challenges as without
// chaining, so reauthorizing reaches the consent page's separate sign-in,
// which every chained card offers whatever the refusal. The reconnected
// session then routes directly and never chains, so even refusals a sign-in
// cannot repair (configuration, invalid target) are served by it. Only
// provider refusals challenge: refresh sees nothing else, so a selection or
// authorization failure would loop between refresh and the challenge and
// stands instead. Without a linked session there is nothing to reconnect and
// the refusal stands; a retryable failure stays a retry.
func TestServePublic_IssuerGate_ChainingRefusalChallengesTheLinkedSession(t *testing.T) {
	t.Parallel()

	type refusal struct {
		stage     identitychaining.Stage
		reason    identitychaining.Reason
		retryable bool
	}
	refusals := []refusal{
		{identitychaining.StageExchange, identitychaining.ReasonAccessDenied, false},
		{identitychaining.StageExchange, identitychaining.ReasonInvalidTarget, false},
		{identitychaining.StageExchange, identitychaining.ReasonConfigurationRequired, false},
		{identitychaining.StageAuthorization, identitychaining.ReasonIneligibleIdentity, false},
		{identitychaining.StageSelection, identitychaining.ReasonConfigurationRequired, false},
		{identitychaining.StageRedemption, identitychaining.ReasonInvalidGrant, false},
		{identitychaining.StageExchange, identitychaining.ReasonUnknownRejection, false},
		{identitychaining.StageExchange, identitychaining.ReasonTransientFailure, true},
	}
	type gateCase struct {
		name    string
		linked  bool
		refusal refusal
	}
	var cases []gateCase
	for _, r := range refusals {
		cases = append(cases,
			gateCase{name: string(r.reason) + "/stale linked session", linked: true, refusal: r},
			gateCase{name: string(r.reason) + "/no linked session", linked: false, refusal: r},
		)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx := requireProjectAuthContext(t, ctx)
			issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
			slug := "chain-refused-" + uuid.NewString()[:8]
			mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://chain-refused.example.test/mcp", slug, "private", issuerID)
			seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockidp.MockUserID, mcpServer.ID.String())
			subject := urn.NewUserSubject(mockidp.MockUserID)
			if tc.linked {
				remoteClient := attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuerID)
				upsertExpiredRefreshableRemoteSession(t, ctx, ti, issuerID, remoteClient.ID, subject)
				pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, remoteClient.ID, func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				})
			}
			token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
				Subject:  subject,
				Audience: urn.NewUserSessionIssuer(issuerID).String(),
				Issuer:   ti.serverURL.String() + "/mcp/" + slug,
				Lifetime: time.Hour,
			})
			require.NoError(t, err)
			persistTestUserSession(t, ti, issuerID, subject, jti)

			refused := identitychaining.Outcome{Stage: tc.refusal.stage, Reason: tc.refusal.reason, Confidence: identitychaining.ConfidenceVerified, Retryable: tc.refusal.retryable, Cached: false, Provider: ""}
			// Refresh sees a refusal only once it is cached, as the chainer reads it.
			refresh := identitychaining.RefreshUnconfirmed
			if refused.ProviderRefusal() {
				refresh = identitychaining.RefreshRefused
			}
			g := &consentGovernor{t: t, issuer: uuid.Nil, served: map[string]bool{}, governs: true, configured: true, usable: false, refresh: refresh, acquire: func(context.Context, identitychaining.Request) identitychaining.Outcome { return refused }, mu: sync.Mutex{}, requests: nil, acquired: nil, refreshed: nil}
			ti.service.SetIdentityChainer(g)

			w, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), token, nil)
			g.mu.Lock()
			acquired := len(g.acquired)
			g.mu.Unlock()
			require.Equal(t, 1, acquired, "the gate tries chaining before answering")
			switch {
			case tc.refusal.retryable:
				require.Contains(t, requireOopsCode(t, err, oops.CodeUnavailable), "retry shortly")
				require.Empty(t, w.Header().Get("WWW-Authenticate"))
			case tc.linked && refused.ProviderRefusal():
				require.Equal(t, reconnectDescription, requireOopsCode(t, err, oops.CodeUnauthorized))
				require.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
			default:
				require.Contains(t, requireOopsCode(t, err, oops.CodeForbidden), "contact your administrator")
				require.Empty(t, w.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func requireProjectAuthContext(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return authCtx
}

// requireOopsCode asserts err carries an oops error with code and returns its
// public message, free of the wrapping the serve path adds.
func requireOopsCode(t *testing.T, err error, code oops.Code) string {
	t.Helper()

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
	return shareable.Error()
}

// attachTestRemoteSessionClient binds a new remote_session_client to the user
// session issuer, giving its sessions an upstream remote session requirement.
func attachTestRemoteSessionClient(t *testing.T, ctx context.Context, ti *testInstance, authCtx *contextvalues.AuthContext, userSessionIssuerID uuid.UUID) remotesessions_repo.RemoteSessionClient {
	t.Helper()

	suffix := uuid.NewString()[:8]
	q := remotesessions_repo.New(ti.conn)
	remoteIssuer, err := q.CreateRemoteSessionIssuer(ctx, remotesessions_repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		Slug:                              "gate-rsi-" + suffix,
		Issuer:                            "https://upstream.example/" + suffix,
		AuthorizationEndpoint:             conv.ToPGText("https://upstream.example/" + suffix + "/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://upstream.example/" + suffix + "/token"),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	})
	require.NoError(t, err)

	remoteClient, err := q.CreateRemoteSessionClient(ctx, remotesessions_repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
		RemoteSessionIssuerID: remoteIssuer.ID,
		ClientID:              "gate-client-" + suffix,
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now()),
	})
	require.NoError(t, err)

	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessions_repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: remoteClient.ID,
		UserSessionIssuerID:   userSessionIssuerID,
	}))
	return remoteClient
}

// upsertExpiredRefreshableRemoteSession links subject with an access token
// that has already expired and a refresh grant, so the next resolution must
// refresh against the client's token endpoint.
func upsertExpiredRefreshableRemoteSession(t *testing.T, ctx context.Context, ti *testInstance, userSessionIssuerID, remoteSessionClientID uuid.UUID, subject urn.SessionSubject) {
	t.Helper()

	accessToken, err := ti.enc.Encrypt([]byte("expired-upstream-access"))
	require.NoError(t, err)
	refreshToken, err := ti.enc.Encrypt([]byte("live-upstream-refresh"))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userSessionIssuerID,
		RemoteSessionClientID: remoteSessionClientID,
		AccessTokenEncrypted:  accessToken,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(-time.Hour)),
		RefreshTokenEncrypted: conv.ToPGText(refreshToken),
		RefreshExpiresAt:      pgtype.Timestamptz{},
		Scopes:                []string{},
		Resource:              pgtype.Text{},
	})
	require.NoError(t, err)
}

// pointRemoteSessionTokenEndpoint redirects the client's issuer to a local
// token endpoint served by handler.
func pointRemoteSessionTokenEndpoint(t *testing.T, ctx context.Context, ti *testInstance, authCtx *contextvalues.AuthContext, remoteSessionClientID uuid.UUID, handler http.HandlerFunc) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	rows, err := testrepo.New(ti.conn).ForceRemoteSessionIssuerTokenEndpointFixture(ctx, testrepo.ForceRemoteSessionIssuerTokenEndpointFixtureParams{
		TokenEndpoint:         conv.ToPGText(server.URL + "/token"),
		RemoteSessionClientID: remoteSessionClientID,
		ProjectID:             *authCtx.ProjectID,
		OrganizationID:        authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
}

// Shared issuers mint both refreshable and access-only resource tokens. The
// challenge must use stored issuance policy on both cache misses and hits.
func TestServePublic_SharedSessionReconnectUsesIssuancePolicy(t *testing.T) {
	t.Parallel()
	sum := sha256.Sum256([]byte("test-refresh"))
	for _, tc := range []struct {
		name        string
		hash        pgtype.Text
		refreshable bool
	}{
		{"refreshable", conv.ToPGText(base64.RawURLEncoding.EncodeToString(sum[:])), true},
		{"id-jag", conv.ToPGText("id-jag:assertion-session"), false},
		{"dashboard", conv.ToPGText("dashboard-mint:session"), false},
		{"without refresh token", pgtype.Text{}, false},
		{"unknown marker", conv.ToPGText("future:session"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			authCtx := requireProjectAuthContext(t, ctx)
			fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "shared-policy")
			setIssuerMode(t, ctx, ti, fixture.UserSessionIssuer.ID, "shared", "")
			subject := urn.NewUserSubject("shared-policy-user")
			token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
				Subject: subject, Audience: ti.serverURL.JoinPath("mcp", fixture.Toolset.McpSlug.String).String(),
				Issuer: ti.serverURL.String() + "/oauth/usi/" + fixture.UserSessionIssuer.ID.String(), Lifetime: time.Hour,
			})
			require.NoError(t, err)
			now := time.Now()
			_, err = usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{
				UserSessionIssuerID: fixture.UserSessionIssuer.ID, SubjectUrn: subject, Jti: jti,
				RefreshTokenHash: tc.hash, ExpiresAt: conv.ToPGTimestamptz(now.Add(time.Hour)),
				RefreshExpiresAt: conv.ToPGTimestamptz(now.Add(time.Hour)),
				SignInID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			})
			require.NoError(t, err)
			for range 2 {
				w, err := servePublicHTTP(t, t.Context(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), token, nil)
				require.Equal(t, reconnectDescription, requireOopsCode(t, err, oops.CodeUnauthorized))
				challenge := w.Header().Get("WWW-Authenticate")
				if tc.refreshable {
					require.Contains(t, challenge, `error="invalid_token"`)
				} else {
					require.NotContains(t, challenge, "error=")
				}
			}
		})
	}
}
