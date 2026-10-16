package analytics

import (
	"context"
	"log"
	"os"
	"sync"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
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

func newTestClickhouse(t *testing.T) clickhouse.Conn {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	return conn
}

type testInstance struct {
	service        *Service
	features       *testFeatures
	ch             clickhouse.Conn
	db             *pgxpool.Pool
	organizationID string
	projectID      string
}

// newTestService builds the service against a cloned database, a real
// ClickHouse, and an auth context with a project selected and admin grants.
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
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-test"), billingClient)
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context should be set")
	require.NotNil(t, authCtx.ProjectID, "auth context should select a project")

	chConn := newTestClickhouse(t)
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	features := &testFeatures{enabled: map[productfeatures.Feature]bool{}}

	return ctx, &testInstance{
		features:       features,
		service:        NewService(logger, tracerProvider, conn, chConn, sessionManager, authzEngine, features),
		ch:             chConn,
		db:             conn,
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      authCtx.ProjectID.String(),
	}
}

// testFeatures is a per-test product feature set. The real client caches in a
// Redis every test shares, under one fixed organization, so a parallel test
// toggling a feature would leak into the others.
type testFeatures struct {
	mu      sync.Mutex
	enabled map[productfeatures.Feature]bool
}

func (f *testFeatures) IsFeatureEnabled(_ context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enabled[feature], nil
}

func (f *testFeatures) set(feature productfeatures.Feature, enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enabled[feature] = enabled
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code, "error: %v", err)
}
