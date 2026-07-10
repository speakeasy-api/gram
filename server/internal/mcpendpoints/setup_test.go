package mcpendpoints_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcptest"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functionstest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true, Temporal: true})
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

type testInstance struct {
	service        *mcpendpoints.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
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

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	auditLogger := audit.NewLogger()

	svc := mcpendpoints.NewService(logger, tracerProvider, conn, sessionManager, authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), auditLogger, nil, false)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}

// fakeGitHubPublisher is a no-op plugins.GitHubPublisher: it never touches
// the network, so the initial-publish workflow's activity completes
// successfully and leaves behind a real plugin_github_connections row.
type fakeGitHubPublisher struct{}

func (fakeGitHubPublisher) CreateRepo(ctx context.Context, installationID int64, org, name string, private bool) error {
	return nil
}

func (fakeGitHubPublisher) PushFiles(ctx context.Context, installationID int64, owner, repo, branch, commitMsg string, files map[string][]byte) (string, error) {
	return "fake-sha", nil
}

func (fakeGitHubPublisher) AddCollaborator(ctx context.Context, installationID int64, owner, repo, username, permission string) error {
	return nil
}

func (fakeGitHubPublisher) HasDirectCollaborator(ctx context.Context, installationID int64, owner, repo string) (bool, error) {
	return false, nil
}

func (fakeGitHubPublisher) GetRepoFiles(ctx context.Context, installationID int64, owner, repo, branch string) (map[string][]byte, error) {
	return nil, ghclient.ErrRepoNotFound
}

// newTestServiceWithGitHubPublishing is newTestService with GitHub publishing
// turned on end-to-end: a Temporal worker is started, configured with a
// plugins.Service backed by a fake (no-op) GitHub client, so an mcp_server's
// auto-attach-triggered initial publish actually runs its workflow to
// completion instead of just being enqueued.
func newTestServiceWithGitHubPublishing(t *testing.T) (context.Context, *testInstance, *temporal.Environment) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	assetStorage := assetstest.NewTestBlobStore(t)
	enc := testenv.NewEncryptionClient(t)
	funcs := functionstest.NewOrchestrator(t, assetStorage)
	mcpRegistryClient := externalmcptest.NewRegistryClient(t, logger, tracerProvider)
	temporalEnv, _ := infra.NewTemporalEnv(t)
	auditLogger := audit.NewLogger()
	f := &feature.InMemory{}

	ghConfig := &plugins.GitHubConfig{
		Client:         fakeGitHubPublisher{},
		Org:            "test-org",
		InstallationID: 12345,
	}
	pluginPublisher := plugins.NewPublisher(logger, conn, auditLogger, ghConfig, "local", "https://app.getgram.ai")

	worker := background.NewTemporalWorker(temporalEnv, logger, tracerProvider, meterProvider,
		background.ForDeploymentProcessing(guardianPolicy, conn, f, assetStorage, enc, funcs, mcpRegistryClient, auditLogger),
		&background.WorkerOptions{PluginPublisher: pluginPublisher},
	)
	t.Cleanup(func() {
		worker.Stop()
	})
	require.NoError(t, worker.Start(), "start temporal worker")

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	svc := mcpendpoints.NewService(logger, tracerProvider, conn, sessionManager, authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), auditLogger, temporalEnv, true)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
	}, temporalEnv
}

func withExactAuthzGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, grants ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "mcpendpoints-rbac-grants-"+uuid.NewString())
	for _, grant := range grants {
		selectors, err := grant.Selector.MarshalJSON()
		require.NoError(t, err)
		_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   principal,
			Scope:          string(grant.Scope),
			Selectors:      selectors,
		})
		require.NoError(t, err)
	}

	loadedGrants, err := authz.LoadGrants(ctx, conn, authCtx.ActiveOrganizationID, []urn.Principal{principal})
	require.NoError(t, err)

	return authz.GrantsToContext(ctx, loadedGrants)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// seedMcpServer creates a remote_mcp_server + mcp_server row directly
// through the generated repos so slug tests have a valid mcp_server_id FK
// without depending on the mcpfrontends service package.
func seedMcpServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	server := remotemcptest.SeedServer(t, ctx, conn, remotemcprepo.CreateServerParams{
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           "https://test.example.com/mcp/" + uuid.NewString(),
	})

	mcpServerID, err := uuid.NewV7()
	require.NoError(t, err)
	frontend, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                mcpServerID,
		ProjectID:         projectID,
		Name:              conv.ToPGText("test mcp server"),
		Slug:              conv.ToPGText("test-mcp-server-" + uuid.NewString()),
		EnvironmentID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID: uuid.NullUUID{UUID: server.ID, Valid: true},
		ToolsetID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:        "disabled",
	})
	require.NoError(t, err)

	return frontend.ID
}

// seedOtherProjectMcpFrontend creates an additional project in the caller's
// organization and inserts an mcp_server under that *other* project.
// Used to exercise cross-tenant ownership rejection.
func seedOtherProjectMcpFrontend(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	slug := "other-" + uuid.New().String()[:8]
	otherProject, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	server := remotemcptest.SeedServer(t, ctx, conn, remotemcprepo.CreateServerParams{
		ProjectID:     otherProject.ID,
		TransportType: "streamable-http",
		Url:           "https://other.example.com/mcp/" + uuid.NewString(),
	})

	mcpServerID, err := uuid.NewV7()
	require.NoError(t, err)
	frontend, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                mcpServerID,
		ProjectID:         otherProject.ID,
		Name:              conv.ToPGText("test mcp server"),
		Slug:              conv.ToPGText("test-mcp-server-" + uuid.NewString()),
		EnvironmentID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID: uuid.NullUUID{UUID: server.ID, Valid: true},
		ToolsetID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:        "disabled",
	})
	require.NoError(t, err)

	return frontend.ID
}
