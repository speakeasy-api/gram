package sigint_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sigint"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: false})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type testInstance struct {
	service *sigint.Service
	conn    *pgxpool.Pool
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "sigint")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessions := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("sigint"), billingClient)
	ctx = authztest.InitAuthContext(t, ctx, conn, sessions)
	engine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	return ctx, &testInstance{
		service: sigint.NewService(logger, tracerProvider, conn, sessions, engine, audit.NewLogger()),
		conn:    conn,
	}
}

func createSignal(t *testing.T, ctx context.Context, ti *testInstance, name string) *types.SigintSignal {
	t.Helper()
	signal, err := ti.service.CreateSignal(ctx, &gen.CreateSignalPayload{
		Name: name, Description: nil, ClassifierCriteria: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	return signal
}

func createSensor(t *testing.T, ctx context.Context, ti *testInstance, name string, mode types.SigintSensorMode, ids ...string) *types.SigintSensor {
	t.Helper()
	sensor, err := ti.service.CreateSensor(ctx, &gen.CreateSensorPayload{
		Name: name, Description: nil, Instructions: nil, Mode: mode, SignalIds: ids,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	return sensor
}

func getSensor(t *testing.T, ctx context.Context, ti *testInstance, id string) *types.SigintSensor {
	t.Helper()
	sensor, err := ti.service.GetSensor(ctx, &gen.GetSensorPayload{
		ID: id, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	return sensor
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}
