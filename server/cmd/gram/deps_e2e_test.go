package gram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/e2econtrol"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	testE2EControlToken = "zz-e2e-not-a-secret-zz-e2e-not-a-secret"
	testDevIDPURL       = "http://localhost:35291"
	testEmulatorURL     = testDevIDPURL + "/workos"
)

// attachTestE2EControl runs the startup decision with no database or identity
// provider behind it (neither is touched by the calls below) and reports the
// status each route then answers.
func attachTestE2EControl(t *testing.T, environment, identityBackend, identityEndpoint, token string) (map[string]int, error) {
	t.Helper()
	siteURL, err := url.Parse("https://site.example.test")
	require.NoError(t, err)
	redisClient := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	mux := goahttp.NewMuxer()
	err = attachE2EControl(t.Context(), testenv.NewLogger(t), mux, e2eControlDeps{
		environment:      environment,
		identityBackend:  identityBackend,
		identityEndpoint: identityEndpoint,
		devIDPURL:        testDevIDPURL,
		token:            token,
		developerEmail:   func(context.Context) string { return "developer@example.test" },
		siteURL:          siteURL,
		serverURL:        siteURL,
		issuerURL:        "",
		db:               nil,
		clickhouse:       nil,
		blob:             nil,
		encryption:       nil,
		redis:            redisClient,
		identity:         nil,
		sessions:         nil,
	})
	statuses := map[string]int{}
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/rpc/e2e.tenant"},
		{http.MethodGet, "/rpc/e2e.signIn"},
		// Asked without the token, so a mounted route answers 401 and nothing runs.
		{http.MethodPost, "/rpc/e2e.reseed"},
		{http.MethodPost, "/rpc/e2e.signInLink"},
	} {
		req := httptest.NewRequestWithContext(t.Context(), route.method, route.path, nil)
		if route.method == http.MethodGet {
			req.Header.Set(e2econtrol.TokenHeader, testE2EControlToken)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		statuses[route.path] = rec.Code
	}
	return statuses, err
}

var e2eControlNotMounted = map[string]int{
	"/rpc/e2e.tenant":     http.StatusNotFound,
	"/rpc/e2e.signIn":     http.StatusNotFound,
	"/rpc/e2e.reseed":     http.StatusNotFound,
	"/rpc/e2e.signInLink": http.StatusNotFound,
}

func TestE2EControlIsOffWithoutAToken(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"local", "dev", "prod"} {
		statuses, err := attachTestE2EControl(t, environment, "local", testEmulatorURL, "")
		require.NoError(t, err, environment)
		require.Equal(t, e2eControlNotMounted, statuses, environment)
	}
}

func TestE2EControlTokenStopsStartupOutsideLocal(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"prod", "production", "dev", "preview", "staging", "Local", ""} {
		statuses, err := attachTestE2EControl(t, environment, "local", testEmulatorURL, testE2EControlToken)
		require.ErrorContains(t, err, "exists only in", environment)
		require.Equal(t, e2eControlNotMounted, statuses, environment)
	}
}

func TestE2EControlTokenStopsStartupWithARealIdentityProvider(t *testing.T) {
	t.Parallel()
	for name, identity := range map[string]struct{ backend, endpoint string }{
		"the dev-idp forwarding to a real provider": {"workos", testEmulatorURL},
		"no backend named":                          {"", testEmulatorURL},
		"no endpoint, so the provider's own":        {"local", ""},
		"an endpoint that is not the dev-idp":       {"local", "https://api.example.test"},
	} {
		statuses, err := attachTestE2EControl(t, "local", identity.backend, identity.endpoint, testE2EControlToken)
		require.ErrorContains(t, err, "dev-idp emulator only", name)
		require.Equal(t, e2eControlNotMounted, statuses, name)
	}
}

func TestE2EControlIsMountedLocallyBehindTheToken(t *testing.T) {
	t.Parallel()
	statuses, err := attachTestE2EControl(t, "local", "local", testEmulatorURL, testE2EControlToken)
	require.NoError(t, err)
	require.Equal(t, map[string]int{
		"/rpc/e2e.tenant":     http.StatusOK,
		"/rpc/e2e.signIn":     http.StatusForbidden, // no code
		"/rpc/e2e.reseed":     http.StatusUnauthorized,
		"/rpc/e2e.signInLink": http.StatusUnauthorized,
	}, statuses)
}

// Only the API server takes the token: a command that accepted it without
// mounting the surface could not refuse to start with it.
func TestE2EControlTokenFlagBelongsToTheServerOnly(t *testing.T) {
	t.Parallel()
	has := func(flags []cli.Flag) bool {
		return slices.ContainsFunc(flags, func(f cli.Flag) bool { return slices.Contains(f.Names(), e2eControlTokenFlag) })
	}
	require.True(t, has(serverFlags()))
	require.False(t, has(mcpRuntimeFlags()))
}
