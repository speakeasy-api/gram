// challenge_synthetic_expiry_test.go is the regression guard for AIS-115:
// when an upstream token response omits expires_in, Gram must store
// access_expires_at as NULL — "no known expiry" — rather than fabricating a
// now+1h deadline. NULL always means "the provider did not report an access
// expiry." The request path serves that token as-is regardless of whether a
// refresh grant was also issued; scheduled auto-refresh owns refresh-grant
// keepalive separately.
//
// Pure mock: an httptest server stands in for the upstream token endpoint, so
// no dev-idp round-trip is needed. The flow is BuildAuthorizationUrl (which
// mints + stores the RemoteLoginState) → HandleRemoteLoginCallback (which
// exchanges the code against the mock and persists the row).

package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TestRemoteLoginCallback_NoExpiresIn_NoRefresh_NonExpiring covers the live
// Slack case: an access_token with neither expires_in nor refresh_token. It
// must persist NULL expiry (not now+1h) and keep resolving indefinitely.
func TestRemoteLoginCallback_NoExpiresIn_NoRefresh_NonExpiring(t *testing.T) {
	t.Parallel()

	const upstreamAccessToken = "xoxp-synthetic-expiry-regression"
	ctx, env := newSyntheticExpiryEnv(t, "norefresh", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Verbatim shape from the AIS-115 repro: no expires_in, no refresh_token.
		_, _ = w.Write([]byte(`{"ok":true,"access_token":"` + upstreamAccessToken + `","token_type":"Bearer","scope":"channels:history"}`))
	})

	// The core regression: no expires_in ⇒ NULL access_expires_at (not now+1h),
	// and no refresh token was fabricated either.
	require.False(t, env.session.AccessExpiresAt.Valid, "access_expires_at must be NULL when upstream omits expires_in")
	require.False(t, env.session.RefreshTokenEncrypted.Valid, "no refresh token should be stored")

	// The gate treats NULL-with-no-refresh as non-expiring and keeps returning
	// the token, rather than rejecting it as ErrNoValidToken once a synthetic
	// hour lapses.
	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, upstreamAccessToken, resolved, "non-expiring token must keep resolving")
}

// TestRemoteLoginCallback_NoExpiresIn_WithRefresh_DoesNotForceRefresh covers
// the other NULL-expiry case: the upstream omitted expires_in but DID hand us
// a refresh token. The refresh token's existence does not prove that access
// expires, so even an old row keeps serving the current access token.
func TestRemoteLoginCallback_NoExpiresIn_WithRefresh_DoesNotForceRefresh(t *testing.T) {
	t.Parallel()

	const initialAccess = "access-initial"
	const rotatedAccess = "access-after-refresh"
	var refreshCount atomic.Int64
	ctx, env := newSyntheticExpiryEnv(t, "withrefresh", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		// Both grants omit expires_in but ship a refresh token, so each stored
		// row stays in the NULL-expiry-with-refresh case.
		if r.Form.Get("grant_type") == "refresh_token" {
			refreshCount.Add(1)
			_, _ = w.Write([]byte(`{"access_token":"` + rotatedAccess + `","refresh_token":"refresh-rotated","token_type":"Bearer"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"` + initialAccess + `","refresh_token":"refresh-initial","token_type":"Bearer"}`))
	})

	// Stored as NULL expiry but WITH a refresh token.
	require.False(t, env.session.AccessExpiresAt.Valid, "no expires_in ⇒ NULL access_expires_at")
	require.True(t, env.session.RefreshTokenEncrypted.Valid, "refresh token must be persisted")

	// The stored token is served and no refresh is attempted.
	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, initialAccess, resolved)
	require.Equal(t, int64(0), refreshCount.Load())

	// updated_at is the scheduled keepalive clock, not an implicit access-token
	// expiry. Backdating it must not make the lazy request path refresh.
	require.NoError(t, env.q.SetRemoteSessionUpdatedAt(ctx, repo.SetRemoteSessionUpdatedAtParams{
		ID:        env.session.ID,
		ProjectID: conv.ToNullUUID(env.projectID),
		UpdatedAt: conv.ToPGTimestamptz(time.Now().Add(-48 * time.Hour)),
	}))
	resolved, err = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, initialAccess, resolved)
	require.Equal(t, int64(0), refreshCount.Load(), "unknown access expiry must not force a refresh")
}

