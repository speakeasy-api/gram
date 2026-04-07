package mcp_test

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"

	keys_gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/keys"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

var (
	infra *testenv.Environment
	funcs functions.ToolCaller
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true, Temporal: true})
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
	service        *mcp.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
	serverURL      *url.URL
	siteURL        *url.URL
	logger         *slog.Logger
	tracerProvider trace.TracerProvider
	cacheAdapter   cache.Cache
}

func newTestMCPService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := noop.NewMeterProvider()

	conn, err := infra.CloneTestDatabase(t, "mcptest")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-test"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	serverURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	siteURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	mcpMetadataRepo := mcpmetadata_repo.New(conn)
	env := environments.NewEnvironmentEntries(logger, conn, enc, mcpMetadataRepo)
	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	guardianPolicy := guardian.NewDefaultPolicy()
	oauthService := oauth.NewService(logger, tracerProvider, meterProvider, conn, serverURL, cacheAdapter, enc, env, sessionManager)
	billingStub := billing.NewStubClient(logger, tracerProvider)
	devProvisioner := openrouter.NewDevelopment("test-openrouter-key")
	chatClient := openrouter.NewUnifiedClient(logger, devProvisioner, nil, nil, nil, nil, nil)
	vectorToolStore := rag.NewToolsetVectorStore(logger, tracerProvider, conn, chatClient)
	chatSessions := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")
	featClient := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	logsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	toolIOLogsEnabled := func(_ context.Context, _ string) (bool, error) { return false, nil }
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	telemService := telemetry.NewService(
		logger,
		tracerProvider,
		conn,
		chConn,
		sessionManager,
		chatSessions,
		logsEnabled,
		toolIOLogsEnabled,
		posthog,
		access.NewManager(logger, conn, nil),
	)

	temporalEnv, _ := infra.NewTemporalEnv(t)

	redisClient, err2 := infra.NewRedisClient(t, 0)
	require.NoError(t, err2)
	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")
	svc := mcp.NewService(logger, tracerProvider, meterProvider, conn, sessionManager, chatSessionsManager, env, posthog, serverURL, enc, cacheAdapter, guardianPolicy, funcs, oauthService, billingStub, billingStub, telemService, featClient, vectorToolStore, temporalEnv, access.NewManager(logger, conn, nil))

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		serverURL:      serverURL,
		siteURL:        siteURL,
		logger:         logger,
		tracerProvider: tracerProvider,
		cacheAdapter:   cacheAdapter,
	}
}

// mockOAuthService allows controlling OAuth validation behavior in tests
type mockOAuthService struct {
	validateFunc func(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*oauth.Token, error)
	refreshFunc  func(ctx context.Context, toolsetID uuid.UUID, token *oauth.Token, proxyProvider *oauth_repo.OauthProxyProvider, toolset *toolsets_repo.Toolset) (*oauth.Token, error)
}

func (m *mockOAuthService) ValidateAccessToken(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*oauth.Token, error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, toolsetId, accessToken)
	}
	return nil, oauth.ErrInvalidAccessToken
}

func (m *mockOAuthService) RefreshProxyToken(ctx context.Context, toolsetID uuid.UUID, token *oauth.Token, proxyProvider *oauth_repo.OauthProxyProvider, toolset *toolsets_repo.Toolset) (*oauth.Token, error) {
	if m.refreshFunc != nil {
		return m.refreshFunc(ctx, toolsetID, token, proxyProvider, toolset)
	}
	return nil, fmt.Errorf("not implemented")
}

