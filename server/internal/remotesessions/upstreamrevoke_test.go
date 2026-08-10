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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// revocationSpy records what the fake upstream received. The revoke runs
// synchronously inside the handler, so a test can read these fields straight
// after the call returns — but the httptest handler still runs on its own
// goroutine, so the mutex is load-bearing under -race.
type revocationSpy struct {
	mu      sync.Mutex
	calls   int
	form    url.Values
	authHdr string
	// status is what the fake upstream answers with. Zero means 200, which is
	// what RFC 7009 §2.2 requires on success.
	status int
}

func (s *revocationSpy) snapshot() (calls int, form url.Values, authHdr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.form, s.authHdr
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
		spy.form = form
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
	}
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
