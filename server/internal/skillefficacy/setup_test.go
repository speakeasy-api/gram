package skillefficacy_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	pfrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/skillefficacy"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
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

type testInstance struct {
	service  *skillefficacy.Service
	conn     *pgxpool.Pool
	features *productfeatures.Client
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	return newTestServiceWithInsights(t, telemetryrepo.New(chConn))
}

func newTestServiceWithInsights(t *testing.T, insights skillefficacy.InsightsReader) (context.Context, *testInstance) {
	t.Helper()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "skill_efficacy_settings_api")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 11)
	require.NoError(t, err)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)
	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	return ctx, &testInstance{
		service:  skillefficacy.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, features, audit.NewLogger(), insights),
		conn:     conn,
		features: features,
	}
}

func setSkillsFeature(t *testing.T, ctx context.Context, ti *testInstance, enabled bool) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	if enabled {
		_, err := pfrepo.New(ti.conn).EnableFeature(ctx, pfrepo.EnableFeatureParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    string(productfeatures.FeatureSkills),
		})
		require.NoError(t, err)
	}
	ti.features.UpdateFeatureCache(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSkills, enabled)
}

func withGrant(t *testing.T, ctx context.Context, scope authz.Scope) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return authztest.WithExactGrants(t, ctx, authz.NewGrant(scope, authCtx.ActiveOrganizationID))
}

func withProjectGrants(t *testing.T, ctx context.Context, scopes ...authz.Scope) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	grants := make([]authz.Grant, 0, len(scopes))
	for _, scope := range scopes {
		grants = append(grants, authz.NewGrant(scope, authCtx.ProjectID.String()))
	}
	return authztest.WithExactGrants(t, ctx, grants...)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
