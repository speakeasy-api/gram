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
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
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

	enc := testenv.NewEncryptionClient(t)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)

	svc := xmcp.NewService(logger, tracerProvider, meterProvider, conn, sessionManager, enc, authzEngine, guardianPolicy, billingClient, billingClient)

	return ctx, &testInstance{
		service:        svc,
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

// runHandler invokes the xmcp handler against a custom method/path with chi
// URL params populated.
func runHandler(t *testing.T, ctx context.Context, ti *testInstance, method, serverID, authorization string, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	mux := chi.NewMux()
	mux.MethodFunc(method, xmcp.RuntimePath, oops.ErrHandle(ti.logger, ti.service.ServeMCP).ServeHTTP)

	req := httptest.NewRequestWithContext(ctx, method, "/x/mcp/"+serverID, bytes.NewReader(body))
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
