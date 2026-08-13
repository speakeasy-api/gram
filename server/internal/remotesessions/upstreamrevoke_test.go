// upstreamrevoke_test.go drives the RFC 7009 client side through the real
// RevokeRemoteSession handler rather than calling UpstreamRevoker directly.
//
// That is deliberate. The property worth protecting is not "the revoker builds
// a correct POST" — it is "revoking a session always soft-deletes locally and
// never surfaces an upstream's behavior to the caller." Testing the revoker in
// isolation would verify the first half and leave the half that matters (the
// commit ordering, the swallowed failure, the missing endpoint) unexercised.

package remotesessions_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orgsessionsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_sessions"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// revocationSpy records what the fake upstream received. The revoke runs
// synchronously inside the handler, so a test can read these fields straight
// after the call returns — but the httptest handler still runs on its own
// goroutine, so the mutex is load-bearing under -race.
type revocationSpy struct {
	mu    sync.Mutex
	calls int
	// forms holds every request body received, in arrival order. A bulk revoke
	// sends one per session, so the batch tests assert on the whole set.
	forms   []url.Values
	authHdr string
	// status is what the fake upstream answers with. Zero means 200, which is
	// what RFC 7009 §2.2 requires on success.
	status int
}

// snapshot returns the call count and the most recent request. The
// single-session tests expect exactly one call, for which "most recent" and
// "the one" are the same thing.
func (s *revocationSpy) snapshot() (calls int, form url.Values, authHdr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var last url.Values
	if len(s.forms) > 0 {
		last = s.forms[len(s.forms)-1]
	}
	return s.calls, last, s.authHdr
}

// revokedTokens returns the token value of every request received, sorted.
// Sorted because a batch runs under bounded concurrency and its completion
// order is not a property worth pinning.
func (s *revocationSpy) revokedTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokens := make([]string, 0, len(s.forms))
	for _, form := range s.forms {
		tokens = append(tokens, form.Get("token"))
	}
	slices.Sort(tokens)
	return tokens
}

// newRevocationUpstream starts a fake authorization server whose /revoke
// endpoint records the request. Handler-side errors are recorded as a 500
// rather than failing the test from a non-test goroutine (testifylint
// go-require).
func newRevocationUpstream(t *testing.T, spy *revocationSpy) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		spy.mu.Lock()
		spy.calls++
		spy.forms = append(spy.forms, form)
		spy.authHdr = r.Header.Get("Authorization")
		status := spy.status
		spy.mu.Unlock()

		if status != 0 {
			w.WriteHeader(status)
			return
		}
		// RFC 7009 §2.2: 200 with an empty body.
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

// revokeFixture is one seeded project-scoped session ready to be revoked, plus
// the identifiers a test needs to assert against.
type revokeFixture struct {
	sessionID   uuid.UUID
	subject     urn.SessionSubject
	clientID    uuid.UUID
	accessToken string
	// refreshToken is "" when the fixture was seeded access-token-only.
	refreshToken string
	externalCID  string

	// userIssuerID and projectID scope the consent-screen disconnect, which
	// resolves its target from the challenge state rather than a session id.
	userIssuerID uuid.UUID
	projectID    uuid.UUID
}

// seedRevocableSession creates issuer → client → session with real ciphertext,
// so the revoker's decrypt step is exercised rather than stubbed.
//
// revocationEndpoint is stored verbatim: pass "" for the issuer-advertises-none
// case. clientSecret "" registers a public client (token_endpoint_auth_method
// none), which changes where RFC 6749 §2.3 puts the client id.
func seedRevocableSession(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	slug string,
	revocationEndpoint string,
	clientSecret string,
	withRefreshToken bool,
) revokeFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	enc := testenv.NewEncryptionClient(t)
	q := repo.New(ti.conn)

	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		OrganizationID:                    conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		RevocationEndpoint:                conv.ToPGTextEmpty(revocationEndpoint),
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "usi-"+slug)

	externalCID := slug + "-cid"
	authMethod := "none"
	secretPG := pgtype.Text{String: "", Valid: false}
	if clientSecret != "" {
		authMethod = "client_secret_post"
		ciphertext, encErr := enc.Encrypt([]byte(clientSecret))
		require.NoError(t, encErr)
		secretPG = conv.ToPGText(ciphertext)
	}

	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                externalCID,
		ClientSecretEncrypted:   secretPG,
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.ToPGTextEmpty(authMethod),
	})
	require.NoError(t, err)

	err = q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuer,
	})
	require.NoError(t, err)

	accessToken := slug + "-access"
	accessEnc, err := enc.Encrypt([]byte(accessToken))
	require.NoError(t, err)

	refreshToken := ""
	refreshPG := pgtype.Text{String: "", Valid: false}
	if withRefreshToken {
		refreshToken = slug + "-refresh"
		refreshEnc, encErr := enc.Encrypt([]byte(refreshToken))
		require.NoError(t, encErr)
		refreshPG = conv.ToPGText(refreshEnc)
	}

	subject := urn.NewUserSubject("revoke-subject-" + slug)
	session, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userIssuer,
		RemoteSessionClientID: client.ID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: refreshPG,
		RefreshExpiresAt:      pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		Scopes:                []string{},
		Resource:              pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	return revokeFixture{
		sessionID:    session.ID,
		subject:      subject,
		clientID:     client.ID,
		accessToken:  accessToken,
		refreshToken: refreshToken,
		externalCID:  externalCID,
		userIssuerID: userIssuer,
		projectID:    *authCtx.ProjectID,
	}
}

