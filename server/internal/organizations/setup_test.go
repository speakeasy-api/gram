package organizations_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/mock"
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
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
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
}

func newTestOrganizationsService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// workos_id is set by InitAuthContext via UpsertOrganizationMetadata (from the mock IDP's workos_id).

	err = userrepo.New(conn).SetUserWorkosID(ctx, userrepo.SetUserWorkosIDParams{
		ID:       authCtx.UserID,
		WorkosID: conv.ToPGText(testAuthUserWorkOSID),
	})
	require.NoError(t, err)

	orgs := newMockOrganizationProvider(t)

	authzEngine := authz.NewEngine(logger, conn, authztest.RBACAlwaysEnabled, thirdpartyworkos.NewStubClient(), cache.NoopCache)
	svc := organizations.NewService(logger, tracerProvider, conn, sessionManager, orgs, stubOrgFeatures{}, authzEngine)

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
		orgs:    orgs,
	}
}

// newTestOrganizationsServiceRBAC creates a service instance where RBAC feature is enabled,
// so requireOrgTeamManagementAccess takes the access.Require path instead of the WorkOS fallback.
func newTestOrganizationsServiceRBAC(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// workos_id is set by InitAuthContext via UpsertOrganizationMetadata (from the mock IDP's workos_id).

	err = userrepo.New(conn).SetUserWorkosID(ctx, userrepo.SetUserWorkosIDParams{
		ID:       authCtx.UserID,
		WorkosID: conv.ToPGText(testAuthUserWorkOSID),
	})
	require.NoError(t, err)

	orgs := newMockOrganizationProvider(t)

	authzEngine := authz.NewEngine(logger, conn, authztest.RBACAlwaysEnabled, thirdpartyworkos.NewStubClient(), cache.NoopCache)
	svc := organizations.NewService(logger, tracerProvider, conn, sessionManager, orgs, stubOrgFeaturesEnabled{}, authzEngine)

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
		orgs:    orgs,
	}
}

// expectWorkOSOrgAdminRole stubs a successful WorkOS admin membership check for the session user.
func expectWorkOSOrgAdminRole(t *testing.T, orgs *MockOrganizationProvider) {
	t.Helper()
	orgs.On("GetOrgMembership", mock.Anything, testAuthUserWorkOSID, mockidp.MockOrgID).Return(&thirdpartyworkos.Member{RoleSlug: "admin"}, nil).Once()
}

// expectWorkOSOrgNonAdminRole stubs WorkOS membership with a non-admin role.
func expectWorkOSOrgNonAdminRole(t *testing.T, orgs *MockOrganizationProvider) {
	t.Helper()
	orgs.On("GetOrgMembership", mock.Anything, testAuthUserWorkOSID, mockidp.MockOrgID).Return(&thirdpartyworkos.Member{RoleSlug: "member"}, nil).Once()
}
