package plugins_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/jackc/pgx/v5/pgtype"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
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

	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	ctx = withauthzGrants(t, ctx, conn,
		authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)},
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	svc := plugins.NewService(logger, tracerProvider, conn, sessionManager, authz.NewEngine(logger, conn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache), nil, "local", "https://app.getgram.ai")

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}

func newTestPluginsServiceWithGitHub(t *testing.T, ghClient plugins.GitHubPublisher) (context.Context, *testInstance) {
	t.Helper()

	ctx := context.Background()

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

	svc := plugins.NewService(
		logger, tracerProvider, conn, sessionManager,
		authz.NewEngine(logger, conn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache),
		ghConfig, "local", "https://app.getgram.ai",
	)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
	}
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

// allOrgKeysIncludingSystemManaged returns every API key (active + system-managed)
// for an organization. The production ListAPIKeysByOrganization query
// excludes system-managed keys so they don't pollute the dashboard's keys
// page; tests need to see them to verify plugin publish behavior.
func allOrgKeysIncludingSystemManaged(ctx context.Context, t *testing.T, conn *pgxpool.Pool, organizationID string) ([]keysrepo.ApiKey, error) {
	t.Helper()

	rows, err := conn.Query(ctx, `
		SELECT id, organization_id, project_id, created_by_user_id, name,
		       key_prefix, key_hash, scopes, toolset_id, plugin_id,
		       system_managed, created_at, updated_at, deleted_at, deleted,
		       last_accessed_at
		FROM api_keys
		WHERE organization_id = $1 AND deleted IS FALSE
		ORDER BY created_at DESC
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("query api_keys: %w", err)
	}
	defer rows.Close()

	var keys []keysrepo.ApiKey
	for rows.Next() {
		var k keysrepo.ApiKey
		if err := rows.Scan(
			&k.ID, &k.OrganizationID, &k.ProjectID, &k.CreatedByUserID, &k.Name,
			&k.KeyPrefix, &k.KeyHash, &k.Scopes, &k.ToolsetID, &k.PluginID,
			&k.SystemManaged, &k.CreatedAt, &k.UpdatedAt, &k.DeletedAt, &k.Deleted,
			&k.LastAccessedAt,
		); err != nil {
			return nil, fmt.Errorf("scan api_keys: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api_keys rows: %w", err)
	}
	return keys, nil
}
