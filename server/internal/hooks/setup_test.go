package hooks

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/conv"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	spendcelenv "github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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
	service         *Service
	conn            *pgxpool.Pool
	chConn          clickhouse.Conn
	redisClient     *redis.Client
	spendGateCache  cache.Cache
	sessionManager  *sessions.Manager
	assetStorage    assets.BlobStore
	efficacySignals *recordingEfficacySignaler
	identitySignals *recordingIdentityMapSignaler
}

// recordingIdentityMapSignaler captures identity map refresh requests emitted
// after attributed account-link writes. Called synchronously by the producer,
// so a test reads the count straight after the call under test.
type recordingIdentityMapSignaler struct {
	mu    sync.Mutex
	count int
}

func (r *recordingIdentityMapSignaler) SignalIdentityMapRefresh(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
	return nil
}

func (r *recordingIdentityMapSignaler) refreshCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// recordingEfficacySignaler captures the skill efficacy wakes a hook path
// emits, and can be made to fail so tests can prove a failed wake never
// reaches the hook response. Signal is called synchronously by the producers,
// so a test reads it straight after the call under test.
type recordingEfficacySignaler struct {
	mu      sync.Mutex
	err     error
	signals []uuid.UUID
}

func (r *recordingEfficacySignaler) Signal(_ context.Context, projectID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signals = append(r.signals, projectID)
	return r.err
}

func (r *recordingEfficacySignaler) failWith(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *recordingEfficacySignaler) signaled() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.signals)
}

// namespacedSpendGateCache keeps snapshots isolated even though hook tests use
// the same Redis database and organization fixture in parallel.
type namespacedSpendGateCache struct {
	cache.Cache
	namespace string
}

func (c *namespacedSpendGateCache) key(key string) string {
	return c.namespace + ":" + key
}

func (c *namespacedSpendGateCache) Get(ctx context.Context, key string, value any) error {
	if err := c.Cache.Get(ctx, c.key(key), value); err != nil {
		return fmt.Errorf("get namespaced spend gate cache: %w", err)
	}
	return nil
}

func (c *namespacedSpendGateCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if err := c.Cache.Set(ctx, c.key(key), value, ttl); err != nil {
		return fmt.Errorf("set namespaced spend gate cache: %w", err)
	}
	return nil
}

func (c *namespacedSpendGateCache) Delete(ctx context.Context, key string) error {
	if err := c.Cache.Delete(ctx, c.key(key)); err != nil {
		return fmt.Errorf("delete namespaced spend gate cache: %w", err)
	}
	return nil
}

func (c *namespacedSpendGateCache) DeleteByPrefix(ctx context.Context, prefix string) error {
	if err := c.Cache.DeleteByPrefix(ctx, c.key(prefix)); err != nil {
		return fmt.Errorf("delete namespaced spend gate cache by prefix: %w", err)
	}
	return nil
}

func newTestHooksService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	spendGateCache := &namespacedSpendGateCache{
		Cache:     cacheAdapter,
		namespace: "hooks-spend-gate-test:" + uuid.NewString(),
	}
	t.Cleanup(func() {
		require.NoError(t, spendGateCache.DeleteByPrefix(context.Background(), ""))
	})

	// Pass nil for telemetry logger, temporalEnv, productFeatures, and chatTitleGenerator in tests
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	assetStorage := assetstest.NewTestBlobStore(t)
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, assetStorage)
	t.Cleanup(func() { _ = chatWriterShutdown(t.Context()) })
	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	serverURL, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	efficacySignals := &recordingEfficacySignaler{mu: sync.Mutex{}, err: nil, signals: nil}
	identitySignals := &recordingIdentityMapSignaler{mu: sync.Mutex{}, count: 0}
	shadowMCPClient := shadowmcp.NewClient(logger, conn, cacheAdapter, serverURL)
	policyBypass := risk.NewPolicyBypassEvaluator(logger, conn)
	spendCelEngine, err := spendcelenv.New()
	require.NoError(t, err)
	spendGate, err := spendrules.NewGate(logger, spendGateCache, spendCelEngine)
	require.NoError(t, err)
	svc := NewService(
		logger,
		conn,
		tracerProvider,
		meterProvider,
		nil,
		gcp.NewNoopPublisher[*otelv1.InboundLogRecord](),
		sessionManager,
		cacheAdapter,
		nil,
		nil,
		authzEngine,
		audit.NewLogger(),
		nil,
		nil,
		nil,
		nil,
		policyBypass,
		spendGate,
		nil,
		shadowMCPClient,
		chatWriter,
		efficacySignals,
		nil,
		identitySignals,
		serverURL,
		siteURL,
		"test-jwt-secret",
	)

	return ctx, &testInstance{
		service:         svc,
		conn:            conn,
		chConn:          chConn,
		redisClient:     redisClient,
		spendGateCache:  spendGateCache,
		sessionManager:  sessionManager,
		assetStorage:    assetStorage,
		efficacySignals: efficacySignals,
		identitySignals: identitySignals,
	}
}

func seedHookUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, userID string, email string) {
	t.Helper()

	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: email,
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)

	err = organizationsrepo.New(conn).AttachWorkOSUserToOrg(ctx, organizationsrepo.AttachWorkOSUserToOrgParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: pgtype.Text{},
	})
	require.NoError(t, err)
}
