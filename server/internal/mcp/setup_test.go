package mcp_test

import (
	"context"
	"log"
	"log/slog"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	keys_gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/keys"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	tools_repo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	enc            *encryption.Client
	authzEngine    *authz.Engine
	audit          *audit.Logger
}

func newTestMCPService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "mcptest")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-test"), billingClient)

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
	oauthService := oauth.NewService(logger, tracerProvider, meterProvider, conn, serverURL, cacheAdapter, enc, env, sessionManager, guardianPolicy)
	billingStub := billing.NewStubClient(logger, tracerProvider)
	devProvisioner := openrouter.NewDevelopment("test-openrouter-key")
	chatClient := openrouter.NewUnifiedClient(logger, guardianPolicy, devProvisioner, nil, nil, nil, nil, nil)
	vectorToolStore := rag.NewToolsetVectorStore(logger, tracerProvider, conn, chatClient)
	chatSessions := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")
	featClient := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	logsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	toolIOLogsEnabled := func(_ context.Context, _ string) (bool, error) { return false, nil }
	sessionCaptureEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)

	telemLogger := telemetry.NewLogger(ctx, logger, chConn, logsEnabled, toolIOLogsEnabled)
	telemService := telemetry.NewService(
		logger,
		tracerProvider,
		conn,
		chConn,
		sessionManager,
		chatSessions,
		logsEnabled,
		sessionCaptureEnabled,
		posthog,
		authzEngine,
	)

	temporalEnv, _ := infra.NewTemporalEnv(t)

	redisClient, err2 := infra.NewRedisClient(t, 0)
	require.NoError(t, err2)
	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")
	assistantTokens := assistanttokens.New("test-jwt-secret", conn, authzEngine)
	_ = featClient
	shadowMCPClient := shadowmcp.NewClient(logger, conn, cacheAdapter)
	auditLogger := audit.NewLogger()
	svc := mcp.NewService(logger, tracerProvider, meterProvider, conn, sessionManager, chatSessionsManager, env, posthog, serverURL, enc, cacheAdapter, guardianPolicy, funcs, oauthService, billingStub, billingStub, telemLogger, telemService, vectorToolStore, nil, temporalEnv, authzEngine, assistantTokens, shadowMCPClient, auditLogger)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		serverURL:      serverURL,
		siteURL:        siteURL,
		logger:         logger,
		tracerProvider: tracerProvider,
		cacheAdapter:   cacheAdapter,
		enc:            enc,
		authzEngine:    authzEngine,
		audit:          auditLogger,
	}
}

