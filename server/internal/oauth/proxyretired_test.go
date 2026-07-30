package oauth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// retiredProxyEnv is a proxy-backed toolset plus a mux serving the oauth
// endpoints against the same database.
type retiredProxyEnv struct {
	mux     goahttp.Muxer
	mcpSlug string
	// attachIssuer links a user_session_issuer to the toolset, which is what
	// retires the proxy for it.
	attachIssuer func(t *testing.T)
}

func newRetiredProxyEnv(t *testing.T) (context.Context, *retiredProxyEnv) {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "proxyretired")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient,
		cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))

	ctx := authztest.InitAuthContext(t, t.Context(), conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context must be initialized")

	proxy := oauthtest.CreateProxyToolset(t, ctx, conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:         "retired-proxy",
		IsPublic:     true,
		ProviderType: "gram",
	})

	env := oauthtest.NewOAuthServiceEnvWithDB(t, conn, cache.NewRedisCacheAdapter(redisClient), enc)
	mux := goahttp.NewMuxer()
	oauth.Attach(mux, env.Service)

	return ctx, &retiredProxyEnv{
		mux:     mux,
		mcpSlug: proxy.Toolset.McpSlug.String,
		attachIssuer: func(t *testing.T) {
			t.Helper()

			issuer, err := usersessions_repo.New(conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
				ProjectID:          *authCtx.ProjectID,
				Slug:               "issuer-" + uuid.New().String()[:8],
				AuthnChallengeMode: "interactive",
				SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
			})
			require.NoError(t, err)

			_, err = toolsets_repo.New(conn).UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
				UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
				Slug:                proxy.Toolset.Slug,
				ProjectID:           proxy.Toolset.ProjectID,
			})
			require.NoError(t, err)
		},
	}
}

func (e *retiredProxyEnv) postToken(t *testing.T, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", "a-stale-proxy-refresh-token")
	form.Set("client_id", "some-client")

	req := httptest.NewRequestWithContext(ctx, http.MethodPost,
		"/oauth/"+e.mcpSlug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)

	return rec
}

// A client holding a proxy refresh token for a migrated server must be told
// invalid_grant, which is the signal it acts on by discarding the token and
// re-running authorization against the issuer.
func TestTokenEndpointRefusesIssuerGatedToolsetWithInvalidGrant(t *testing.T) {
	t.Parallel()

	ctx, env := newRetiredProxyEnv(t)
	env.attachIssuer(t)

	rec := env.postToken(t, ctx)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	body := rec.Body.String()
	require.Contains(t, body, `"error":"invalid_grant"`)
	require.Contains(t, body, "Re-authorize to continue")
}

// Without an issuer the proxy still serves; the request fails on its own
// merits (a bogus refresh token) rather than on the retirement gate.
func TestTokenEndpointStillServesToolsetWithoutIssuer(t *testing.T) {
	t.Parallel()

	ctx, env := newRetiredProxyEnv(t)

	rec := env.postToken(t, ctx)

	require.NotContains(t, rec.Body.String(), "moved to a new authorization server",
		"a toolset with no issuer must not be treated as retired")
}

func TestAuthorizeEndpointRefusesIssuerGatedToolset(t *testing.T) {
	t.Parallel()

	ctx, env := newRetiredProxyEnv(t)
	env.attachIssuer(t)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet,
		"/oauth/"+env.mcpSlug+"/authorize?response_type=code&client_id=c&redirect_uri=http://localhost/cb", nil)
	rec := httptest.NewRecorder()
	env.mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code,
		"a retired proxy must not start a new authorization")
}

func TestRegisterEndpointRefusesIssuerGatedToolset(t *testing.T) {
	t.Parallel()

	ctx, env := newRetiredProxyEnv(t)
	env.attachIssuer(t)

	req := httptest.NewRequestWithContext(ctx, http.MethodPost,
		"/oauth/"+env.mcpSlug+"/register",
		strings.NewReader(`{"client_name":"new client","redirect_uris":["http://localhost/cb"]}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	env.mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code,
		"a retired proxy must not accept new client registrations")
}
