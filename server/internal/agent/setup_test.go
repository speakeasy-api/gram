package agent_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const testServerURL = "https://app.getgram.ai"

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
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
	service   *agent.Service
	conn      *pgxpool.Pool
	orgID     string
	projectID uuid.UUID
}

// newTestAgentService clones a fresh DB, seeds the mock org + a project (via
// InitAuthContext), and wires a real agent.Service. The agent handler reads
// only the auth context's org and queries the DB — it does not run authz
// checks — so no role grants are seeded.
func newTestAgentService(t *testing.T) (context.Context, *testInstance) {
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
	require.NotNil(t, authCtx.ProjectID)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	svc := agent.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, testServerURL)

	return ctx, &testInstance{
		service:   svc,
		conn:      conn,
		orgID:     authCtx.ActiveOrganizationID,
		projectID: *authCtx.ProjectID,
	}
}

// publishMarketplace gives a project a marketplace_token, which is what makes
// its marketplace visible to the agent endpoint (the query requires one).
func publishMarketplace(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, token string) {
	t.Helper()
	_, err := pluginsrepo.New(conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID:        projectID,
		InstallationID:   1,
		RepoOwner:        "speakeasy",
		RepoName:         "plugins-" + token, // unique per (installation, owner, repo)
		MarketplaceToken: pgtype.Text{String: token, Valid: true},
	})
	require.NoError(t, err)
}

// seedProject creates an additional project in an existing org, used to exercise
// multi-project orgs (e.g. the default-project selection in GetAgentPluginSet).
func seedProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, slug string) uuid.UUID {
	t.Helper()
	p, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	return p.ID
}

// setMarketplaceOverride sets a project's per-project marketplace name override,
// the same row UpdateMarketplaceSettings writes. Used to exercise the agent
// emitting a project's published (override) name rather than the org default.
func setMarketplaceOverride(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, name string) {
	t.Helper()
	_, err := pluginsrepo.New(conn).UpsertMarketplaceSettings(ctx, pluginsrepo.UpsertMarketplaceSettingsParams{
		ProjectID:       projectID,
		MarketplaceName: pgtype.Text{String: name, Valid: true},
	})
	require.NoError(t, err)
}

func seedPlugin(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	p, err := pluginsrepo.New(conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: orgID,
		ProjectID:      projectID,
		Name:           slug,
		Slug:           slug,
	})
	require.NoError(t, err)
	return p.ID
}

func assignPlugin(t *testing.T, ctx context.Context, conn *pgxpool.Pool, pluginID uuid.UUID, orgID, principalURN string) {
	t.Helper()
	_, err := pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		PluginID:       pluginID,
		OrganizationID: orgID,
		PrincipalUrn:   principalURN,
	})
	require.NoError(t, err)
}

// seedSecondOrg creates a separate org with its own published marketplace +
// assigned plugin, used to prove the endpoint never leaks another org's data.
func seedSecondOrg(t *testing.T, ctx context.Context, conn *pgxpool.Pool) {
	t.Helper()
	const otherOrgID = "other-org-id"
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:   otherOrgID,
		Name: "Other Org",
		Slug: "other-org",
	})
	require.NoError(t, err)

	proj, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "other-proj",
		Slug:           "other-proj",
		OrganizationID: otherOrgID,
	})
	require.NoError(t, err)

	publishMarketplace(t, ctx, conn, proj.ID, "other-token")
	pid := seedPlugin(t, ctx, conn, otherOrgID, proj.ID, "other-plugin")
	assignPlugin(t, ctx, conn, pid, otherOrgID, "*")
}
