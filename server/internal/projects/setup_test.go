package projects_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/projects"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	service        *projects.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
	assetStorage   assets.BlobStore
}

type stubFeatureChecker struct {
	enabled bool
}

func (s stubFeatureChecker) IsFeatureEnabled(_ context.Context, _ string, _ productfeatures.Feature) (bool, error) {
	return s.enabled, nil
}

func newTestProjectsService(t *testing.T, enableRBAC bool) (context.Context, *testInstance) {
	t.Helper()

	ctx := context.Background()

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
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	ctx = withAccessGrants(t, ctx, conn,
		access.Grant{Scope: access.ScopeBuildRead, Resource: authCtx.ProjectID.String()},
		access.Grant{Scope: access.ScopeBuildWrite, Resource: authCtx.ProjectID.String()},
		access.Grant{Scope: access.ScopeOrgAdmin, Resource: authCtx.ActiveOrganizationID},
	)

	// Create test asset storage for testing
	assetStorage := assetstest.NewTestBlobStore(t)

	svc := projects.NewService(logger, tracerProvider, conn, sessionManager, access.NewManager(logger, conn, stubFeatureChecker{enabled: enableRBAC}))

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		assetStorage:   assetStorage,
	}
}

func withAccessGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, grants ...access.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	for _, grant := range grants {
		seedGrant(t, ctx, conn, authCtx.ActiveOrganizationID, userPrincipal, grant.Scope, grant.Resource)
	}

	loadedGrants, err := access.LoadGrants(ctx, conn, authCtx.ActiveOrganizationID, []urn.Principal{userPrincipal})
	require.NoError(t, err)

	return access.GrantsToContext(ctx, loadedGrants)
}

func withExactAccessGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, grants ...access.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "test-exact-grants")
	for _, grant := range grants {
		seedGrant(t, ctx, conn, authCtx.ActiveOrganizationID, principal, grant.Scope, grant.Resource)
	}

	loadedGrants, err := access.LoadGrants(ctx, conn, authCtx.ActiveOrganizationID, []urn.Principal{principal})
	require.NoError(t, err)

	return access.GrantsToContext(ctx, loadedGrants)
}

func seedGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal, scope access.Scope, resource string) {
	t.Helper()

	_, err := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Resource:       resource,
	})
	require.NoError(t, err)
}
