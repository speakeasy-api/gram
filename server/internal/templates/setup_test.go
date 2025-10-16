package templates_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/templates"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	service        *templates.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
}

func newTestTemplateService(t *testing.T) (context.Context, *testInstance) {
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

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	toolsetsSvc := &toolsetsServiceStub{
		InvalidateCacheByToolFunc: func(ctx context.Context, toolURN urn.Tool, projectID uuid.UUID) error {
			println("Invalidating cache for tool", toolURN.String(), "and project", projectID.String())
			return nil
		},
	}
	svc := templates.NewService(logger, conn, sessionManager, toolsetsSvc)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}

type toolsetsServiceStub struct {
	InvalidateCacheByToolFunc func(ctx context.Context, toolURN urn.Tool, projectID uuid.UUID) error
}

func (s *toolsetsServiceStub) InvalidateCacheByTool(ctx context.Context, toolURN urn.Tool, projectID uuid.UUID) error {
	return s.InvalidateCacheByToolFunc(ctx, toolURN, projectID)
}
