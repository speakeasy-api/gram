package productfeatures_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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
	service        *productfeatures.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
}

func newTestProductFeaturesService(t *testing.T) (context.Context, *testInstance) {
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

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	// The mock IDP returns the same organization for every test, so parallel
	// subtests would otherwise share the cache key feature:<orgID>:<feature>
	// in the shared redis db. One test enabling and another disabling the
	// same feature races on that key and produces flaky failures (e.g.
	// "returns true for enabled feature" reading a `false` written by
	// "returns false after feature is disabled"). Override the org ID with
	// a fresh UUID per test so cache keys are unique. organization_features
	// has no FK on organization_id, and ShouldEnforce skips RBAC for the
	// non-enterprise account type used in tests. The synthetic org does need
	// an organization_metadata row: audited toggles append to the outbox,
	// whose organization_id carries a foreign key.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context not found")
	authCtx.ActiveOrganizationID = uuid.NewString()
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	err = testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 authCtx.ActiveOrganizationID,
		Name:               "Product Features Test Org",
		Slug:               authCtx.ActiveOrganizationID,
		GramAccountType:    "free",
		WorkosID:           conv.PtrToPGText(nil),
		Whitelisted:        false,
		FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now().UTC()),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(time.Now().UTC().Add(14 * 24 * time.Hour)),
		DisabledAt:         conv.PtrToPGTimestamptz(nil),
	})
	require.NoError(t, err)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	svc := productfeatures.NewService(logger, tracerProvider, conn, sessionManager, redisClient, authzEngine, audit.NewLogger())

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}
