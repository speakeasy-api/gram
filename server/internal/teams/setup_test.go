package teams_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/teams"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
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
	service *teams.Service
	conn    *pgxpool.Pool
}

func newTestTeamsService(t *testing.T, wos *workos.WorkOS) (context.Context, *testInstance) {
	t.Helper()

	ctx := context.Background()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	if err != nil {
		t.Fatalf("failed to clone test database: %v", err)
	}

	redisClient, err := infra.NewRedisClient(t, 0)
	if err != nil {
		t.Fatalf("failed to create redis client: %v", err)
	}

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManagerWithWorkOS(t, logger, conn, redisClient, "gram-local", billingClient, wos)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	svc := teams.NewService(logger, conn, sessionManager)

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
	}
}