func TestRemoteLoginCallback_StandardRefreshExpirationFields(t *testing.T) {
	t.Parallel()

	before := time.Now()
	_, env := newSyntheticExpiryEnv(t, "standard-refresh-expiry", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"access_token":"access",
			"refresh_token":"refresh",
			"token_type":"Bearer",
			"refresh_token_timeout":7200,
			"authorization_expires_in":3600
		}`))
	})

	require.WithinDuration(t, before.Add(2*time.Hour), env.session.RefreshExpiresAt.Time, time.Minute)
	require.WithinDuration(t, before.Add(time.Hour), env.session.AuthorizationExpiresAt.Time, time.Minute)

	states, err := env.mgr.RemoteSessionStatuses(
		t.Context(),
		env.subject,
		env.projectID,
		env.session.UserSessionIssuerID,
	)
	require.NoError(t, err)
	state := states[env.clientID]
	require.True(t, state.CanRefresh)
	require.NotNil(t, state.RefreshExpiresAt)
	require.WithinDuration(t, before.Add(time.Hour), *state.RefreshExpiresAt, time.Minute,
		"the consent tooltip must show the earliest known renewal deadline")
}

// syntheticExpiryEnv is the materialized state after a remote-login round trip
// against a mock upstream token endpoint.
type syntheticExpiryEnv struct {
	mgr *remotesessions.ChallengeManager
	// refresher shares the manager's database, encryption key, and Redis lock
	// cache, standing in for the scheduled sweep's caller in concurrency tests.
	refresher    *remotesessions.RefreshService
	newRefresher func(cache.Cache) *remotesessions.RefreshService
	q            *repo.Queries
	projectID    uuid.UUID
	clientID     uuid.UUID
	subject      urn.SessionSubject
	session      repo.RemoteSession
}

// newSyntheticExpiryEnv wires a ChallengeManager to a mock upstream token
// endpoint (tokenHandler) and drives BuildAuthorizationUrl →
// HandleRemoteLoginCallback, returning the request context plus the persisted
// remote_sessions row and the handles needed to resolve it. slugSuffix keeps
// fixtures unique per test.
func newSyntheticExpiryEnv(t *testing.T, slugSuffix string, tokenHandler http.HandlerFunc) (context.Context, syntheticExpiryEnv) {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	tokenServer := httptest.NewServer(tokenHandler)
	t.Cleanup(tokenServer.Close)

	enc := testenv.NewEncryptionClient(t)
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	// A real Redis-backed cache is required: BuildAuthorizationUrl writes the
	// RemoteLoginState and HandleRemoteLoginCallback reads it back. NoopCache
	// (used by the URL-shape e2e tests) would drop it.
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	mgr := remotesessions.NewChallengeManager(
		logger,
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		enc,
		policy,
		cache.NewRedisCacheAdapter(redisClient),
		mustURL(t, "http://localhost"),
	)
	refresher := remotesessions.NewRefreshService(logger, ti.conn, enc, policy, cache.NewRedisCacheAdapter(redisClient))

	q := repo.New(ti.conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Slug:                              "synthetic-expiry-issuer-" + slugSuffix,
		Issuer:                            "https://idp.example.com",
		Name:                              pgtype.Text{String: "", Valid: false},
		LogoAssetID:                       uuid.NullUUID{},
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText(tokenServer.URL),
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   []string{"channels:history"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "usi-synthetic-"+slugSuffix)

	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "synthetic-cid-" + slugSuffix,
		ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:        pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.ToPGText("none"),
		Scope:                   nil,
	})
	require.NoError(t, err)

	// The join table is the sole read path for client↔issuer bindings, so the
	// row's user_session_issuer_id alone is not enough for ListClients to find it.
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuer,
	}))

	clients, err := mgr.ListClients(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuer)
	require.NoError(t, err)
	require.Len(t, clients, 1)

	// BuildAuthorizationUrl mints + stores the RemoteLoginState; the returned
	// URL carries the opaque state id the callback exchanges against.
	subject := urn.NewUserSubject("synthetic-subject-" + slugSuffix)
	authURL, err := mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID:                  uuid.NewString(),
		ProjectID:           *authCtx.ProjectID,
		UserSessionIssuerID: userIssuer,
		Subject:             &subject,
		McpSlug:             "synthetic-mcp-" + slugSuffix,
		FinalRedirectURI:    "",
	}, clients[0])
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)

	// The upstream code is single-use and opaque to the mock; any non-empty
	// value drives exchangeCode against the token server.
	cbReq := httptest.NewRequest(http.MethodGet,
		"/mcp/remote_login_callback?code=upstream-code&state="+url.QueryEscape(state), nil)
	cbW := httptest.NewRecorder()
	require.NoError(t, mgr.HandleRemoteLoginCallback(cbW, cbReq))
	require.Equal(t, http.StatusSeeOther, cbW.Code)

	session, err := q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: client.ID,
	})
	require.NoError(t, err)

	return ctx, syntheticExpiryEnv{
		mgr:       mgr,
		refresher: refresher,
		newRefresher: func(locks cache.Cache) *remotesessions.RefreshService {
			return remotesessions.NewRefreshService(logger, ti.conn, enc, policy, locks)
		},
		q:         q,
		projectID: *authCtx.ProjectID,
		clientID:  client.ID,
		subject:   subject,
		session:   session,
	}
}