// seedRevocableClient creates one issuer → client carrying sessionCount
// sessions, the shape the bulk and cascade paths operate on. Every session gets
// a distinct refresh token so a test can prove each one was sent individually
// rather than one being sent repeatedly.
func seedRevocableClient(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	slug string,
	revocationEndpoint string,
	sessionCount int,
) (clientID uuid.UUID, fixtures []revokeFixture) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	enc := testenv.NewEncryptionClient(t)
	q := repo.New(ti.conn)

	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		OrganizationID:                    conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		RevocationEndpoint:                conv.ToPGTextEmpty(revocationEndpoint),
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "usi-"+slug)

	externalCID := slug + "-cid"
	secretCiphertext, err := enc.Encrypt([]byte("s3cret"))
	require.NoError(t, err)

	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                externalCID,
		ClientSecretEncrypted:   conv.ToPGText(secretCiphertext),
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.ToPGTextEmpty("client_secret_post"),
	})
	require.NoError(t, err)

	err = q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuer,
	})
	require.NoError(t, err)

	fixtures = make([]revokeFixture, 0, sessionCount)
	for i := range sessionCount {
		suffix := fmt.Sprintf("%s-%d", slug, i)

		accessEnc, encErr := enc.Encrypt([]byte(suffix + "-access"))
		require.NoError(t, encErr)
		refreshToken := suffix + "-refresh"
		refreshEnc, encErr := enc.Encrypt([]byte(refreshToken))
		require.NoError(t, encErr)

		subject := urn.NewUserSubject("revoke-subject-" + suffix)
		session, sessErr := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
			SubjectUrn:            subject,
			UserSessionIssuerID:   userIssuer,
			RemoteSessionClientID: client.ID,
			AccessTokenEncrypted:  accessEnc,
			AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
			RefreshTokenEncrypted: conv.ToPGText(refreshEnc),
			RefreshExpiresAt:      pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			Scopes:                []string{},
			Resource:              pgtype.Text{String: "", Valid: false},
		})
		require.NoError(t, sessErr)

		fixtures = append(fixtures, revokeFixture{
			sessionID:    session.ID,
			subject:      subject,
			clientID:     client.ID,
			accessToken:  suffix + "-access",
			refreshToken: refreshToken,
			externalCID:  externalCID,
			userIssuerID: userIssuer,
			projectID:    *authCtx.ProjectID,
		})
	}

	return client.ID, fixtures
}

// requireSessionRevoked asserts the local soft-delete committed. Every test
// here checks it, because the whole point of the best-effort design is that
// the local revoke is unconditional.
func requireSessionRevoked(t *testing.T, ctx context.Context, ti *testInstance, fx revokeFixture) {
	t.Helper()

	// The active-session lookup filters soft-deleted rows, so no-rows is the
	// assertion: the binding the MCP runtime would resolve is gone.
	_, err := repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "local revoke must have committed regardless of the upstream result")
}

// The refresh token is the credential worth revoking: RFC 7009 §2.1 says
// revoking it SHOULD invalidate the access tokens derived from it, whereas
// revoking the access token leaves the refresh grant free to mint replacements.
func TestRevokeRemoteSession_RevokesRefreshTokenUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	upstream := newRevocationUpstream(t, spy)

	fx := seedRevocableSession(t, ctx, ti, "revoke-refresh", upstream.URL+"/revoke", "s3cret", true)

	err := ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{ID: fx.sessionID.String()})
	require.NoError(t, err)

	calls, form, authHdr := spy.snapshot()
	require.Equal(t, 1, calls, "exactly one RFC 7009 request")
	require.Equal(t, fx.refreshToken, form.Get("token"))
	require.Equal(t, "refresh_token", form.Get("token_type_hint"))
	// client_secret_post: RFC 6749 §2.3 puts the credentials in the body and
	// nothing in the Authorization header.
	require.Equal(t, fx.externalCID, form.Get("client_id"))
	require.Equal(t, "s3cret", form.Get("client_secret"))
	require.Empty(t, authHdr)

	requireSessionRevoked(t, ctx, ti, fx)
}

