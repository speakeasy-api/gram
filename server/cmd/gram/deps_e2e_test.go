package gram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/e2econtrol"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const testE2EControlToken = "zz-e2e-not-a-secret-zz-e2e-not-a-secret"

// e2eStartup is one server as its deployment configures it.
type e2eStartup struct {
	environment string
	token       string
	database    string
	site        string
	server      string
}

// A pull request preview as the preview chart renders it.
func previewStartup() e2eStartup {
	return e2eStartup{
		environment: "dev",
		token:       testE2EControlToken,
		database:    "gram_preview_pr_123",
		site:        "https://pr-123.dev.example.test",
		server:      "https://pr-123.dev.example.test",
	}
}

// recordingSessions records which user the surface confined.
type recordingSessions struct {
	confined map[string]string
}

func (recordingSessions) SignInEnforced(context.Context, string) bool { return false }

func (recordingSessions) Issue(context.Context, http.ResponseWriter, sessions.Session, time.Time) error {
	return nil
}

func (recordingSessions) RaiseUserEpoch(context.Context, string, time.Time) error { return nil }

func (r recordingSessions) ConfineUser(userID, organizationID string) {
	r.confined[userID] = organizationID
}

// attachTestE2EControl runs the startup decision with no database or session
// store behind it (neither is touched by the calls below) and reports the
// status each route then answers, and whom the surface confined.
func attachTestE2EControl(t *testing.T, s e2eStartup) (map[string]int, map[string]string, error) {
	t.Helper()
	siteURL, err := url.Parse(s.site)
	require.NoError(t, err)
	serverURL, err := url.Parse(s.server)
	require.NoError(t, err)
	redisClient := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	mux := goahttp.NewMuxer()
	confined := map[string]string{}
	err = attachE2EControl(t.Context(), testenv.NewLogger(t), mux, e2eControlDeps{
		environment: s.environment,
		token:       s.token,
		databaseName: func(context.Context) (string, error) {
			if s.database == "" {
				return "", errors.New("the database did not answer")
			}
			return s.database, nil
		},
		developerEmail: func(context.Context) string { return "developer@example.test" },
		siteURL:        siteURL,
		serverURL:      serverURL,
		issuerURL:      "",
		db:             nil,
		clickhouse:     nil,
		blob:           nil,
		encryption:     nil,
		redis:          redisClient,
		users:          nil,
		sessions:       recordingSessions{confined: confined},
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
	return statuses, confined, err
}

var (
	e2eControlNotMounted = map[string]int{
		"/rpc/e2e.tenant":     http.StatusNotFound,
		"/rpc/e2e.signIn":     http.StatusNotFound,
		"/rpc/e2e.reseed":     http.StatusNotFound,
		"/rpc/e2e.signInLink": http.StatusNotFound,
	}
	e2eControlMounted = map[string]int{
		"/rpc/e2e.tenant":     http.StatusOK,
		"/rpc/e2e.signIn":     http.StatusForbidden, // no code
		"/rpc/e2e.reseed":     http.StatusUnauthorized,
		"/rpc/e2e.signInLink": http.StatusUnauthorized,
	}
)

func TestE2EControlIsOffWithoutAToken(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"local", "dev", "prod"} {
		s := previewStartup()
		s.environment, s.token = environment, ""
		statuses, confined, err := attachTestE2EControl(t, s)
		require.NoError(t, err, environment)
		require.Equal(t, e2eControlNotMounted, statuses, environment)
		require.Empty(t, confined, environment)
	}
}

// Every environment but local and dev refuses, even one that is otherwise
// configured exactly as a preview.
func TestE2EControlTokenStopsStartupInOtherEnvironments(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"prod", "production", "preview", "staging", "Local", "Dev", " dev", ""} {
		s := previewStartup()
		s.environment = environment
		statuses, _, err := attachTestE2EControl(t, s)
		require.ErrorContains(t, err, "exists only on a local stack or a pull request preview", environment)
		require.Equal(t, e2eControlNotMounted, statuses, environment)
	}
}

// In the dev environment the surface exists only where the server is connected
// to a preview's own database and served at that preview's host.
func TestE2EControlTokenStopsStartupInDevUnlessItIsAPreview(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*e2eStartup){
		"the environment's own database": func(s *e2eStartup) { s.database = "gram" },
		"the environment's own deployment": func(s *e2eStartup) {
			s.database, s.site, s.server = "gram", "https://dev.example.test", "https://dev.example.test"
		},
		"a database that does not answer":   func(s *e2eStartup) { s.database = "" },
		"a database named like a preview's": func(s *e2eStartup) { s.database = "gram_preview_pr_123_copy" },
		"a prefix before the name":          func(s *e2eStartup) { s.database = "x_gram_preview_pr_123" },
		"no pull request number":            func(s *e2eStartup) { s.database = "gram_preview_pr_" },
		"pull request zero":                 func(s *e2eStartup) { s.database = "gram_preview_pr_0" },
		"a number with a leading zero":      func(s *e2eStartup) { s.database = "gram_preview_pr_0123" },
		"a number too large to name":        func(s *e2eStartup) { s.database = "gram_preview_pr_268435456" },
		"another preview's database":        func(s *e2eStartup) { s.database = "gram_preview_pr_124" },
		"a longer number in the host": func(s *e2eStartup) {
			s.site, s.server = "https://pr-1234.dev.example.test", "https://pr-1234.dev.example.test"
		},
		"the environment's dashboard host": func(s *e2eStartup) { s.site = "https://dev.example.test" },
		"the environment's server host":    func(s *e2eStartup) { s.server = "https://dev.example.test" },
		"two different preview hosts":      func(s *e2eStartup) { s.server = "https://pr-123.other.example.test" },
		"the number later in the host": func(s *e2eStartup) {
			s.site, s.server = "https://dev.pr-123.example.test", "https://dev.pr-123.example.test"
		},
		"a loopback host": func(s *e2eStartup) { s.site, s.server = "http://localhost:5173", "http://localhost:8080" },
	}
	for name, mutate := range cases {
		s := previewStartup()
		mutate(&s)
		statuses, confined, err := attachTestE2EControl(t, s)
		require.Error(t, err, name)
		require.Equal(t, e2eControlNotMounted, statuses, name)
		require.Empty(t, confined, name)
	}
}

func TestE2EControlIsMountedOnAPreviewBehindTheToken(t *testing.T) {
	t.Parallel()
	statuses, confined, err := attachTestE2EControl(t, previewStartup())
	require.NoError(t, err)
	require.Equal(t, e2eControlMounted, statuses)
	// The synthetic test user of pull request 123, and nobody else, is pinned
	// to pull request 123's test organization.
	require.Equal(t, map[string]string{"user_e2e123_e2e-runner": "org_e2e_pr_123"}, confined)
}

func TestE2EControlIsMountedLocallyBehindTheToken(t *testing.T) {
	t.Parallel()
	statuses, confined, err := attachTestE2EControl(t, e2eStartup{
		environment: "local",
		token:       testE2EControlToken,
		database:    "gram",
		site:        "https://localhost:5173",
		server:      "https://localhost:8080",
	})
	require.NoError(t, err)
	require.Equal(t, e2eControlMounted, statuses)
	require.Empty(t, confined, "the developer keeps the run of their own stack")
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