// newTestMCPServiceWithOAuth creates a test MCP service with a custom OAuth service
func newTestMCPServiceWithOAuth(t *testing.T, oauthSvc mcp.OAuthService) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := noop.NewMeterProvider()

	conn, err := infra.CloneTestDatabase(t, "mcptest")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-test"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	serverURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	siteURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	mcpMetadataRepo := mcpmetadata_repo.New(conn)
	env := environments.NewEnvironmentEntries(logger, conn, enc, mcpMetadataRepo)
	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	guardianPolicy := guardian.NewDefaultPolicy()
	billingStub := billing.NewStubClient(logger, tracerProvider)
	devProvisioner := openrouter.NewDevelopment("test-openrouter-key")
	chatClient := openrouter.NewUnifiedClient(logger, devProvisioner, nil, nil, nil, nil, nil)
	vectorToolStore := rag.NewToolsetVectorStore(logger, tracerProvider, conn, chatClient)
	featClient := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	temporalEnv, _ := infra.NewTemporalEnv(t)

	redisClient, err2 := infra.NewRedisClient(t, 0)
	require.NoError(t, err2)
	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")
	logsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	toolIOLogsEnabled := func(_ context.Context, _ string) (bool, error) { return false, nil }

	telemService := telemetry.NewService(
		logger,
		tracerProvider,
		conn,
		chConn,
		sessionManager,
		chatSessionsManager,
		logsEnabled,
		toolIOLogsEnabled,
		posthog,
		access.NewManager(logger, conn, nil),
	)
	svc := mcp.NewService(logger, tracerProvider, meterProvider, conn, sessionManager, chatSessionsManager, env, posthog, serverURL, enc, cacheAdapter, guardianPolicy, funcs, oauthSvc, billingStub, billingStub, telemService, featClient, vectorToolStore, temporalEnv, access.NewManager(logger, conn, nil))

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		serverURL:      serverURL,
		siteURL:        siteURL,
		logger:         logger,
		tracerProvider: tracerProvider,
		cacheAdapter:   cacheAdapter,
	}
}

// createTestAPIKey creates an API key for the test context project
func (ti *testInstance) createTestAPIKey(ctx context.Context, t *testing.T) string {
	t.Helper()
	keysService := keys.NewService(ti.logger, ti.tracerProvider, ti.conn, ti.sessionManager, "local", access.NewManager(ti.logger, ti.conn, nil))

	key, err := keysService.CreateKey(ctx, &keys_gen.CreateKeyPayload{
		Name:   "test-key",
		Scopes: []string{"consumer"},
	})
	require.NoError(t, err)

	return *key.Key
}

// getSessionToken returns a valid session token for testing.
// The session must already be established via InitAuthContext.
func (ti *testInstance) getSessionToken(ctx context.Context, t *testing.T) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context must be set before calling getSessionToken")
	require.NotNil(t, authCtx.SessionID, "session ID must be set in auth context")
	return *authCtx.SessionID
}

// addToolWithSecurity creates a deployment, an HTTP tool definition with an apiKey
// security scheme, and a toolset_version linking them. This makes DescribeToolset
// return SecurityVariables so the security check in ServePublic is exercised.
// Returns the env var name used for the apiKey scheme.
func (ti *testInstance) addToolWithSecurity(ctx context.Context, t *testing.T, toolsetID uuid.UUID, projectID uuid.UUID, orgID string) string {
	t.Helper()

	envVarName := "TEST_API_KEY"

	// Create deployment
	var deploymentID uuid.UUID
	err := ti.conn.QueryRow(ctx, `
		INSERT INTO deployments (project_id, organization_id, user_id, idempotency_key)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, projectID, orgID, "test-user", uuid.New().String()).Scan(&deploymentID)
	require.NoError(t, err)

	_, err = ti.conn.Exec(ctx, `
		INSERT INTO deployment_statuses (deployment_id, status)
		VALUES ($1, 'completed')
	`, deploymentID)
	require.NoError(t, err)

	// Create HTTP tool definition with security referencing "test_api_key" scheme
	toolURN := "tools:http:test-api:" + uuid.New().String()[:8]
	_, err = ti.conn.Exec(ctx, `
		INSERT INTO http_tool_definitions (
			project_id, deployment_id, tool_urn, name, untruncated_name,
			summary, description, tags, http_method, path,
			schema_version, schema, server_env_var, security,
			header_settings, query_settings, path_settings
		) VALUES (
			$1, $2, $3, 'test_tool', '', 'Test tool', 'A test tool with security',
			'{}', 'GET', '/test', '3.0.0', '{}', 'TEST_SERVER_URL',
			$4, '{}', '{}', '{}'
		)
	`, projectID, deploymentID, toolURN, `[{"test_api_key": []}]`)
	require.NoError(t, err)

	// Create matching http_security row
	_, err = ti.conn.Exec(ctx, `
		INSERT INTO http_security (
			key, deployment_id, project_id, type, name, in_placement, env_variables
		) VALUES ($1, $2, $3, 'apiKey', 'X-Api-Key', 'header', $4)
	`, "test_api_key", deploymentID, projectID, []string{envVarName})
	require.NoError(t, err)

	// Create toolset_version linking the tool
	_, err = ti.conn.Exec(ctx, `
		INSERT INTO toolset_versions (toolset_id, version, tool_urns, resource_urns)
		VALUES ($1, 1, $2, '{}')
	`, toolsetID, []string{toolURN})
	require.NoError(t, err)

	return envVarName
}
