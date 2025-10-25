package mcpmetadata_test

import (
	"context"
	"log"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

var (
	infra *testenv.Environment
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

	conn, err := infra.CloneTestDatabase(t, "mcpmetadatatest")
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

	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	svc := mcpmetadata.NewService(logger, conn, sessionManager, serverURL, siteURL, cacheAdapter)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		serverURL:      serverURL,
		toolsetRepo:    toolsets_repo.New(conn),
	}
}
