package mcp_test

import (
	"context"
	"log"
	"log/slog"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	tm_repo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/keys"
	keys_gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	temporal_client "go.temporal.io/sdk/client"
)

var (
	infra *testenv.Environment
	funcs functions.ToolCaller
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background())
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

	sessionManager, err := sessions.NewUnsafeManager(logger, conn, redisClient, cache.Suffix("gram-test"), "", billingClient)
	require.NoError(t, err)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	serverURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	siteURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	env := environments.NewEnvironmentEntries(logger, conn, enc)
	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	guardianPolicy := guardian.NewDefaultPolicy()
	oauthService := oauth.NewService(logger, tracerProvider, meterProvider, conn, serverURL, cacheAdapter, enc, env, sessionManager)
	billingStub := billing.NewStubClient(logger, tracerProvider)
	devProvisioner := openrouter.NewDevelopment("test-openrouter-key")
	chatClient := openrouter.NewChatClient(logger, devProvisioner)
	vectorToolStore := rag.NewToolsetVectorStore(tracerProvider, conn, chatClient)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	toolMetrics := tm_repo.New(logger, tracerProvider, chConn, func(context.Context, string) (bool, error) {
		return true, nil
	})

	var temporalClient temporal_client.Client
	temporalClient, devserver := infra.NewTemporalClient(t)
	t.Cleanup(func() {
		temporalClient.Close()
		require.NoError(t, devserver.Stop(), "shutdown temporal")
	})

	redisClient, err2 := infra.NewRedisClient(t, 0)
	require.NoError(t, err2)
	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")
	svc := mcp.NewService(logger, tracerProvider, meterProvider, conn, sessionManager, chatSessionsManager, env, posthog, serverURL, enc, cacheAdapter, guardianPolicy, funcs, oauthService, billingStub, billingStub, toolMetrics, vectorToolStore, temporalClient)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		serverURL:      serverURL,
		siteURL:        siteURL,
		logger:         logger,
		cacheAdapter:   cacheAdapter,
	}
}

// mockOAuthService allows controlling OAuth validation behavior in tests
type mockOAuthService struct {
	validateFunc func(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*oauth.Token, error)
}

func (m *mockOAuthService) ValidateAccessToken(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*oauth.Token, error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, toolsetId, accessToken)
	}
	return nil, oauth.ErrInvalidAccessToken
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

	sessionManager, err := sessions.NewUnsafeManager(logger, conn, redisClient, cache.Suffix("gram-test"), "", billingClient)
	require.NoError(t, err)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	serverURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	siteURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	env := environments.NewEnvironmentEntries(logger, conn, enc)
	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	guardianPolicy := guardian.NewDefaultPolicy()
	billingStub := billing.NewStubClient(logger, tracerProvider)
	devProvisioner := openrouter.NewDevelopment("test-openrouter-key")
	chatClient := openrouter.NewChatClient(logger, devProvisioner)
	vectorToolStore := rag.NewToolsetVectorStore(tracerProvider, conn, chatClient)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	toolMetrics := tm_repo.New(logger, tracerProvider, chConn, func(context.Context, string) (bool, error) {
		return true, nil
	})

	var temporalClient temporal_client.Client
	temporalClient, devserver := infra.NewTemporalClient(t)
	t.Cleanup(func() {
		temporalClient.Close()
		require.NoError(t, devserver.Stop(), "shutdown temporal")
	})

	redisClient, err2 := infra.NewRedisClient(t, 0)
	require.NoError(t, err2)
	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")
	svc := mcp.NewService(logger, tracerProvider, meterProvider, conn, sessionManager, chatSessionsManager, env, posthog, serverURL, enc, cacheAdapter, guardianPolicy, funcs, oauthSvc, billingStub, billingStub, toolMetrics, vectorToolStore, temporalClient)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		serverURL:      serverURL,
		siteURL:        siteURL,
		logger:         logger,
		cacheAdapter:   cacheAdapter,
	}
}

// createTestAPIKey creates an API key for the test context project
func (ti *testInstance) createTestAPIKey(ctx context.Context, t *testing.T) string {
	t.Helper()
	keysService := keys.NewService(ti.logger, ti.conn, ti.sessionManager, "local")

	key, err := keysService.CreateKey(ctx, &keys_gen.CreateKeyPayload{
		Name:   "test-key",
		Scopes: []string{"consumer"},
	})
	require.NoError(t, err)

	return *key.Key
}

// getSessionToken returns a valid session token for testing
func (ti *testInstance) getSessionToken(ctx context.Context, t *testing.T) string {
	t.Helper()
	sessionToken, err := ti.sessionManager.PopulateLocalDevDefaultAuthSession(ctx)
	require.NoError(t, err)
	return sessionToken
}
