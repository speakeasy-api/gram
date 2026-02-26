package oauth_test

import (
	"context"
	"log"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background())
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	service        *oauth.ExternalOAuthService
	conn           *pgxpool.Pool
	enc            *encryption.Client
	sessionManager *sessions.Manager
	serverURL      *url.URL
}

func newTestExternalOAuthService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "oauthtest")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager, err := sessions.NewUnsafeManager(logger, conn, redisClient, cache.Suffix("gram-test"), "", billingClient)
	require.NoError(t, err)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	serverURL, err := url.Parse("http://localhost:8080")
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	authAuth := auth.New(logger, conn, sessionManager)
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)

	svc := oauth.NewExternalOAuthService(
		logger,
		conn,
		cacheAdapter,
		authAuth,
		enc,
		oauth.ExternalOAuthServiceConfig{
			ServerURL:            serverURL,
			AllowedRedirectHosts: []string{"localhost"},
		},
	)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		enc:            enc,
		sessionManager: sessionManager,
		serverURL:      serverURL,
	}
}
