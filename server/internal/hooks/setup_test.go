package hooks

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var (
	infra *testenv.Environment
)

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
	service          *Service
	conn             *pgxpool.Pool
	redisClient      *redis.Client
	telemetryService *telemetry.Service
	sessionManager   *sessions.Manager
	tracerProvider   trace.TracerProvider
}

func newTestHooksService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager, err := sessions.NewUnsafeManager(logger, conn, redisClient, cache.Suffix("gram-local"), "", billingClient)
	require.NoError(t, err)

	chatSessionManager, err := chatsessions.NewManager(logger, conn, redisClient, cache.Suffix("gram-local"), "")
	require.NoError(t, err)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	// Set up ClickHouse client
	clickhouseClient, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	// Create a stub feature checker that always returns true
	alwaysEnabled := func(ctx context.Context, organizationID string) (bool, error) {
		return true, nil
	}

	// Create stub posthog client
	stubPosthog := &telemetry.StubPosthogClient{}

	telemetryService := telemetry.NewService(
		logger,
		conn,
		clickhouseClient,
		sessionManager,
		chatSessionManager,
		alwaysEnabled, // logsEnabled
		alwaysEnabled, // toolIOLogsEnabled
		stubPosthog,
	)

	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)

	svc := NewService(logger, conn, tracerProvider, telemetryService, sessionManager, cacheAdapter)

	return ctx, &testInstance{
		service:          svc,
		conn:             conn,
		redisClient:      redisClient,
		telemetryService: telemetryService,
		sessionManager:   sessionManager,
		tracerProvider:   tracerProvider,
	}
}
