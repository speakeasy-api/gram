// tokenservice_audience_test.go drives the refresh path against an
// httptest token endpoint and asserts that the upstream POST body
// includes the configured audience value when set, and omits it when
// unset.

package remotesessions_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// spyRefreshHandler is the standard success token endpoint: it captures the
// inbound form + Authorization header into spy and returns a rotated pair.
func spyRefreshHandler(spy *upstreamSpy) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

func setupRefreshFixtureWithAudience(t *testing.T, audience pgtype.Text, spy *upstreamSpy) (context.Context, *remotesessions.ChallengeManager, *testInstance, uuid.UUID, urn.SessionSubject) {
	t.Helper()

	slugSuffix := "aud-set"
	if !audience.Valid {
		slugSuffix = "aud-unset"
	}
	return setupRefreshFixtureWithHandler(t, slugSuffix, audience, spyRefreshHandler(spy))
}

// setupRefreshFixtureWithHandler seeds issuer + client + expired NULL-resource
// session rows against a custom token endpoint handler, with per-test slugs.
func setupRefreshFixtureWithHandler(t *testing.T, slugSuffix string, audience pgtype.Text, handler http.HandlerFunc) (context.Context, *remotesessions.ChallengeManager, *testInstance, uuid.UUID, urn.SessionSubject) {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	tokenServer := httptest.NewServer(handler)
	t.Cleanup(tokenServer.Close)

	enc := testenv.NewEncryptionClient(t)
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	mgr := remotesessions.NewChallengeManager(
		logger,
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		enc,
		policy,
		cache.NoopCache,
		mustURL(t, "http://localhost"),
	)

	q := repo.New(ti.conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Slug:                              "refresh-" + slugSuffix,
		Issuer:                            tokenServer.URL,
		AuthorizationEndpoint:             conv.ToPGText(tokenServer.URL + "/authorize"),
		TokenEndpoint:                     conv.ToPGText(tokenServer.URL + "/token"),
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

	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "usi-refresh-"+slugSuffix)

	secretCiphertext, err := enc.Encrypt([]byte("aud-secret"))
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "aud-cid",
		ClientSecretEncrypted:   conv.ToPGText(secretCiphertext),
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.ToPGText("client_secret_post"),
		Scope:                   nil,
		Audience:                audience,
	})
	require.NoError(t, err)

	err = q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuer,
	})
	require.NoError(t, err)

	subject := urn.NewUserSubject("refresh-subject-" + slugSuffix)
	seedExpiredRemoteSession(t, ctx, ti, enc, subject, userIssuer, client.ID)

	return ctx, mgr, ti, client.ID, subject
}

func TestResolveAccessToken_RefreshIncludesAudience(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, _, clientID, subject := setupRefreshFixtureWithAudience(t, conv.ToPGText("https://api.example.com"), &spy)

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, "")
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	require.Equal(t, "https://api.example.com", spy.form.Get("audience"), "refresh body must echo client.audience when set")
}

func TestResolveAccessToken_RefreshOmitsAudienceWhenUnset(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, _, clientID, subject := setupRefreshFixtureWithAudience(t, pgtype.Text{String: "", Valid: false}, &spy)

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, "")
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	require.False(t, spy.form.Has("audience"), "refresh body must omit audience when client.audience is unset")
}
