package plugins_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/jackc/pgx/v5/pgtype"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	infra *testenv.Environment
)

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
	service        *plugins.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
}

func newTestPluginsService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := context.Background()

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
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	ctx = withauthzGrants(t, ctx, conn,
		authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)},
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	auditLogger := audit.NewLogger()

	svc := plugins.NewService(logger, tracerProvider, conn, sessionManager, authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), auditLogger, nil, "local", "https://app.getgram.ai", nil)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}

func newTestPluginsServiceWithGitHub(t *testing.T, ghClient plugins.GitHubPublisher) (context.Context, *testInstance) {
	t.Helper()
	return newTestPluginsServiceWithGitHubAndFeatures(t, ghClient, nil)
}

// newTestPluginsServiceWithGitHubAndFeatures builds a dashboard-style Service
// (with auth) that also carries a feature provider, so the phased-rollout gating
// on human-initiated hook-output changes (marketplace rename, observability-mode
// toggle) can be exercised end to end.
func newTestPluginsServiceWithGitHubAndFeatures(t *testing.T, ghClient plugins.GitHubPublisher, features feature.Provider) (context.Context, *testInstance) {
	t.Helper()

	ctx := context.Background()

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
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	ctx = withauthzGrants(t, ctx, conn,
		authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)},
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	ghConfig := &plugins.GitHubConfig{
		Client:         ghClient,
		Org:            "test-org",
		InstallationID: 12345,
	}

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	auditLogger := audit.NewLogger()

	svc := plugins.NewService(
		logger,
		tracerProvider,
		conn,
		sessionManager,
		authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
		auditLogger,
		ghConfig,
		"local",
		"https://app.getgram.ai",
		features,
	)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}

// newTestPluginPublisher builds an automated-publisher Service (as the Temporal
// worker does) that shares ti's database and GitHub mock but carries a feature
// provider, so phased-rollout gating can be exercised end to end. Build fixtures
// via ti.service (which has auth); publish via the returned publisher.
func newTestPluginPublisher(t *testing.T, ti *testInstance, ghClient plugins.GitHubPublisher, features feature.Provider) *plugins.Service {
	t.Helper()

	ghConfig := &plugins.GitHubConfig{
		Client:         ghClient,
		Org:            "test-org",
		InstallationID: 12345,
	}

	return plugins.NewPublisher(
		testenv.NewLogger(t),
		ti.conn,
		audit.NewLogger(),
		ghConfig,
		"local",
		"https://app.getgram.ai",
		features,
	)
}

// rewindPublishedHooksVersion overwrites the stored hooks generator version for
// a project's GitHub connection, simulating an org that last received an older
// hooks version than the current generator. It preserves every other connection
// field so the MCP fingerprints still match (an MCP publish stays unchanged).
func rewindPublishedHooksVersion(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, version string) {
	t.Helper()

	q := pluginsrepo.New(conn)
	current, err := q.GetGitHubConnection(ctx, projectID)
	require.NoError(t, err)

	_, err = q.UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID:                projectID,
		InstallationID:           current.InstallationID,
		RepoOwner:                current.RepoOwner,
		RepoName:                 current.RepoName,
		MarketplaceToken:         current.MarketplaceToken,
		PublishedMcpFingerprints: current.PublishedMcpFingerprints,
		PublishedHooksVersion:    conv.ToPGText(version),
		PublishedHooksConfig:     current.PublishedHooksConfig,
	})
	require.NoError(t, err)
}

// publishOrgID returns the organization id the publisher resolves for a project
// — the org-metadata id used as the FlagHooksRollout distinct id, which is not
// necessarily the same string as authCtx.ActiveOrganizationID.
func publishOrgID(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) string {
	t.Helper()

	project, err := projectsrepo.New(conn).GetProjectWithOrganizationMetadata(ctx, projectID)
	require.NoError(t, err)
	return project.ID
}

// countPluginHooksKeys returns how many hooks-scoped plugin API keys the org
// has. Regenerating the hooks component mints a fresh one, so a change in this
// count distinguishes a hooks regeneration from a carry.
func countPluginHooksKeys(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string) int {
	t.Helper()

	keys, err := keysrepo.New(conn).ListAPIKeysByOrganization(ctx, orgID)
	require.NoError(t, err)

	count := 0
	for _, k := range keys {
		if strings.HasPrefix(k.Name, "plugins-hooks-") {
			count++
		}
	}
	return count
}

