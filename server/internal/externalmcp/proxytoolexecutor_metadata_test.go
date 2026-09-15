package externalmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestProxyToolMetadataClearsPlaceholderSchema(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	toolURN := urn.NewTool(urn.ToolKindExternalMCP, "upstream", "proxy")
	placeholder := &ToolCallPlan{ToolName: "proxy", InputSchema: json.RawMessage(`{}`), Slug: "upstream"}
	executor := &ProxyToolExecutor{entries: []ProxyToolEntry{{SourceSlug: "upstream", URN: toolURN}}}
	plan, err := executor.MatchPlanInputs(t.Context(), "upstream--search--nested", projectID, func(_ context.Context, gotURN urn.Tool, gotProjectID uuid.UUID) (*ToolCallPlan, error) {
		require.Equal(t, toolURN, gotURN)
		require.Equal(t, projectID, gotProjectID)
		return placeholder, nil
	})
	require.NoError(t, err)
	require.Equal(t, "search--nested", plan.ToolName)
	require.Nil(t, plan.InputSchema)
	require.Equal(t, "proxy", placeholder.ToolName, "resolver-owned plans must not be mutated")
	require.JSONEq(t, `{}`, string(placeholder.InputSchema))
}

func TestProxyToolMetadataLiveListDoesNotSeedSeparateClient(t *testing.T) {
	t.Parallel()
	const originalName = "search--nested"
	const schema = `{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"Owner"}}}`
	var listCalls atomic.Int32
	callHeaders := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(request.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-11-25",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "metadata-test", "version": "1.0.0"},
			}
		case "tools/list":
			listCalls.Add(1)
			result = map[string]any{"tools": []any{map[string]any{"name": originalName, "inputSchema": json.RawMessage(schema)}}}
		case "tools/call":
			callHeaders <- r.Header.Clone()
			result = map[string]any{"content": []any{}}
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer server.Close()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	projectID := uuid.New()
	toolURN := urn.NewTool(urn.ToolKindExternalMCP, "upstream", "proxy")
	plan := &ToolCallPlan{RemoteURL: server.URL, Slug: "upstream", RequiresOAuth: true, TransportType: types.TransportTypeStreamableHTTP}
	executor := &ProxyToolExecutor{
		logger:         testenv.NewLogger(t),
		guardianPolicy: policy,
		entries:        []ProxyToolEntry{{SourceSlug: "upstream", URN: toolURN}},
	}
	emptyEnv := toolconfig.NewCaseInsensitiveEnv()
	tools, err := executor.DoList(t.Context(), projectID, emptyEnv, "test-token",
		func(context.Context, urn.Tool) (*toolconfig.CaseInsensitiveEnv, error) { return emptyEnv, nil },
		func(context.Context, urn.Tool, uuid.UUID) (*ToolCallPlan, error) { return plan, nil },
	)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.Equal(t, "upstream--"+originalName, tools[0].Name)
	require.JSONEq(t, schema, string(tools[0].Schema))
	opts := &ClientOptions{Headers: BuildHeaders(emptyEnv, emptyEnv, nil, "test-token")}
	client, err := NewClient(t.Context(), executor.logger, policy, server.URL, plan.TransportType, opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	// Proxy plans deliberately have no schema. A live listing from another
	// client must not affect the first call or cause a preliminary listing.
	_, err = client.CallTool(t.Context(), originalName, json.RawMessage(`{"owner":"example"}`), nil)
	require.NoError(t, err)
	require.Empty(t, (<-callHeaders).Get("Mcp-Param-Owner"))
	require.EqualValues(t, 1, listCalls.Load())
}
