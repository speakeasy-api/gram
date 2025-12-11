package telemetry_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
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

	if err = cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

type testInstance struct {
	service        *telemetry.Service
	conn           *pgxpool.Pool
	chClient       *repo.Queries
	sessionManager *sessions.Manager
}

func newTestLogsService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, testenv.NewTracerProvider(t))

	sessionManager, err := sessions.NewUnsafeManager(logger, conn, redisClient, cache.Suffix("gram-test"), "", billingClient)
	require.NoError(t, err)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	tracerProvider := testenv.NewTracerProvider(t)

	chClient := repo.New(logger, tracerProvider, chConn, func(context.Context, string) (bool, error) {
		return true, nil
	})

	svc := telemetry.NewService(logger, conn, sessionManager, chClient)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		chClient:       chClient,
		sessionManager: sessionManager,
	}
}

func setProjectID(t *testing.T, ctx context.Context, projectID string) context.Context {
	t.Helper()

	authContext, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	id := uuid.MustParse(projectID)
	authContext.ProjectID = &id

	return contextvalues.SetAuthContext(ctx, authContext)
}