// createTestAPIKey creates an API key for the test context project
func (ti *testInstance) createTestAPIKey(ctx context.Context, t *testing.T) string {
	t.Helper()
	keysService := keys.NewService(ti.logger, ti.tracerProvider, ti.conn, ti.sessionManager, "local", ti.authzEngine, ti.audit)

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

	deploymentID, err := deployments_repo.New(ti.conn).InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         "test-user",
		IdempotencyKey: uuid.New().String(),
	})
	require.NoError(t, err)

	err = deployments_repo.New(ti.conn).CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	})
	require.NoError(t, err)

	toolURN := urn.NewTool(urn.ToolKindHTTP, "test-api", uuid.New().String()[:8])
	err = tools_repo.New(ti.conn).CreateHTTPToolDefinition(ctx, tools_repo.CreateHTTPToolDefinitionParams{
		ProjectID:       projectID,
		DeploymentID:    deploymentID,
		ToolUrn:         toolURN,
		Name:            "test_tool",
		UntruncatedName: pgtype.Text{},
		Summary:         "Test tool",
		Description:     "A test tool with security",
		Tags:            []string{},
		HttpMethod:      "GET",
		Path:            "/test",
		SchemaVersion:   "3.0.0",
		Schema:          []byte(`{}`),
		ServerEnvVar:    "TEST_SERVER_URL",
		Security:        []byte(`[{"test_api_key": []}]`),
		HeaderSettings:  []byte(`{}`),
		QuerySettings:   []byte(`{}`),
		PathSettings:    []byte(`{}`),
		ReadOnlyHint:    pgtype.Bool{},
		DestructiveHint: pgtype.Bool{},
		IdempotentHint:  pgtype.Bool{},
		OpenWorldHint:   pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = deployments_repo.New(ti.conn).CreateHTTPSecurity(ctx, deployments_repo.CreateHTTPSecurityParams{
		Key:                 "test_api_key",
		DeploymentID:        deploymentID,
		ProjectID:           uuid.NullUUID{UUID: projectID, Valid: true},
		Openapiv3DocumentID: uuid.NullUUID{},
		Type:                pgtype.Text{String: "apiKey", Valid: true},
		Name:                pgtype.Text{String: "X-Api-Key", Valid: true},
		InPlacement:         pgtype.Text{String: "header", Valid: true},
		Scheme:              pgtype.Text{},
		BearerFormat:        pgtype.Text{},
		EnvVariables:        []string{envVarName},
		OauthTypes:          nil,
		OauthFlows:          nil,
	})
	require.NoError(t, err)

	_, err = toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolsetID,
		Version:       1,
		ToolUrns:      []urn.Tool{toolURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	return envVarName
}

// addToolWithDualSecurity creates a deployment with an HTTP tool that accepts
// EITHER an apiKey OR an oauth2 access token. This exercises the
// anySchemeSatisfied logic where multiple alternative schemes exist.
// Returns the deployment ID so callers can reference it.
func (ti *testInstance) addToolWithDualSecurity(ctx context.Context, t *testing.T, toolsetID uuid.UUID, projectID uuid.UUID, orgID string) uuid.UUID {
	t.Helper()

	deploymentID, err := deployments_repo.New(ti.conn).InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         "test-user",
		IdempotencyKey: uuid.New().String(),
	})
	require.NoError(t, err)

	err = deployments_repo.New(ti.conn).CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	})
	require.NoError(t, err)

	// Tool security: either "test_api_key" OR "test_oauth" can satisfy.
	toolURN := urn.NewTool(urn.ToolKindHTTP, "dual-sec", uuid.New().String()[:8])
	err = tools_repo.New(ti.conn).CreateHTTPToolDefinition(ctx, tools_repo.CreateHTTPToolDefinitionParams{
		ProjectID:       projectID,
		DeploymentID:    deploymentID,
		ToolUrn:         toolURN,
		Name:            "dual_sec_tool",
		UntruncatedName: pgtype.Text{},
		Summary:         "Dual security tool",
		Description:     "A tool with apiKey and oauth2 security",
		Tags:            []string{},
		HttpMethod:      "GET",
		Path:            "/dual",
		SchemaVersion:   "3.0.0",
		Schema:          []byte(`{}`),
		ServerEnvVar:    "TEST_SERVER_URL",
		Security:        []byte(`[{"test_api_key": []}, {"test_oauth": []}]`),
		HeaderSettings:  []byte(`{}`),
		QuerySettings:   []byte(`{}`),
		PathSettings:    []byte(`{}`),
		ReadOnlyHint:    pgtype.Bool{},
		DestructiveHint: pgtype.Bool{},
		IdempotentHint:  pgtype.Bool{},
		OpenWorldHint:   pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = deployments_repo.New(ti.conn).CreateHTTPSecurity(ctx, deployments_repo.CreateHTTPSecurityParams{
		Key:                 "test_api_key",
		DeploymentID:        deploymentID,
		ProjectID:           uuid.NullUUID{UUID: projectID, Valid: true},
		Openapiv3DocumentID: uuid.NullUUID{},
		Type:                pgtype.Text{String: "apiKey", Valid: true},
		Name:                pgtype.Text{String: "X-Api-Key", Valid: true},
		InPlacement:         pgtype.Text{String: "header", Valid: true},
		Scheme:              pgtype.Text{},
		BearerFormat:        pgtype.Text{},
		EnvVariables:        []string{"TEST_API_KEY"},
		OauthTypes:          nil,
		OauthFlows:          nil,
	})
	require.NoError(t, err)

	// oauth2: name and in_placement are nullable for this type.
	_, err = deployments_repo.New(ti.conn).CreateHTTPSecurity(ctx, deployments_repo.CreateHTTPSecurityParams{
		Key:                 "test_oauth",
		DeploymentID:        deploymentID,
		ProjectID:           uuid.NullUUID{UUID: projectID, Valid: true},
		Openapiv3DocumentID: uuid.NullUUID{},
		Type:                pgtype.Text{String: "oauth2", Valid: true},
		Name:                pgtype.Text{},
		InPlacement:         pgtype.Text{},
		Scheme:              pgtype.Text{},
		BearerFormat:        pgtype.Text{},
		EnvVariables:        []string{"TEST_OAUTH_ACCESS_TOKEN"},
		OauthTypes:          nil,
		OauthFlows:          nil,
	})
	require.NoError(t, err)

	_, err = toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolsetID,
		Version:       1,
		ToolUrns:      []urn.Tool{toolURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	return deploymentID
}
