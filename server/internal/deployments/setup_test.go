package deployments_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/access/accesstest"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	packages "github.com/speakeasy-api/gram/server/internal/packages"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, Temporal: true})
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
	service        *deployments.Service
	feature        *feature.InMemory
	assets         *assets.Service
	packages       *packages.Service
	conn           *pgxpool.Pool
	temporalEnv    *temporal.Environment
	sessionManager *sessions.Manager
}

func newTestDeploymentService(t *testing.T, assetStorage assets.BlobStore) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	funcs := testenv.NewFunctionsTestOrchestrator(t, assetStorage)
	mcpRegistryClient := testenv.NewMCPRegistryClient(t, logger, tracerProvider)

	f := &feature.InMemory{}

	temporalEnv, _ := infra.NewTemporalEnv(t)
	worker := background.NewTemporalWorker(temporalEnv, logger, tracerProvider, meterProvider, background.ForDeploymentProcessing(conn, f, assetStorage, enc, funcs, mcpRegistryClient))
	t.Cleanup(func() {
		worker.Stop()
	})
	require.NoError(t, worker.Start(), "start temporal worker")

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	svc := deployments.NewService(logger, tracerProvider, conn, temporalEnv, sessionManager, assetStorage, posthog, testenv.DefaultSiteURL(t), mcpRegistryClient, access.NewManager(logger, conn, accesstest.AlwaysEnabledFeatureChecker{}))
	assetsSvc := assets.NewService(logger, tracerProvider, conn, sessionManager, chatSessionsManager, assetStorage, "test-jwt-secret", access.NewManager(logger, conn, accesstest.AlwaysEnabledFeatureChecker{}))
	packagesSvc := packages.NewService(logger, tracerProvider, conn, sessionManager, access.NewManager(logger, conn, accesstest.AlwaysEnabledFeatureChecker{}))

	return ctx, &testInstance{
		service:        svc,
		feature:        f,
		assets:         assetsSvc,
		packages:       packagesSvc,
		conn:           conn,
		temporalEnv:    temporalEnv,
		sessionManager: sessionManager,
	}
}

// withExactAccessGrants sets AccountType to "enterprise" and seeds the given grants
// into the database, returning a context with those grants loaded.
func withExactAccessGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, grants ...access.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "deployments-rbac-grants-"+uuid.NewString())
	for _, grant := range grants {
		_, err := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   principal,
			Scope:          string(grant.Scope),
			Resource:       grant.Resource,
		})
		require.NoError(t, err)
	}

	loadedGrants, err := access.LoadGrants(ctx, conn, authCtx.ActiveOrganizationID, []urn.Principal{principal})
	require.NoError(t, err)

	return access.GrantsToContext(ctx, loadedGrants)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