// A session that never received a refresh token still has an access token worth
// destroying, and a public client authenticates by client_id alone.
func TestRevokeRemoteSession_RevokesAccessTokenWhenNoRefreshToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	upstream := newRevocationUpstream(t, spy)

	fx := seedRevocableSession(t, ctx, ti, "revoke-access-only", upstream.URL+"/revoke", "", false)

	err := ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{ID: fx.sessionID.String()})
	require.NoError(t, err)

	calls, form, authHdr := spy.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, fx.accessToken, form.Get("token"))
	require.Equal(t, "access_token", form.Get("token_type_hint"))
	require.Equal(t, fx.externalCID, form.Get("client_id"))
	require.Empty(t, form.Get("client_secret"), "a public client sends no secret")
	require.Empty(t, authHdr)

	requireSessionRevoked(t, ctx, ti, fx)
}

// Most upstreams advertise no revocation_endpoint. That must be a silent no-op,
// not an error and not a request to some fallback URL.
func TestRevokeRemoteSession_NoRevocationEndpointSkipsUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	// Started but never advertised: if the revoker invented an endpoint from the
	// issuer URL or the token endpoint, the spy would catch it.
	newRevocationUpstream(t, spy)

	fx := seedRevocableSession(t, ctx, ti, "revoke-no-endpoint", "", "s3cret", true)

	err := ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{ID: fx.sessionID.String()})
	require.NoError(t, err)

	calls, _, _ := spy.snapshot()
	require.Zero(t, calls, "an issuer advertising no revocation endpoint must not be contacted")

	requireSessionRevoked(t, ctx, ti, fx)
}

// An upstream that answers with an error is the upstream's problem. The caller
// asked for a local revoke, the local revoke happened, and the response says so.
func TestRevokeRemoteSession_UpstreamErrorStillSucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{status: http.StatusInternalServerError}
	upstream := newRevocationUpstream(t, spy)

	fx := seedRevocableSession(t, ctx, ti, "revoke-upstream-500", upstream.URL+"/revoke", "s3cret", true)

	err := ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{ID: fx.sessionID.String()})
	require.NoError(t, err, "an upstream 5xx must not surface to the caller")

	calls, _, _ := spy.snapshot()
	require.Equal(t, 1, calls, "the attempt was made")

	requireSessionRevoked(t, ctx, ti, fx)
}

// The unreachable case is distinct from the error case: no answer at all rather
// than a bad one. Pointing at a closed listener exercises the connection-error
// branch without waiting out a timeout.
func TestRevokeRemoteSession_UnreachableUpstreamStillSucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	fx := seedRevocableSession(t, ctx, ti, "revoke-unreachable", deadURL+"/revoke", "s3cret", true)

	err := ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{ID: fx.sessionID.String()})
	require.NoError(t, err, "an unreachable identity provider must not surface to the caller")

	requireSessionRevoked(t, ctx, ti, fx)
}

// A bulk revoke has to reach the upstream once per session. The failure this
// guards against is a batch that revokes locally and pushes one token, or none.
func TestRevokeAllClientSessions_RevokesEverySessionUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	upstream := newRevocationUpstream(t, spy)

	clientID, fixtures := seedRevocableClient(t, ctx, ti, "revokeall-upstream", upstream.URL+"/revoke", 3)

	result, err := ti.service.RevokeAllClientSessions(ctx, &orgsessionsgen.RevokeAllClientSessionsPayload{
		ClientID:     clientID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, 3, result.RevokedCount)

	want := []string{
		fixtures[0].refreshToken,
		fixtures[1].refreshToken,
		fixtures[2].refreshToken,
	}
	slices.Sort(want)
	require.Equal(t, want, spy.revokedTokens(), "every session's refresh token must be sent, exactly once each")

	for _, fx := range fixtures {
		requireSessionRevoked(t, ctx, ti, fx)
	}
}

// The quiet-skip guarantee has to hold for batches too: an issuer advertising
// no revocation endpoint means no requests at all, not one per session.
func TestRevokeAllClientSessions_NoRevocationEndpointSkipsUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	newRevocationUpstream(t, spy)

	clientID, fixtures := seedRevocableClient(t, ctx, ti, "revokeall-no-endpoint", "", 3)

	result, err := ti.service.RevokeAllClientSessions(ctx, &orgsessionsgen.RevokeAllClientSessionsPayload{
		ClientID:     clientID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, 3, result.RevokedCount)

	calls, _, _ := spy.snapshot()
	require.Zero(t, calls, "an issuer advertising no revocation endpoint must not be contacted")

	for _, fx := range fixtures {
		requireSessionRevoked(t, ctx, ti, fx)
	}
}

