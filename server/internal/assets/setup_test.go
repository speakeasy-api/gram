package assets_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
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
	service             *assets.Service
	conn                *pgxpool.Pool
	storage             assets.BlobStore
	repo                *repo.Queries
	sessionManager      *sessions.Manager
	chatSessionsManager *chatsessions.Manager
}

func newTestAssetsService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	tracerProvider := testenv.NewTracerProvider(t)
	// UnsafePolicy with an empty blocklist lets httptest loopback succeed.
	// SSRF tests that need the production CIDR set use
	// [newTestAssetsServiceWithPolicy] with [guardian.NewDefaultPolicy].
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	return newTestAssetsServiceWithPolicy(t, guardianPolicy)
}

func newTestAssetsServiceWithPolicy(t *testing.T, guardianPolicy *guardian.Policy) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")

	storage := assetstest.NewTestBlobStore(t)

	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	auditLogger := audit.NewLogger()

	svc := assets.NewService(logger,
		tracerProvider,
		guardianPolicy,
		conn,
		sessionManager,
		chatSessionsManager,
		storage,
		"test-jwt-secret",
		authz.NewEngine(logger,
			conn,

			authztest.ChallengeLoggingAlwaysDisabled,
			workos.NewStubClient()),

		auditLogger,
	)
	repository := repo.New(conn)

	return ctx, &testInstance{
		service:             svc,
		conn:                conn,
		storage:             storage,
		repo:                repository,
		sessionManager:      sessionManager,
		chatSessionsManager: chatSessionsManager,
	}
}

// withAdmin returns ctx with the auth context's IsAdmin flag flipped to true.
// Tests for admin-only endpoints opt in explicitly so non-admin paths exercise
// the realistic default produced by authztest.InitAuthContext. The auth
// context is copied so the original ctx stays non-admin.
func withAdmin(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	adminAuthCtx := *authCtx
	adminAuthCtx.IsAdmin = true
	return contextvalues.SetAuthContext(ctx, &adminAuthCtx)
}
