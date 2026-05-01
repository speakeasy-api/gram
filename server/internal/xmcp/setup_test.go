package xmcp_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpmetadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

var (
	infra *testenv.Environment
	funcs functions.ToolCaller
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true, Temporal: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	service        *xmcp.Service
	mcpService     *mcp.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
	logger         *slog.Logger
	enc            *encryption.Client
	authzEngine    *authz.Engine
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "xmcptest")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-xmcp-test"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	serverURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	authzEngine := authz.NewEngine(logger, conn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	mcpMetadataRepo := mcpmetadatarepo.New(conn)
	env := environments.NewEnvironmentEntries(logger, conn, enc, mcpMetadataRepo)
	posthogClient := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	oauthService := oauth.NewService(logger, tracerProvider, meterProvider, conn, serverURL, cacheAdapter, enc, env, sessionManager, guardianPolicy)
	devProvisioner := openrouter.NewDevelopment("test-openrouter-key")
	chatClient := openrouter.NewUnifiedClient(logger, guardianPolicy, devProvisioner, nil, nil, nil, nil, nil)
	vectorToolStore := rag.NewToolsetVectorStore(logger, tracerProvider, conn, chatClient)
	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")
	featClient := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	logsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	toolIOLogsEnabled := func(_ context.Context, _ string) (bool, error) { return false, nil }
	sessionCaptureEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	telemLogger := telemetry.NewLogger(ctx, logger, chConn, logsEnabled, toolIOLogsEnabled)
	telemService := telemetry.NewService(logger, tracerProvider, conn, chConn, sessionManager, chatSessionsManager, logsEnabled, sessionCaptureEnabled, posthogClient, authzEngine)

	temporalEnv, _ := infra.NewTemporalEnv(t)

	assistantTokens := assistanttokens.New("test-jwt-secret", conn, authzEngine)
	mcpService := mcp.NewService(logger, tracerProvider, meterProvider, conn, sessionManager, chatSessionsManager, env, posthogClient, serverURL, enc, cacheAdapter, guardianPolicy, funcs, oauthService, billingClient, billingClient, telemLogger, telemService, featClient, vectorToolStore, nil, temporalEnv, authzEngine, assistantTokens)

	svc := xmcp.NewService(logger, tracerProvider, meterProvider, conn, enc, authzEngine, guardianPolicy, billingClient, billingClient, mcpService)

	return ctx, &testInstance{
		service:        svc,
		mcpService:     mcpService,
		conn:           conn,
		sessionManager: sessionManager,
		logger:         logger,
		enc:            enc,
		authzEngine:    authzEngine,
	}
}

// seedAPIKey inserts a new API key row for the given project and returns the
// raw key the client should send in Authorization. The key is stored using
// the same SHA-256 hash the auth layer expects.
func seedAPIKey(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, userID string, projectID *uuid.UUID, scopes []string) string {
	t.Helper()

	raw := make([]byte, 24)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	fullKey := "gram_test_" + hex.EncodeToString(raw)

	hash, err := auth.GetAPIKeyHash(fullKey)
	require.NoError(t, err)

	var pgProjectID uuid.NullUUID
	if projectID != nil {
		pgProjectID = uuid.NullUUID{UUID: *projectID, Valid: true}
	}

	_, err = keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  organizationID,
		ProjectID:       pgProjectID,
		CreatedByUserID: userID,
		Name:            "xmcp-test-" + uuid.NewString()[:8],
		KeyPrefix:       fullKey[:10],
		KeyHash:         hash,
		Scopes:          scopes,
	})
	require.NoError(t, err)

	return fullKey
}

// seedRemoteMCPServer inserts a new remote_mcp_servers row and any configured
// headers, encrypting secret values the same way the management API does.
func seedRemoteMCPServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, url string, headers ...remotemcprepo.CreateHeaderParams) remotemcprepo.RemoteMcpServer {
	t.Helper()

	r := remotemcprepo.New(ti.conn)
	server, err := r.CreateServer(ctx, remotemcprepo.CreateServerParams{
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           url,
	})
	require.NoError(t, err)

	for _, h := range headers {
		params := h
		params.RemoteMcpServerID = server.ID

		if params.IsSecret && params.Value.Valid && params.Value.String != "" {
			encrypted, encErr := ti.enc.Encrypt([]byte(params.Value.String))
			require.NoError(t, encErr)
			params.Value = pgtype.Text{String: encrypted, Valid: true}
		}

		_, err = r.CreateHeader(ctx, params)
		require.NoError(t, err)
	}

	return server
}

// randomSlug returns a unique mcp_endpoints.slug suitable for parallel
// tests. Full UUID entropy keeps collisions out of birthday range even
// at high parallelism.
func randomSlug() string {
	return "xmcp-test-" + uuid.NewString()
}

