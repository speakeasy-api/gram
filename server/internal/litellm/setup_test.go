package litellm

import (
	"context"
	"log"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	hookpolicies "github.com/speakeasy-api/gram/server/internal/hooks/policies"
	keysservice "github.com/speakeasy-api/gram/server/internal/keys"
	"github.com/speakeasy-api/gram/server/internal/litellm/callcache"
	"github.com/speakeasy-api/gram/server/internal/litellm/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	spendcelenv "github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var testInfra *testenv.Environment

type captureEnabledFeatures struct{}

func (captureEnabledFeatures) IsFeatureEnabled(_ context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	return feature == productfeatures.FeatureSessionCapture, nil
}

type testProductFeatures struct {
	enabled bool
}

func (f *testProductFeatures) IsFeatureEnabled(_ context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	return feature == productfeatures.FeatureAIPlatformPushIntegrations && f.enabled, nil
}

func TestMain(m *testing.M) {
	infra, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	testInfra = infra
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("clean up test infrastructure: %v", err)
	}
	os.Exit(code)
}

type realTestInstance struct {
	service   *Service
	hooks     *hooks.Service
	conn      *pgxpool.Pool
	chConn    clickhouse.Conn
	telemetry *telemetry.Logger
	observer  *recordingMessageObserver
	features  *testProductFeatures
	keys      *keysservice.Service
}

type recordingMessageObserver struct {
	mu       sync.Mutex
	projects []uuid.UUID
}

func (r *recordingMessageObserver) OnMessagesStored(_ context.Context, projectID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects = append(r.projects, projectID)
}

func (r *recordingMessageObserver) count(projectID uuid.UUID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, observed := range r.projects {
		if observed == projectID {
			count++
		}
	}
	return count
}

// newTestPolicyRunner mirrors the cmd registration block (newHookPolicyRunner
// in cmd/gram/hook_policies.go): tests cannot import package gram's cmd, so
// the same policies are registered here in the same pinned order from the
// same Enforcer method values. Keep the two blocks in sync.
func newTestPolicyRunner(logger *slog.Logger, enforcer *hooks.Enforcer) *agenthooks.Runner {
	r := agenthooks.New(agenthooks.WithLogger(logger.With(attr.SlogComponent("hooks"))))

	r.Use(hookpolicies.ActorResolution)

	r.OnPromptSubmitted(
		hookpolicies.SpendGatePrompt(enforcer.CheckSpend),
		hookpolicies.RiskScanPromptGate(enforcer.ScanPrompt, enforcer.WarnAcknowledged, enforcer.WarnDenyReason),
	)
	r.OnToolPre(
		hookpolicies.SpendGateToolPre(enforcer.CheckSpend, enforcer.AppendBlockPageURL),
		hookpolicies.RiskScanToolPreGate(enforcer.ScanToolRequest, enforcer.ScanMCPToolRequest, enforcer.AppendBlockPageURL, enforcer.WarnAcknowledged, enforcer.WarnDenyReason),
		hookpolicies.ShadowMCPToolPreGate(enforcer.EvaluateShadowMCP),
	)
	r.OnPermission(
		hookpolicies.SpendGatePermission(enforcer.CheckSpend, enforcer.AppendBlockPageURL),
		hookpolicies.RiskScanPermissionGate(enforcer.ScanPermissionRequest, enforcer.AppendBlockPageURL, enforcer.WarnAcknowledged, enforcer.WarnDenyReason),
		hookpolicies.RiskScanPermissionToolGate(enforcer.ScanToolRequest, enforcer.ScanMCPToolRequest, enforcer.AppendBlockPageURL, enforcer.WarnAcknowledged, enforcer.WarnDenyReason),
		hookpolicies.ShadowMCPPermissionGate(enforcer.EvaluateShadowMCP),
	)

	return r
}

func newRealTestService(t *testing.T, scanner risk.RiskScanner) (context.Context, *realTestInstance) {
	t.Helper()
	return newRealTestServiceWithScannerFactory(t, func(*pgxpool.Pool) risk.RiskScanner { return scanner })
}

