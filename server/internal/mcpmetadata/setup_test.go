package mcpmetadata_test

import (
	"context"
	"log"
	"net/url"
	"os"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpoints_repo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
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

	redisDB := int(redisDBCounter.Add(1) % 16)
	redisClient, err := infra.NewRedisClient(t, redisDB)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-test"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	serverURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	siteURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	auditLogger := audit.NewLogger()

	svc := mcpmetadata.NewService(logger, tracerProvider, conn, sessionManager, serverURL, siteURL, cacheAdapter, authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), auditLogger)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		serverURL:      serverURL,
		toolsetRepo:    toolsets_repo.New(conn),
	}
}

// mcpServerFixtureOptions tunes the fixture pair created by
// createMcpServerWithEndpoint. ToolsetID is non-Nil for toolset-backed
// mcp_servers (the dual-source bridge path); RemoteMcpServerID is non-Nil for
// Remote-MCP-backed installs.
type mcpServerFixtureOptions struct {
	name              string
	visibility        string
	endpointSlug      string
	toolsetID         uuid.NullUUID
	remoteMcpServerID uuid.NullUUID
	customDomainID    uuid.NullUUID
}

func createMcpServerWithEndpoint(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	opts mcpServerFixtureOptions,
) (mcpservers_repo.McpServer, mcpendpoints_repo.McpEndpoint) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	if opts.name == "" {
		opts.name = "Test MCP Server"
	}
	if opts.visibility == "" {
		opts.visibility = mcpservers.VisibilityPrivate
	}
	if opts.endpointSlug == "" {
		opts.endpointSlug = "test-endpoint-" + uuid.NewString()[:8]
	}

	// mcp_servers carries an XOR check on (toolset_id, remote_mcp_server_id);
	// when neither is supplied by the caller, default to a fresh toolset so
	// fixtures focused on the metadata flow don't need to spell out a backend.
	if !opts.toolsetID.Valid && !opts.remoteMcpServerID.Valid {
		toolset, err := toolsets_repo.New(ti.conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Fixture Toolset",
			Slug:                   "fixture-toolset-" + uuid.NewString()[:8],
			Description:            conv.ToPGText("Fixture toolset for mcp_server backend"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                pgtype.Text{String: "", Valid: false},
			McpEnabled:             false,
		})
		require.NoError(t, err)
		opts.toolsetID = uuid.NullUUID{UUID: toolset.ID, Valid: true}
	}

	id, err := uuid.NewV7()
	require.NoError(t, err)

	server, err := mcpservers_repo.New(ti.conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  id,
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText(opts.name),
		Slug:                conv.ToPGText("mcp-server-" + uuid.NewString()[:8]),
		EnvironmentID:       uuid.NullUUID{},
		UserSessionIssuerID: uuid.NullUUID{},
		RemoteMcpServerID:   opts.remoteMcpServerID,
		ToolsetID:           opts.toolsetID,
		Visibility:          opts.visibility,
	})
	require.NoError(t, err)

	endpoint, err := mcpendpoints_repo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpoints_repo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: opts.customDomainID,
		McpServerID:    server.ID,
		Slug:           opts.endpointSlug,
	})
	require.NoError(t, err)

	return server, endpoint
}