// seedRemoteMCPEndpoint wires up a full /x/mcp/{slug} resolution chain for a
// remote-backed mcp_server: a remote_mcp_servers row + an mcp_servers row
// pointing at it + an mcp_endpoints row exposing it via the returned slug.
// visibility must be "public", "private", or "disabled".
func seedRemoteMCPEndpoint(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, upstreamURL, visibility string, headers ...remotemcprepo.CreateHeaderParams) (slug string, mcpServer mcpserversrepo.McpServer, remoteServer remotemcprepo.RemoteMcpServer) {
	t.Helper()

	remoteServer = seedRemoteMCPServer(t, ctx, ti, projectID, upstreamURL, headers...)
	mcpServer, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ProjectID:             projectID,
		EnvironmentID:         uuid.NullUUID{},
		ExternalOauthServerID: uuid.NullUUID{},
		OauthProxyServerID:    uuid.NullUUID{},
		RemoteMcpServerID:     uuid.NullUUID{UUID: remoteServer.ID, Valid: true},
		ToolsetID:             uuid.NullUUID{},
		Visibility:            visibility,
	})
	require.NoError(t, err)

	slug = randomSlug()
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: uuid.NullUUID{},
		McpServerID:    mcpServer.ID,
		Slug:           slug,
	})
	require.NoError(t, err)

	return slug, mcpServer, remoteServer
}

// seedExternalOAuthServer inserts a minimal external_oauth_server_metadata
// row in the given project so that an mcp_server can reference it via
// external_oauth_server_id.
func seedExternalOAuthServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	row, err := oauthrepo.New(ti.conn).CreateExternalOAuthServerMetadata(ctx, oauthrepo.CreateExternalOAuthServerMetadataParams{
		ProjectID: projectID,
		Slug:      "ext-" + uuid.NewString()[:8],
		Metadata:  []byte(`{}`),
	})
	require.NoError(t, err)
	return row.ID
}

// seedRemoteMCPEndpointWithExternalOAuth wires up the same chain as
// seedRemoteMCPEndpoint but additionally attaches an
// external_oauth_server_id to the mcp_server, exercising the public +
// external-OAuth runtime path where the caller's Authorization header is
// expected to be forwarded to the upstream MCP server.
func seedRemoteMCPEndpointWithExternalOAuth(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, upstreamURL string) (slug string) {
	t.Helper()

	remoteServer := seedRemoteMCPServer(t, ctx, ti, projectID, upstreamURL)
	externalOAuthID := seedExternalOAuthServer(t, ctx, ti, projectID)

	mcpServer, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ProjectID:             projectID,
		EnvironmentID:         uuid.NullUUID{},
		ExternalOauthServerID: uuid.NullUUID{UUID: externalOAuthID, Valid: true},
		OauthProxyServerID:    uuid.NullUUID{},
		RemoteMcpServerID:     uuid.NullUUID{UUID: remoteServer.ID, Valid: true},
		ToolsetID:             uuid.NullUUID{},
		Visibility:            "public",
	})
	require.NoError(t, err)

	slug = randomSlug()
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: uuid.NullUUID{},
		McpServerID:    mcpServer.ID,
		Slug:           slug,
	})
	require.NoError(t, err)

	return slug
}

// seedOAuthProxyServer inserts a minimal oauth_proxy_servers row in the
// given project so that an mcp_server can reference it via
// oauth_proxy_server_id.
func seedOAuthProxyServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	row, err := oauthrepo.New(ti.conn).UpsertOAuthProxyServer(ctx, oauthrepo.UpsertOAuthProxyServerParams{
		ProjectID: projectID,
		Slug:      "proxy-" + uuid.NewString()[:8],
		Audience:  pgtype.Text{String: "https://example.invalid", Valid: true},
	})
	require.NoError(t, err)
	return row.ID
}

// seedRemoteMCPEndpointWithOAuthProxy wires up a remote-backed mcp_server
// configured for the OAuth-proxy token-swap flow. The proxy resolution
// is currently stubbed in mcp.Service.ResolveOAuthProxyUpstreamToken
// (returns "", nil), so this seeding is enough to drive the auth-switch
// branch in xmcp; once the resolver is implemented it will exercise the
// full token-swap path.
func seedRemoteMCPEndpointWithOAuthProxy(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, upstreamURL string) (slug string) {
	t.Helper()

	remoteServer := seedRemoteMCPServer(t, ctx, ti, projectID, upstreamURL)
	oauthProxyServerID := seedOAuthProxyServer(t, ctx, ti, projectID)

	mcpServer, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ProjectID:             projectID,
		EnvironmentID:         uuid.NullUUID{},
		ExternalOauthServerID: uuid.NullUUID{},
		OauthProxyServerID:    uuid.NullUUID{UUID: oauthProxyServerID, Valid: true},
		RemoteMcpServerID:     uuid.NullUUID{UUID: remoteServer.ID, Valid: true},
		ToolsetID:             uuid.NullUUID{},
		Visibility:            "public",
	})
	require.NoError(t, err)

	slug = randomSlug()
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: uuid.NullUUID{},
		McpServerID:    mcpServer.ID,
		Slug:           slug,
	})
	require.NoError(t, err)

	return slug
}

// runHandler invokes the xmcp handler against a custom method/path with chi
// URL params populated.
func runHandler(t *testing.T, ctx context.Context, ti *testInstance, method, slug, authorization string, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	mux := chi.NewMux()
	mux.MethodFunc(method, xmcp.RuntimePath, oops.ErrHandle(ti.logger, ti.service.ServeMCP).ServeHTTP)

	req := httptest.NewRequestWithContext(ctx, method, "/x/mcp/"+slug, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// MCP Streamable HTTP § Sending Messages to the Server (step 2) requires
	// clients to list both application/json and text/event-stream on POST.
	req.Header.Set("Accept", "application/json, text/event-stream")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// bearer builds an Authorization header value for the given raw key.
func bearer(key string) string {
	return fmt.Sprintf("Bearer %s", key)
}
