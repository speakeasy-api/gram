package aiinsights_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/aiinsights"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/insights"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	"github.com/speakeasy-api/gram/server/internal/variations"
)

var infra *testenv.Environment

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

type testHarness struct {
	server         *httptest.Server
	sessionToken   string
	projectSlug    string
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
}

func newHarness(t *testing.T) (context.Context, *testHarness) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context should be set")
	require.NotNil(t, authCtx.SessionID, "session token should be set")
	require.NotNil(t, authCtx.ProjectSlug, "project slug should be set")

	authzEngine := authz.NewEngine(logger, conn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	variationsSvc := variations.NewService(logger, tracerProvider, conn, sessionManager, authzEngine)
	toolsetsSvc := toolsets.NewService(logger, tracerProvider, conn, sessionManager, cache.NoopCache, authzEngine)
	insightsSvc := insights.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, variationsSvc, toolsetsSvc)

	authHelper := auth.New(logger, conn, sessionManager, authzEngine)
	mcpServer := aiinsights.New(logger, authHelper, insightsSvc)

	mux := goahttp.NewMuxer()
	aiinsights.Attach(mux, mcpServer)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return ctx, &testHarness{
		server:         ts,
		sessionToken:   *authCtx.SessionID,
		projectSlug:    *authCtx.ProjectSlug,
		conn:           conn,
		sessionManager: sessionManager,
	}
}

func (h *testHarness) postJSONRPC(t *testing.T, body any) []byte {
	t.Helper()
	bs, err := json.Marshal(body)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, h.server.URL+"/mcp/ai-insights", bytes.NewReader(bs))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(constants.SessionHeader, h.sessionToken)
	req.Header.Set(constants.ProjectHeader, h.projectSlug)

	resp, err := h.server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "unexpected status: body=%s", string(respBody))
	return respBody
}

func TestAIInsightsMCP_Get_Returns405_NoAuth(t *testing.T) {
	t.Parallel()

	_, h := newHarness(t)

	// No Gram-Session, no Gram-Project, no cookies — GET should still
	// return 405, not 401/403. Some MCP clients probe with GET to detect
	// SSE support; an auth rejection here would confuse them.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, h.server.URL+"/mcp/ai-insights", nil)
	require.NoError(t, err)

	resp, err := h.server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestAIInsightsMCP_ToolsList_ReturnsSixTools(t *testing.T) {
	t.Parallel()

	_, h := newHarness(t)

	body := h.postJSONRPC(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]any{},
	})

	var env struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &env), "response body: %s", string(body))

	require.Equal(t, "2.0", env.JSONRPC)
	require.Equal(t, 1, env.ID)
	require.Len(t, env.Result.Tools, 6, "expected six tools")

	gotNames := make(map[string]bool)
	for _, tool := range env.Result.Tools {
		gotNames[tool.Name] = true
		require.NotEmpty(t, tool.Description, "tool %q should have a description", tool.Name)
	}
	expected := []string{
		"insights_propose_variation",
		"insights_propose_toolset_change",
		"insights_remember",
		"insights_forget",
		"insights_recall_memory",
		"insights_record_finding",
	}
	for _, name := range expected {
		require.True(t, gotNames[name], "tool %q should be in the list", name)
	}
}

func TestAIInsightsMCP_ToolsCall_Remember_Succeeds(t *testing.T) {
	t.Parallel()

	_, h := newHarness(t)

	body := h.postJSONRPC(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "call-1",
		"method":  "tools/call",
		"params": map[string]any{
			"name": "insights_remember",
			"arguments": map[string]any{
				"kind":    "fact",
				"content": "smoke-test fact content",
				"tags":    []string{"smoke", "test"},
			},
		},
	})

	var env struct {
		JSONRPC string `json:"jsonrpc"`
		Result  struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &env), "response body: %s", string(body))
	require.Equal(t, "2.0", env.JSONRPC)
	require.False(t, env.Result.IsError, "remember should not be an error: %s", string(body))
	require.NotEmpty(t, env.Result.Content)

	// The text content is a JSON-encoded MemoryResult. Verify it parses and
	// has an id.
	var mem struct {
		Memory struct {
			ID      string `json:"ID"`
			Kind    string `json:"Kind"`
			Content string `json:"Content"`
		} `json:"Memory"`
	}
	require.NoError(t, json.Unmarshal([]byte(env.Result.Content[0].Text), &mem), "inner text: %s", env.Result.Content[0].Text)
	require.NotEmpty(t, mem.Memory.ID, "memory should have an ID")
	require.Equal(t, "fact", mem.Memory.Kind)
	require.Equal(t, "smoke-test fact content", mem.Memory.Content)
}
