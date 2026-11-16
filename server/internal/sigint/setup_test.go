package sigint_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
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
	service  *sigint.Service
	conn     *pgxpool.Pool
	features *productfeatures.Client
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
	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// The database is cloned per test but Redis is shared across the package, so
	// tests that kept the default organization id would also share the
	// entitlement cache — one test disabling the feature would flake its parallel
	// neighbours. Sigint queries require the project to belong to the active
	// organization, so the project moves with it.
	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Sigint Test Org",
		Slug:        "sigint-test-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	projectSlug := "sigint-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: projectSlug, Slug: projectSlug, OrganizationID: orgID,
	})
	require.NoError(t, err)
	authCtx.ActiveOrganizationID = orgID
	authCtx.ProjectID = &project.ID
	authCtx.ProjectSlug = &project.Slug
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	require.NoError(t, features.SetFeatureEnabled(ctx, orgID, productfeatures.FeatureSignalsIntelligence, true))
	return ctx, &testInstance{
		service:  sigint.NewService(logger, tracerProvider, conn, sessions, engine, audit.NewLogger(), features),
		conn:     conn,
		features: features,
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
