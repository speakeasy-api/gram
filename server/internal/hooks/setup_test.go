package hooks

import (
	"context"
	"log"
	"net/url"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

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
	sessionManager  *sessions.Manager
	efficacySignals *recordingEfficacySignaler
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

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)

	// Pass nil for telemetry logger, temporalEnv, productFeatures, and chatTitleGenerator in tests
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, nil)
	t.Cleanup(func() { _ = chatWriterShutdown(t.Context()) })
	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	serverURL, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	efficacySignals := &recordingEfficacySignaler{mu: sync.Mutex{}, err: nil, signals: nil}
	shadowMCPClient := shadowmcp.NewClient(logger, conn, cacheAdapter, serverURL)
	policyBypass := risk.NewPolicyBypassEvaluator(logger, conn)
	svc := NewService(
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
		nil,
		nil,
		nil,
		policyBypass,
		shadowMCPClient,
		chatWriter,
		efficacySignals,
		serverURL,
		siteURL,
		"test-jwt-secret",
	)

	return ctx, &testInstance{
		service:         svc,
		conn:            conn,
		chConn:          chConn,
		redisClient:     redisClient,
		sessionManager:  sessionManager,
		efficacySignals: efficacySignals,
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
