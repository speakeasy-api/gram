package triggers_test

import (
	"context"
	"log"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/triggers"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type stubEnvLoader struct {
	values map[string]string
}

func (s stubEnvLoader) Load(_ context.Context, _ uuid.UUID, _ toolconfig.SlugOrID) (map[string]string, error) {
	return s.values, nil
}

type testInstance struct {
	service       *triggers.Service
	conn          *pgxpool.Pool
	environmentID uuid.UUID
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
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
	require.NotNil(t, authCtx.ProjectID)

	envRow, err := environmentsrepo.New(conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "test",
		Slug:           "test-" + uuid.NewString()[:8],
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	loader := stubEnvLoader{values: map[string]string{"SLACK_SIGNING_SECRET": "test-secret"}}

	serverURL, err := url.Parse("https://gram.test.local")
	require.NoError(t, err)

	app := bgtriggers.NewApp(
		logger,
		conn,
		nil,
		loader,
		bgtriggers.NewTriggerDeliveryLogger(func(_ context.Context, _ bgtriggers.TriggerDeliveryLog) {}),
		serverURL,
	)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	auditLogger := audit.NewLogger()

	svc := triggers.NewService(
		logger,
		tracerProvider,
		conn,
		sessionManager,
		authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache),
		app,
		auditLogger,
	)

	return ctx, &testInstance{
		service:       svc,
		conn:          conn,
		environmentID: envRow.ID,
	}
}
