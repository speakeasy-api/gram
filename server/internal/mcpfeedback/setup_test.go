package mcpfeedback_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chatredact"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	unproxiedmcprepo "github.com/speakeasy-api/gram/server/internal/unproxiedmcp/repo"
)

// testServiceName is the service.name the test recorder stamps on exported records.
const testServiceName = "gram-server"

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
	service *mcpfeedback.Service
	conn    *pgxpool.Pool
	redis   *redis.Client
}

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
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	service := mcpfeedback.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, audit.NewLogger())
	return ctx, &testInstance{service: service, conn: conn, redis: redisClient}
}

// newTestRecorder returns a recorder that masks with the real redactor,
// enforces the given limits, and reads settings on every call.
func newTestRecorder(t *testing.T, ti *testInstance, limits mcpfeedback.Limits) *mcpfeedback.Recorder {
	t.Helper()

	return mcpfeedback.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, chatredact.New(), limits, testServiceName, exportOn(), mcpfeedback.WithSettingsFreshFor(0))
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// seedHostedServer inserts an MCP server whose tools Speakeasy serves itself,
// the only kind that offers the feedback tool.
func seedHostedServer(t *testing.T, ctx context.Context, ti *testInstance) mcpserversrepo.McpServer {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "acme-tasks-" + uuid.NewString()
	toolsetID, err := testrepo.New(ti.conn).CreateToolsetFixture(ctx, testrepo.CreateToolsetFixtureParams{
		ID:             uuid.Must(uuid.NewV7()),
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Acme Tasks",
		Slug:           slug,
	})
	require.NoError(t, err)

	return seedServer(t, ctx, ti, func(params *mcpserversrepo.CreateMCPServerParams) {
		params.ToolsetID = uuid.NullUUID{UUID: toolsetID, Valid: true}
	})
}

// seedRemoteBackedServer inserts an MCP server whose traffic Speakeasy
// proxies to a third party's server.
func seedRemoteBackedServer(t *testing.T, ctx context.Context, ti *testInstance) mcpserversrepo.McpServer {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remote := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcprepo.CreateServerParams{
		ID:            uuid.Nil,
		ProjectID:     *authCtx.ProjectID,
		Name:          pgtype.Text{String: "", Valid: false},
		Slug:          pgtype.Text{String: "", Valid: false},
		TransportType: "streamable-http",
		Url:           "https://feedback.example.test/mcp/" + uuid.NewString(),
	})

	return seedServer(t, ctx, ti, func(params *mcpserversrepo.CreateMCPServerParams) {
		params.RemoteMcpServerID = uuid.NullUUID{UUID: remote.ID, Valid: true}
	})
}

// seedCataloguedServer inserts an MCP server agents reach directly, without
// going through Speakeasy.
func seedCataloguedServer(t *testing.T, ctx context.Context, ti *testInstance) mcpserversrepo.McpServer {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	unproxied, err := unproxiedmcprepo.New(ti.conn).CreateServer(ctx, unproxiedmcprepo.CreateServerParams{
		ID:          uuid.Must(uuid.NewV7()),
		ProjectID:   *authCtx.ProjectID,
		Name:        conv.ToPGText("Catalogued"),
		Slug:        conv.ToPGText("catalogued-" + uuid.NewString()),
		Url:         "https://catalogued.example.test/mcp",
		Description: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	return seedServer(t, ctx, ti, func(params *mcpserversrepo.CreateMCPServerParams) {
		params.UnproxiedMcpServerID = uuid.NullUUID{UUID: unproxied.ID, Valid: true}
	})
}

func seedServer(t *testing.T, ctx context.Context, ti *testInstance, backend func(*mcpserversrepo.CreateMCPServerParams)) mcpserversrepo.McpServer {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	id := uuid.Must(uuid.NewV7())
	params := mcpserversrepo.CreateMCPServerParams{
		ID:                    id,
		ProjectID:             *authCtx.ProjectID,
		Name:                  conv.ToPGText("Acme Tasks"),
		Slug:                  conv.ToPGText("acme-tasks-" + id.String()),
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            "private",
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	}
	backend(&params)

	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, params)
	require.NoError(t, err)
	return server
}

// targetOf is the recorder target for a seeded server.
func targetOf(t *testing.T, ctx context.Context, server mcpserversrepo.McpServer) mcpfeedback.Target {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	return mcpfeedback.Target{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: server.ProjectID, McpServerID: server.ID}
}

// exportOn has every organization's reports queued for the OTLP export.
func exportOn() *feature.InMemory {
	flags := new(feature.InMemory)
	flags.SetFlag(feature.FlagMCPFeedbackExport, feature.AnyDistinctID, true)
	return flags
}
