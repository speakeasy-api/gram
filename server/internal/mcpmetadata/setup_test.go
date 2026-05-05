package mcpmetadata_test

import (
	"context"
	"log"
	"net/url"
	"os"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

var (
	infra *testenv.Environment
	// redisDBCounter assigns each parallel test its own Redis DB to prevent
	// cache key collisions between concurrent test auth setups.
	redisDBCounter atomic.Int32
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
	service        *mcpmetadata.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
	serverURL      *url.URL
	toolsetRepo    *toolsets_repo.Queries
}

func newTestMCPMetadataService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "mcpmetadatatest")
	require.NoError(t, err)

	redisDB := int(redisDBCounter.Add(1) % 16)
	redisClient, err := infra.NewRedisClient(t, redisDB)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-test"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	serverURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	siteURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	auditLogger := audit.NewLogger()

	svc := mcpmetadata.NewService(logger, tracerProvider, conn, sessionManager, serverURL, siteURL, cacheAdapter, authz.NewEngine(logger, conn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache), auditLogger)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		serverURL:      serverURL,
		toolsetRepo:    toolsets_repo.New(conn),
	}
}