// RevokedCount reports local tombstones, which are what the operator asked for.
// An upstream refusing every request must not change it or fail the call.
func TestRevokeAllClientSessions_UpstreamErrorsStillSucceed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{status: http.StatusInternalServerError}
	upstream := newRevocationUpstream(t, spy)

	clientID, fixtures := seedRevocableClient(t, ctx, ti, "revokeall-upstream-500", upstream.URL+"/revoke", 2)

	result, err := ti.service.RevokeAllClientSessions(ctx, &orgsessionsgen.RevokeAllClientSessionsPayload{
		ClientID:     clientID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err, "an upstream 5xx must not surface to the caller")
	require.Equal(t, 2, result.RevokedCount, "RevokedCount counts local tombstones, not upstream acknowledgements")

	calls, _, _ := spy.snapshot()
	require.Equal(t, 2, calls, "both attempts were made")

	for _, fx := range fixtures {
		requireSessionRevoked(t, ctx, ti, fx)
	}
}

// Deleting a client cascades a soft-delete to its sessions, and those tokens
// are exactly the ones that must die upstream — the operator is dismantling the
// integration. The client row is tombstoned in the same transaction, so this
// also pins the revoker's lookup tolerating a soft-deleted client: a lookup
// filtering on deleted would make this path silently revoke nothing.
func TestDeleteRemoteSessionClient_RevokesCascadedSessionsUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	upstream := newRevocationUpstream(t, spy)

	clientID, fixtures := seedRevocableClient(t, ctx, ti, "delete-client-cascade", upstream.URL+"/revoke", 2)

	err := ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{
		ID:               clientID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	want := []string{fixtures[0].refreshToken, fixtures[1].refreshToken}
	slices.Sort(want)
	require.Equal(t, want, spy.revokedTokens(), "a cascaded soft-delete must still push every token upstream")

	for _, fx := range fixtures {
		requireSessionRevoked(t, ctx, ti, fx)
	}
}

// newDisconnectChallengeManager builds the manager behind the consent screen
// against the same pool and encryption key the fixtures were seeded with, so
// the disconnect decrypts the very tokens it is expected to send upstream.
func newDisconnectChallengeManager(t *testing.T, ti *testInstance) *remotesessions.ChallengeManager {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	return remotesessions.NewChallengeManager(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		testenv.NewEncryptionClient(t),
		policy,
		cache.NoopCache,
		mustURL(t, "http://localhost"),
	)
}

// The consent screen's per-provider "Disconnect" is the one session-ending path
// an end user drives rather than an admin. Dropping the row while the provider
// still honours the refresh grant is precisely what the user asked not to
// happen, so this path revokes upstream like every admin-driven one.
func TestDisconnectRemoteSession_RevokesUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	upstream := newRevocationUpstream(t, spy)

	fx := seedRevocableSession(t, ctx, ti, "disconnect-revokes", upstream.URL+"/revoke", "s3cret", true)

	n, err := newDisconnectChallengeManager(t, ti).DisconnectRemoteSession(ctx, fx.subject, fx.projectID, fx.userIssuerID, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	calls, form, _ := spy.snapshot()
	require.Equal(t, 1, calls, "exactly one RFC 7009 request")
	require.Equal(t, fx.refreshToken, form.Get("token"))
	require.Equal(t, "refresh_token", form.Get("token_type_hint"))
	require.Equal(t, fx.externalCID, form.Get("client_id"))

	requireSessionRevoked(t, ctx, ti, fx)
}

// Most upstreams advertise no revocation_endpoint, and a disconnect against one
// of those must stay a silent local no-op rather than erroring or reaching for
// some fallback URL.
func TestDisconnectRemoteSession_NoRevocationEndpointSkipsUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	// Stood up only to prove nothing reaches it: the issuer below is seeded
	// with no revocation endpoint at all.
	newRevocationUpstream(t, spy)

	fx := seedRevocableSession(t, ctx, ti, "disconnect-no-endpoint", "", "s3cret", true)

	n, err := newDisconnectChallengeManager(t, ti).DisconnectRemoteSession(ctx, fx.subject, fx.projectID, fx.userIssuerID, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	calls, _, _ := spy.snapshot()
	require.Zero(t, calls, "no revocation endpoint means no upstream request")

	requireSessionRevoked(t, ctx, ti, fx)
}

// An upstream that refuses the revocation must not turn a disconnect the user
// already performed into a visible failure.
func TestDisconnectRemoteSession_UpstreamErrorStillDisconnects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)

	fx := seedRevocableSession(t, ctx, ti, "disconnect-upstream-500", upstream.URL+"/revoke", "s3cret", true)

	n, err := newDisconnectChallengeManager(t, ti).DisconnectRemoteSession(ctx, fx.subject, fx.projectID, fx.userIssuerID, fx.clientID)
	require.NoError(t, err, "an upstream failure must not surface to the consent screen")
	require.Equal(t, int64(1), n)

	requireSessionRevoked(t, ctx, ti, fx)
}