func newRealTestServiceWithScannerFactory(t *testing.T, scannerFactory func(*pgxpool.Pool) risk.RiskScanner) (context.Context, *realTestInstance) {
	t.Helper()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	conn, err := testInfra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	scanner := scannerFactory(conn)
	redisClient, err := testInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("litellm-test-"+uuid.NewString()), billingClient)
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	chConn, err := testInfra.NewClickhouseClient(t)
	require.NoError(t, err)
	telemetryLogger := telemetry.NewLogger(
		ctx,
		logger,
		tracerProvider,
		meterProvider,
		chConn,
		func(context.Context, string) (bool, error) { return true, nil },
		func(context.Context, string) (bool, error) { return false, nil },
		telemetry.NewUserInfoResolver(logger, conn, cache.NewRedisCacheAdapter(redisClient)),
		telemetry.NewNoopLogPublisher(logger),
	)
	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	assetStorage := assetstest.NewTestBlobStore(t)
	chatWriter, shutdownWriter := chat.NewChatMessageWriter(logger, conn, assetStorage)
	t.Cleanup(func() { require.NoError(t, shutdownWriter(t.Context())) })
	observer := &recordingMessageObserver{mu: sync.Mutex{}, projects: nil}
	chatWriter.AddObserver(observer)
	serverURL, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	spendEngine, err := spendcelenv.New()
	require.NoError(t, err)
	spendGate, err := spendrules.NewGate(logger, cacheAdapter, spendEngine)
	require.NoError(t, err)
	hooksEnforcer := hooks.NewEnforcer(
		logger,
		conn,
		cacheAdapter,
		scanner,
		risk.NewPolicyBypassEvaluator(logger, conn),
		spendGate,
		shadowmcp.NewClient(logger, conn, cacheAdapter, serverURL),
		siteURL,
		"test-jwt-secret",
	)
	hookService := hooks.NewService(
		logger,
		conn,
		tracerProvider,
		meterProvider,
		nil,
		sessionManager,
		cacheAdapter,
		nil,
		nil,
		authzEngine,
		captureEnabledFeatures{},
		nil,
		chatWriter,
		nil,
		nil,
		serverURL,
		hooksEnforcer,
		newTestPolicyRunner(logger, hooksEnforcer),
	)
	calls := callcache.New(cacheAdapter)
	traceProcessor := NewTraceProcessor(logger, meterProvider, telemetryLogger, calls)
	metricProcessor := NewMetricProcessor(logger, meterProvider, telemetryLogger)
	healthProcessor := NewHealthProcessor(logger, conn)
	instanceResolver := NewInstanceResolver(logger, conn)
	traceProcessor.SetInstanceResolver(instanceResolver)
	metricProcessor.SetInstanceResolver(instanceResolver)
	traceProcessor.Start(ctx)
	metricProcessor.Start(ctx)
	healthProcessor.Start(ctx)
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, traceProcessor.Shutdown(shutdownCtx))
		require.NoError(t, metricProcessor.Shutdown(shutdownCtx))
		require.NoError(t, healthProcessor.Shutdown(shutdownCtx))
	})
	features := &testProductFeatures{enabled: false}
	auditLogger := audit.NewLogger()
	service := NewService(logger, tracerProvider, conn, chConn, sessionManager, authzEngine, hookService, calls, traceProcessor, metricProcessor, healthProcessor, instanceResolver, features, auditLogger, "local")
	return ctx, &realTestInstance{
		service:   service,
		hooks:     hookService,
		conn:      conn,
		chConn:    chConn,
		telemetry: telemetryLogger,
		observer:  observer,
		features:  features,
		keys:      keysservice.NewService(logger, tracerProvider, conn, sessionManager, "local", authzEngine, auditLogger),
	}
}

func newDisabledHealthProcessor(t *testing.T) *HealthProcessor {
	t.Helper()
	return newHealthProcessor(testenv.NewLogger(t), time.Hour, func(context.Context, repo.RecordLiteLLMInstanceHealthParams) error { return nil })
}
