// tokenservice_authmethod_test.go verifies the upstream-facing serialization
// of token_endpoint_auth_method on the refresh path. exchangeCode shares the
// same branching, so the post-vs-basic check here also covers initial login
// by construction.

package remotesessions_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// upstreamSpy captures the form values + Authorization header of an inbound
// refresh request so a test can assert which auth method shape was used.
type upstreamSpy struct {
	form       url.Values
	authHdr    string
	handlerErr error
}

// setupRefreshFixture seeds the issuer + client + expired session rows
// needed to drive validateAndRefresh, points the token endpoint at an
// httptest.Server, and returns a ChallengeManager wired to the same
// db/encryption client.
func setupRefreshFixture(t *testing.T, authMethod string, spy *upstreamSpy) (context.Context, *remotesessions.ChallengeManager, uuid.UUID, urn.SessionSubject, string, string) {
	t.Helper()

	const (
		externalCID  = "post-cid"
		clientSecret = "post-secret"
	)

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Errors inside this handler can't fail the test directly
		// (testifylint go-require) — capture them and let the
		// surrounding test goroutine assert after the round-trip.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			spy.handlerErr = err
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			spy.handlerErr = err
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		spy.form = form
		spy.authHdr = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"refreshed-access","token_type":"Bearer","expires_in":3600,"refresh_token":"refreshed-refresh"}`))
	}))
	t.Cleanup(tokenServer.Close)

	// Build deps for ChallengeManager. Each must share state with the
	// rows we're about to insert: same db pool, same encryption client.
	enc := testenv.NewEncryptionClient(t)
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	mgr := remotesessions.NewChallengeManager(
		logger,
		ti.conn,
		enc,
		policy,
		cache.NoopCache,
		mustURL(t, "http://localhost"),
	)

	q := repo.New(ti.conn)
	authzEP := tokenServer.URL + "/authorize"
	tokenEP := tokenServer.URL + "/token"
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         *authCtx.ProjectID,
		Slug:                              "auth-method-" + strings.ReplaceAll(authMethod, "_", "-"),
		Issuer:                            tokenServer.URL,
		AuthorizationEndpoint:             conv.ToPGText(authzEP),
		TokenEndpoint:                     conv.ToPGText(tokenEP),
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post"},
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "usi-"+strings.ReplaceAll(authMethod, "_", "-"))

	secretCiphertext, err := enc.Encrypt([]byte(clientSecret))
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               *authCtx.ProjectID,
		RemoteSessionIssuerID:   issuer.ID,
		UserSessionIssuerID:     userIssuer,
		ClientID:                externalCID,
		ClientSecretEncrypted:   conv.ToPGText(secretCiphertext),
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.ToPGText(authMethod),
	})
	require.NoError(t, err)

	subject := urn.NewUserSubject("refresh-subject-" + authMethod)
	seedExpiredRemoteSession(t, ctx, ti, enc, subject, userIssuer, client.ID)

	return ctx, mgr, client.ID, subject, clientSecret, externalCID
}

// seedExpiredRemoteSession inserts a remote_sessions row whose access token
// is already expired but which carries a refresh token, so the first call to
// ResolveAccessToken takes the refresh branch.
func seedExpiredRemoteSession(t *testing.T, ctx context.Context, ti *testInstance, enc *encryption.Client, subject urn.SessionSubject, userIssuerID, clientID uuid.UUID) {
	t.Helper()

	accessEnc, err := enc.Encrypt([]byte("stale-access"))
	require.NoError(t, err)
	refreshEnc, err := enc.Encrypt([]byte("upstream-refresh"))
	require.NoError(t, err)
	refreshEncStr := refreshEnc

	_, err = repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(-time.Minute)),
		RefreshTokenEncrypted: conv.PtrToPGText(&refreshEncStr),
		RefreshExpiresAt:      pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		Scopes:                []string{},
	})
	require.NoError(t, err)
}

func TestResolveAccessToken_RefreshUsesClientSecretPost(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, clientID, subject, clientSecret, externalCID := setupRefreshFixture(t, "client_secret_post", &spy)

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject)
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	require.Equal(t, externalCID, spy.form.Get("client_id"), "client_id is in the body regardless of auth method")
	require.Equal(t, clientSecret, spy.form.Get("client_secret"), "client_secret_post puts secret in the form body")
	require.Empty(t, spy.authHdr, "client_secret_post must not also send Authorization header")
}

func TestResolveAccessToken_RefreshUsesClientSecretBasic(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, clientID, subject, _, _ := setupRefreshFixture(t, "client_secret_basic", &spy)

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject)
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	require.Empty(t, spy.form.Get("client_secret"), "client_secret_basic must not echo secret in the body")
	require.Empty(t, spy.form.Get("client_id"), "client_secret_basic must not duplicate client_id in the body — WorkOS rejects it")
	require.True(t, strings.HasPrefix(spy.authHdr, "Basic "), "client_secret_basic uses Authorization: Basic")
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}
