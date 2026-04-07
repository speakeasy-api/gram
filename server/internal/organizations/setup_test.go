package organizations_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// testAuthUserWorkOSID is the WorkOS user id for the session user in tests (admin in org_workos_test).
const testAuthUserWorkOSID = "user_01WORKOS_INVITER"

type stubOrgFeatures struct{}

func (stubOrgFeatures) IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error) {
	return false, nil
}

// expectWorkOSOrgAdminRole stubs a successful WorkOS admin membership check for the session user.
func expectWorkOSOrgAdminRole(t *testing.T, orgs *MockOrganizationProvider) {
	t.Helper()
	orgs.On("GetOrgMembership", mock.Anything, testAuthUserWorkOSID, "org_workos_test").Return(&thirdpartyworkos.Member{RoleSlug: "admin"}, nil).Once()
}

// expectWorkOSOrgNonAdminRole stubs WorkOS membership with a non-admin role (team management should be denied).
func expectWorkOSOrgNonAdminRole(t *testing.T, orgs *MockOrganizationProvider) {
	t.Helper()
	orgs.On("GetOrgMembership", mock.Anything, testAuthUserWorkOSID, "org_workos_test").Return(&thirdpartyworkos.Member{RoleSlug: "member"}, nil).Once()
}

// requireOrgManagementForbidden asserts the error from a team-management endpoint when the caller is not an org admin.
func requireOrgManagementForbidden(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

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

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	_, err = orgrepo.New(conn).SetOrgWorkosID(ctx, orgrepo.SetOrgWorkosIDParams{
		WorkosID:       conv.PtrToPGText(conv.PtrEmpty("org_workos_test")),
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	err = userrepo.New(conn).SetUserWorkosID(ctx, userrepo.SetUserWorkosIDParams{
		ID:       authCtx.UserID,
		WorkosID: conv.ToPGText(testAuthUserWorkOSID),
	})
	require.NoError(t, err)

	orgs := newMockOrganizationProvider(t)

	svc := organizations.NewService(logger, tracerProvider, conn, sessionManager, orgs, stubOrgFeatures{})

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
		orgs:    orgs,
	}
}
