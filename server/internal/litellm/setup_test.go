package litellm

import (
	"context"
	"log"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/litellm/callcache"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	spendcelenv "github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var testInfra *testenv.Environment

type captureEnabledFeatures struct{}

func (captureEnabledFeatures) IsFeatureEnabled(_ context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	return feature == productfeatures.FeatureSessionCapture, nil
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
	service  *Service
	hooks    *hooks.Service
	conn     *pgxpool.Pool
	observer *recordingMessageObserver
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
		scanner,
		risk.NewPolicyBypassEvaluator(logger, conn),
		spendGate,
		shadowmcp.NewClient(logger, conn, cacheAdapter, serverURL),
		chatWriter,
		nil,
		nil,
		serverURL,
		siteURL,
		"test-jwt-secret",
	)
	service := NewService(logger, tracerProvider, conn, sessionManager, authzEngine, hookService, callcache.New(cacheAdapter))
	return ctx, &realTestInstance{
		service:  service,
		hooks:    hookService,
		conn:     conn,
		observer: observer,
	}
}
