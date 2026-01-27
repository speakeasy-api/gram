package telemetry_test

import (
	"context"
	"log"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"

	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

var (
	infra             *testenv.Environment
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
	logger         *slog.Logger
	conn           *pgxpool.Pool
	chClient       *repo.Queries
	sessionManager *sessions.Manager
	orgID          string
	disabledLogsOrgID string
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

	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	chClient := repo.New(chConn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context should be set")

	disabledLogsOrgID := uuid.New().String()
	logsEnabled := func(_ context.Context, orgID string) (bool, error) {
		if orgID == disabledLogsOrgID {
			return false, nil
		}

		return true, nil
	}

	posthogClient := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	logWriter := telemetry.NewLogWriter(logger, chConn, logsEnabled, telemetry.LogWriterOptions{})

	svc := telemetry.NewService(logger, conn, chConn, sessionManager, chatSessionsManager, logsEnabled, logWriter, posthogClient)

	return ctx, &testInstance{
		service:        svc,
		logger:         logger,
		conn:           conn,
		chClient:       chClient,
		sessionManager: sessionManager,
		orgID:          authCtx.ActiveOrganizationID,
		disabledLogsOrgID: disabledLogsOrgID,
	}
}

func switchOrganizationInCtx(t *testing.T, ctx context.Context, newOrgID string) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context should be set")

	// Use a different org ID which won't have logs enabled
	// This org won't have the logs feature enabled
	authCtx.ActiveOrganizationID = newOrgID
	authCtx.OrganizationSlug = "organization-456"

	return contextvalues.SetAuthContext(ctx, authCtx)
}
