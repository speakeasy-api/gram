package instances

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpmetadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/require"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: false, Temporal: false})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

func newTestInstanceService(t *testing.T) (context.Context, *Service, *pgxpool.Pool) {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracer := testenv.NewTracerProvider(t)
	meter := testenv.NewMeterProvider(t)
	conn, err := infra.CloneTestDatabase(t, "instancetest")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	billingClient := billing.NewStubClient(logger, tracer)
	sessions := testenv.NewTestManager(t, logger, tracer, conn, redisClient, cache.Suffix("instances-test"), billingClient)
	ctx := authztest.InitAuthContext(t, t.Context(), conn, sessions)
	enc := testenv.NewEncryptionClient(t)
	env := environments.NewEnvironmentEntries(logger, conn, enc, mcpmetadatarepo.New(conn))
	policy, err := guardian.NewUnsafePolicy(tracer, nil)
	require.NoError(t, err)
	engine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	svc := NewService(logger, tracer, meter, conn, sessions, nil, env, enc, testenv.NewMemoryCache(), policy, nil, nil, billingClient, telemetry.NewStub(logger), nil, testenv.DefaultSiteURL(t), engine)
	return ctx, svc, conn
}
