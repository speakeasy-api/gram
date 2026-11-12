package dataexports_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/otelsink"
	"github.com/speakeasy-api/gram/server/internal/oops"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
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
	service *dataexports.Service
	conn    *pgxpool.Pool
	enc     *encryption.Client
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
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
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	enc := testenv.NewEncryptionClient(t)
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	// Unsafe so tests can reach httptest servers on loopback.
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	meterProvider := testenv.NewMeterProvider(t)
	sender, err := otelsvc.NewRouteTester(policy, append(otelsvc.NewRouteTestSources(logger, meterProvider), otelsink.NewTestSource(otelsvc.NewConnectionLogTestRequests(logger, meterProvider), "gram-server"))...)
	require.NoError(t, err)
	store := ratelimit.NewRedisStore(redisClient)
	// Every test shares one organization ID and one Redis, so each gets its own organization bucket.
	routeTests := dataexports.NewRouteTestCore(logger, conn, audit.NewLogger(), enc, sender, dataexports.RouteTestLimits{
		Route:        ratelimit.New(store, dataexports.RouteTestLimitName, ratelimit.PerMinute(dataexports.RouteTestsPerMinute)),
		Organization: ratelimit.New(store, dataexports.RouteTestOrganizationLimitName+":"+uuid.NewString(), ratelimit.PerMinute(dataexports.RouteTestsPerOrganizationPerMinute)),
	})

	service := dataexports.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, audit.NewLogger(), enc, routeTests)
	return ctx, &testInstance{service: service, conn: conn, enc: enc}
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func createDestination(t *testing.T, ctx context.Context, ti *testInstance, endpointURL, sensitiveData string) *gen.Destination {
	t.Helper()
	destination, err := ti.service.CreateDestination(ctx, &gen.CreateDestinationPayload{SessionToken: nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             "Test destination",
		DestinationType:  "otel",
		SensitiveData:    sensitiveData,
		Otel: &gen.CreateOtelDestinationInput{
			EndpointURL: endpointURL,
			Headers:     []*gen.CreateOtelDestinationHeaderInput{},
		}})
	require.NoError(t, err)
	return destination
}
