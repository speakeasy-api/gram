package organizations_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	svix "github.com/svix/svix-webhooks/go"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/svix/svixtest"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
)

// stubOrgFeatures returns false for all features — tests use the WorkOS fallback path.
type stubOrgFeatures struct{}

func (stubOrgFeatures) IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error) {
	return false, nil
}

// stubOrgFeaturesEnabled returns true for all features — tests use the RBAC path.
type stubOrgFeaturesEnabled struct{}

func (stubOrgFeaturesEnabled) IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error) {
	return true, nil
}

// testAuthUserWorkOSID is the WorkOS user id for the session user in tests.
const testAuthUserWorkOSID = "user_01WORKOS_INVITER"

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
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

type testInstance struct {
	service *organizations.Service
	conn    *pgxpool.Pool
	orgs    *MockOrganizationProvider
	svixSrv *svixtest.MockServer
}

func newTestOrganizationsService(t *testing.T) (context.Context, *testInstance) {
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
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// UpsertUserFromIDP (called inside InitAuthContext) now backfills workos_id
	// with the mock IDP's user ID. Override it to the test-specific WorkOS user
	// ID so that mock expectations on GetOrgMembership match.
	err = userrepo.New(conn).OverwriteUserWorkosID(ctx, userrepo.OverwriteUserWorkosIDParams{
		ID:       authCtx.UserID,
		WorkosID: conv.ToPGText(testAuthUserWorkOSID),
	})
	require.NoError(t, err)

	orgs := newMockOrganizationProvider(t)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, thirdpartyworkos.NewStubClient(), cache.NoopCache)

	auditLogger := audit.NewLogger()

	svixSrv := svixtest.NewMockServer(logger)
	t.Cleanup(svixSrv.Close)
	svixClient, err := svix.New("test-token", &svix.SvixOptions{ServerUrl: svixSrv.URL()})
	require.NoError(t, err)

	svc := organizations.NewService(logger, tracerProvider, conn, sessionManager, orgs, stubOrgFeatures{}, authzEngine, nil, "http://localhost:5173", "http://localhost:35291", auditLogger, svixClient)

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
		orgs:    orgs,
		svixSrv: svixSrv,
	}
}

// newTestOrganizationsServiceRBAC creates a service instance where RBAC feature is enabled,
// so requireOrgTeamManagementAccess takes the access.Require path instead of the WorkOS fallback.
func newTestOrganizationsServiceRBAC(t *testing.T) (context.Context, *testInstance) {
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
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// UpsertUserFromIDP (called inside InitAuthContext) now backfills workos_id
	// with the mock IDP's user ID. Override it to the test-specific WorkOS user
	// ID so that mock expectations on GetOrgMembership match.
	err = userrepo.New(conn).OverwriteUserWorkosID(ctx, userrepo.OverwriteUserWorkosIDParams{
		ID:       authCtx.UserID,
		WorkosID: conv.ToPGText(testAuthUserWorkOSID),
	})
	require.NoError(t, err)

	orgs := newMockOrganizationProvider(t)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, thirdpartyworkos.NewStubClient(), cache.NoopCache)

	auditLogger := audit.NewLogger()

	svixSrv := svixtest.NewMockServer(logger)
	t.Cleanup(svixSrv.Close)
	svixClient, err := svix.New("test-token", &svix.SvixOptions{ServerUrl: svixSrv.URL()})
	require.NoError(t, err)

	svc := organizations.NewService(logger, tracerProvider, conn, sessionManager, orgs, stubOrgFeaturesEnabled{}, authzEngine, nil, "http://localhost:5173", "http://localhost:35291", auditLogger, svixClient)

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
		orgs:    orgs,
		svixSrv: svixSrv,
	}
}