// publishedHooksVersion reads back the stored hooks generator version.
func publishedHooksVersion(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) string {
	t.Helper()

	current, err := pluginsrepo.New(conn).GetGitHubConnection(ctx, projectID)
	require.NoError(t, err)
	return current.PublishedHooksVersion.String
}

// orgObservabilitySlugs returns the per-org observability plugin directory
// names that PublishPlugins will produce, computed the same way the impl does.
func orgObservabilitySlugs(t *testing.T, ctx context.Context, ti *testInstance) (claudeObservability, cursorObservability string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgName, err := pluginsrepo.New(ti.conn).GetOrganizationName(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	cfg := plugins.GenerateConfig{OrgName: orgName}
	return plugins.ClaudeObservabilitySlug(cfg), plugins.CursorObservabilitySlug(cfg)
}

func createTestToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, name string) toolsetsrepo.Toolset {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := fmt.Sprintf("test-%s-%s", name, uuid.New().String()[:8])
	ts, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   name,
		Slug:                   slug,
		Description:            pgtype.Text{Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{Valid: false},
		McpSlug:                pgtype.Text{String: slug, Valid: true},
		McpEnabled:             true,
	})
	require.NoError(t, err)
	return ts
}

// mcpServerFixture is an mcp_server with (optionally) a single endpoint,
// created directly via the repos. The plugin publishing path only reads the
// mcp_server's id/name/slug/visibility and its endpoints, so a toolset-backed
// server stands in for a Remote MCP-backed one without the remote_mcp_server /
// user_session_issuer fixture weight.
type mcpServerFixture struct {
	id           uuid.UUID
	idStr        string
	name         string
	slug         string
	endpointSlug string
}

// createTestMcpServer creates an mcp_server in the active project with a single
// platform endpoint (no custom domain). visibility controls publishability.
func createTestMcpServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, name, visibility string) mcpServerFixture {
	t.Helper()
	return createTestMcpServerWithEndpoint(t, ctx, conn, name, visibility, true)
}

func createTestMcpServerWithEndpoint(t *testing.T, ctx context.Context, conn *pgxpool.Pool, name, visibility string, withEndpoint bool) mcpServerFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Back the mcp_server with a toolset to satisfy the backend-exclusivity
	// check; the plugin path does not distinguish remote- vs toolset-backed.
	backing := createTestToolset(t, ctx, conn, name+"-backing")

	slug := fmt.Sprintf("mcp-%s-%s", name, uuid.New().String()[:8])
	serverID := uuid.New()
	_, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                serverID,
		ProjectID:         *authCtx.ProjectID,
		Name:              pgtype.Text{String: name, Valid: true},
		Slug:              pgtype.Text{String: slug, Valid: true},
		ToolsetID:         uuid.NullUUID{UUID: backing.ID, Valid: true},
		RemoteMcpServerID: uuid.NullUUID{},
		Visibility:        visibility,
	})
	require.NoError(t, err)

	fixture := mcpServerFixture{id: serverID, idStr: serverID.String(), name: name, slug: slug}

	if withEndpoint {
		endpointSlug := slug + "-endpoint"
		_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
			ProjectID:      *authCtx.ProjectID,
			CustomDomainID: uuid.NullUUID{},
			McpServerID:    serverID,
			Slug:           endpointSlug,
		})
		require.NoError(t, err)
		fixture.endpointSlug = endpointSlug
	}

	return fixture
}

func withauthzGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, grants ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	for _, grant := range grants {
		selectors, _ := grant.Selector.MarshalJSON()
		_, err := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   userPrincipal,
			Scope:          string(grant.Scope),
			Selectors:      selectors,
		})
		require.NoError(t, err)
	}

	loadedGrants, err := authz.LoadGrants(ctx, conn, authCtx.ActiveOrganizationID, []urn.Principal{userPrincipal})
	require.NoError(t, err)

	return authz.GrantsToContext(ctx, loadedGrants)
}
