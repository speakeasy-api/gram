package externalmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

const annotatedSchema = `{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"Account"},"secret":{"type":"string"}}}`
const plainSchema = `{"type":"object","properties":{"owner":{"type":"string"}}}`

type parameterRequest struct {
	Method string          `json:"method"`
	ID     json.RawMessage `json:"id"`
	Params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Cursor    string          `json:"cursor"`
	} `json:"params"`
	Headers http.Header `json:"-"`
}

type parameterServer struct {
	mu           sync.Mutex
	requests     []parameterRequest
	schema       string
	paginate     bool
	absent       bool
	repeatCursor bool
	legacy       bool
	isError      bool
	// A non-nil set enables an independent rejecting wire-level validator.
	wantParameters http.Header
	// reject provides explicit wire-level status/code; zero means success.
	reject func(int) (int, int)
	calls  int
}

func (s *parameterServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req parameterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	req.Headers = r.Header.Clone()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var result any
	switch req.Method {
	case "server/discover":
		if s.legacy {
			w.WriteHeader(404)
			return
		}
		result = map[string]any{"supportedVersions": []string{"2026-07-28"}, "capabilities": map[string]any{"tools": map[string]any{}}, "ttlMs": 600000}
	case "initialize":
		result = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "fixture", "version": "1"}}
	case "tools/list":
		name := "lookup"
		cursor := ""
		if s.paginate && req.Params.Cursor == "" {
			name = "first"
			cursor = "second"
		}
		if s.repeatCursor {
			cursor = "second"
		}
		tools := []any{}
		if !s.absent {
			tools = append(tools, map[string]any{"name": name, "inputSchema": json.RawMessage(s.schema)})
		}
		result = map[string]any{"tools": tools, "nextCursor": cursor, "ttlMs": 600000}
	case "tools/call":
		s.calls++
		if s.wantParameters != nil {
			actual := make(http.Header)
			for name, values := range req.Headers {
				if strings.HasPrefix(strings.ToLower(name), "mcp-param-") {
					actual[name] = values
				}
			}
			if !reflect.DeepEqual(actual, s.wantParameters) {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": mcp.CodeHeaderMismatch, "message": "parameter header mismatch"}})
				return
			}
		}
		if s.reject != nil {
			status, code := s.reject(s.calls)
			if status != 0 {
				w.WriteHeader(status)
				if code != 0 {
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": code, "message": "rejected parameter should-not-be-logged"}})
				}
				return
			}
		}
		result = map[string]any{"isError": s.isError, "content": []any{map[string]any{"type": "text", "text": "ok"}}}
	default:
		result = map[string]any{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

func (s *parameterServer) matching(method string) []parameterRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []parameterRequest
	for _, r := range s.requests {
		if r.Method == method {
			out = append(out, r)
		}
	}
	return out
}

func newParameterClient(t *testing.T, s *parameterServer, opts *ClientOptions) func() *Client {
	t.Helper()
	server := httptest.NewServer(s)
	t.Cleanup(server.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return func() *Client {
		c, err := NewClient(t.Context(), testenv.NewLogger(t), policy, server.URL, types.TransportTypeStreamableHTTP, opts)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
}

func TestParameterHeadersKnownSchema(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema, want string
		legacy             bool
	}{
		{"annotated", annotatedSchema, "sample/org", false},
		{"annotation-free", plainSchema, "", false},
		{"legacy", annotatedSchema, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &parameterServer{schema: tc.schema, legacy: tc.legacy}
			c := newParameterClient(t, s, &ClientOptions{Headers: map[string]string{"mCp-pArAm-Account": "configured", "Mcp-Param-Stale": "stale", "X-Config": "kept"}})()
			args := json.RawMessage(`{"owner":"sample/org","secret":"not-for-headers","large":9007199254740993,"decimal":1.0000000000000001}`)
			_, err := c.CallTool(t.Context(), "lookup", args, json.RawMessage(tc.schema))
			require.NoError(t, err)
			require.Empty(t, s.matching("tools/list"))
			calls := s.matching("tools/call")
			require.Len(t, calls, 1)
			require.Equal(t, tc.want, calls[0].Headers.Get("Mcp-Param-Account"))
			require.Empty(t, calls[0].Headers.Get("Mcp-Param-Stale"))
			require.Empty(t, calls[0].Headers.Get("Mcp-Param-Secret"))
			require.Equal(t, "kept", calls[0].Headers.Get("X-Config"))
			require.JSONEq(t, string(args), string(calls[0].Params.Arguments))
			require.Contains(t, string(calls[0].Params.Arguments), "9007199254740993")
			require.Contains(t, string(calls[0].Params.Arguments), "1.0000000000000001")
			for _, r := range s.matching("server/discover") {
				require.Empty(t, r.Headers.Get("Mcp-Param-Account"))
			}
		})
	}
}

func TestParameterHeadersDiscoveryAndIsolatedCache(t *testing.T) {
	t.Parallel()
	s := &parameterServer{schema: annotatedSchema, paginate: true}
	makeClient := newParameterClient(t, s, &ClientOptions{MetadataScope: t.Name(), Headers: map[string]string{"Authorization": "Bearer fixture"}})
	c := makeClient()
	tools, err := c.ListTools(t.Context())
	require.NoError(t, err)
	require.Len(t, tools, 2)
	require.JSONEq(t, annotatedSchema, string(tools[1].Schema))
	require.NoError(t, c.Close())
	_, err = makeClient().CallTool(t.Context(), "lookup", json.RawMessage(`{"owner":"fresh"}`))
	require.NoError(t, err)
	require.Len(t, s.matching("tools/list"), 2)
	require.Len(t, s.matching("server/discover"), 2)
	require.Equal(t, "fresh", s.matching("tools/call")[0].Headers.Get("Mcp-Param-Account"))
}

func TestParameterHeadersColdMiss(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		absent, cycle bool
	}{{"paginated", false, false}, {"absent", true, false}, {"cycle", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &parameterServer{schema: annotatedSchema, paginate: true, absent: tc.absent, repeatCursor: tc.cycle}
			c := newParameterClient(t, s, nil)()
			_, err := c.CallTool(t.Context(), "lookup", json.RawMessage(`{"owner":"fresh"}`))
			require.Len(t, s.matching("tools/list"), 2)
			if tc.absent {
				require.Error(t, err)
				require.Empty(t, s.matching("tools/call"))
				return
			}
			require.NoError(t, err)
			require.Equal(t, "fresh", s.matching("tools/call")[0].Headers.Get("Mcp-Param-Account"))
		})
	}
}

func TestParameterHeadersRecovery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema, want string
		prelist            bool
	}{
		{"changed", `{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"New"}}}`, "value", false},
		{"removed", plainSchema, "", false},
		{"sdk-ttl", plainSchema, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &parameterServer{schema: annotatedSchema, reject: func(n int) (int, int) {
				if n == 1 {
					return 400, -32020
				}
				return 0, 0
			}}
			makeClient := newParameterClient(t, s, &ClientOptions{MetadataScope: t.Name(), Headers: map[string]string{"Authorization": "Bearer fixture", "Mcp-Param-Account": "configured"}})
			c := makeClient()
			if tc.prelist {
				_, err := c.ListTools(t.Context())
				require.NoError(t, err)
				_, err = c.ListTools(t.Context())
				require.NoError(t, err)
				require.Len(t, s.matching("tools/list"), 1)
			}
			s.mu.Lock()
			s.schema = tc.schema
			s.mu.Unlock()
			args := json.RawMessage(`{"owner":"value","large":9007199254740993}`)
			_, err := c.CallTool(t.Context(), "lookup", args, json.RawMessage(annotatedSchema))
			require.NoError(t, err)
			wantLists := 1
			if tc.prelist {
				wantLists = 2
			}
			require.Len(t, s.matching("tools/list"), wantLists)
			require.Len(t, s.matching("server/discover"), 2)
			calls := s.matching("tools/call")
			require.Len(t, calls, 2)
			require.Equal(t, "value", calls[0].Headers.Get("Mcp-Param-Account"))
			require.Empty(t, calls[1].Headers.Get("Mcp-Param-Account"))
			require.Equal(t, tc.want, calls[1].Headers.Get("Mcp-Param-New"))
			for _, r := range calls {
				require.Equal(t, "Bearer fixture", r.Headers.Get("Authorization"))
				require.Equal(t, string(args), string(r.Params.Arguments))
			}
			// A later isolated call must prefer refreshed metadata over a stale persisted plan.
			_, err = makeClient().CallTool(t.Context(), "lookup", args, json.RawMessage(annotatedSchema))
			require.NoError(t, err)
			require.Len(t, s.matching("tools/list"), wantLists)
			calls = s.matching("tools/call")
			require.Len(t, calls, 3)
			require.Empty(t, calls[2].Headers.Get("Mcp-Param-Account"))
			require.Equal(t, tc.want, calls[2].Headers.Get("Mcp-Param-New"))
		})
	}
}

func TestParameterHeadersRetryBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                               string
		status, code, wantCalls, wantLists int
	}{
		{"mismatch-exhausted", 400, -32020, 2, 1},
		{"other-jsonrpc", 400, -32602, 1, 0},
		{"plain-400", 400, 0, 1, 0},
		{"ambiguous-503", 503, 0, 1, 0},
		{"jsonrpc-200", 200, -32603, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &parameterServer{schema: annotatedSchema, reject: func(int) (int, int) { return tc.status, tc.code }}
			c := newParameterClient(t, s, nil)()
			_, err := c.CallTool(t.Context(), "lookup", json.RawMessage(`{"owner":"value"}`), json.RawMessage(annotatedSchema))
			require.Error(t, err)
			if tc.code != 0 {
				var rpcErr *jsonrpc.Error
				require.ErrorAs(t, err, &rpcErr)
				require.EqualValues(t, tc.code, rpcErr.Code)
				if tc.code == -32020 {
					require.NotContains(t, err.Error(), "should-not-be-logged")
				}
			}
			require.Len(t, s.matching("tools/call"), tc.wantCalls)
			require.Len(t, s.matching("tools/list"), tc.wantLists)
		})
	}
}

func TestParameterHeadersInvalidMetadata(t *testing.T) {
	t.Parallel()
	invalid := json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"bad name"}}}`)
	t.Run("persisted", func(t *testing.T) {
		t.Parallel()
		s := &parameterServer{schema: plainSchema}
		c := newParameterClient(t, s, nil)()
		_, err := c.CallTool(t.Context(), "lookup", json.RawMessage(`{}`), invalid)
		require.Error(t, err)
		require.Empty(t, s.matching("tools/list"))
		require.Empty(t, s.matching("tools/call"))
	})
	t.Run("discovered", func(t *testing.T) {
		t.Parallel()
		s := &parameterServer{schema: string(invalid)}
		c := newParameterClient(t, s, nil)()
		_, err := c.CallTool(t.Context(), "lookup", json.RawMessage(`{}`))
		require.ErrorContains(t, err, "absent or invalid")
		require.Len(t, s.matching("tools/list"), 1)
		require.Empty(t, s.matching("tools/call"))
	})
}

func TestParameterHeadersInBandErrorNotRetried(t *testing.T) {
	t.Parallel()
	s := &parameterServer{schema: plainSchema, isError: true}
	c := newParameterClient(t, s, nil)()
	result, err := c.CallTool(t.Context(), "lookup", json.RawMessage(`{}`), json.RawMessage(plainSchema))
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Len(t, s.matching("tools/call"), 1)
	require.Empty(t, s.matching("tools/list"))
}

func TestParameterHeadersAdapterMatchesSDKOnWire(t *testing.T) {
	t.Parallel()
	schema := json.RawMessage(`{"type":"object","properties":{"nested":{"type":"object","properties":{"account":{"type":"string","x-mcp-header":"Account"},"enabled":{"type":"boolean","x-mcp-header":"Flag"}}},"count":{"type":"integer","x-mcp-header":"Count"}}}`)
	args := json.RawMessage(`{"nested":{"account":"café / ?","enabled":true},"count":9007199254740991}`)
	s := &parameterServer{schema: string(schema)}
	makeClient := newParameterClient(t, s, nil)
	_, err := makeClient().CallTool(t.Context(), "lookup", args, schema)
	require.NoError(t, err)
	_, err = makeClient().CallTool(t.Context(), "lookup", args)
	require.NoError(t, err)
	calls := s.matching("tools/call")
	require.Len(t, calls, 2)
	for _, h := range []string{"Mcp-Param-Account", "Mcp-Param-Flag", "Mcp-Param-Count"} {
		require.NotEmpty(t, calls[0].Headers.Get(h))
		require.Equal(t, calls[0].Headers.Values(h), calls[1].Headers.Values(h))
		require.Len(t, calls[0].Headers.Values(h), 1)
	}
	require.Len(t, s.matching("tools/list"), 1)
}

func TestParameterHeadersRejectAmbiguousAuthConfiguration(t *testing.T) {
	t.Parallel()
	_, err := NewClient(t.Context(), testenv.NewLogger(t), nil, "https://mcp.example.test", types.TransportTypeStreamableHTTP, &ClientOptions{
		Headers: map[string]string{"Authorization": "Bearer first", "authorization": "Bearer second"},
	})
	require.ErrorContains(t, err, "duplicate external MCP configured header name")
	require.NotContains(t, err.Error(), "Bearer")
}

func TestParameterHeadersFreshSchemaRejectingServer(t *testing.T) {
	t.Parallel()
	for _, property := range []string{`{"type":["string","null"]}`, `true`, `false`} {
		for _, value := range []string{"value", ""} {
			for _, path := range []string{"known", "cold", "recovery"} {
				t.Run(property+"/"+value+"/"+path, func(t *testing.T) {
					t.Parallel()
					schema := json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"Account"},"unrelated":` + property + `}}`)
					encoded := value
					if encoded == "" {
						encoded = "=?base64??="
					}
					want := make(http.Header)
					want.Set("Mcp-Param-Account", encoded)
					s := &parameterServer{schema: string(schema), wantParameters: want}
					client := newParameterClient(t, s, nil)()
					var inputSchema json.RawMessage
					wantLists, wantCalls := 0, 1
					switch path {
					case "known":
						inputSchema = schema
					case "cold":
						wantLists = 1
					case "recovery":
						wantLists, wantCalls = 1, 2
						inputSchema = json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"Old"}}}`)
					}
					rawValue, err := json.Marshal(value)
					require.NoError(t, err)
					args := json.RawMessage(`{"owner":` + string(rawValue) + `,"large":9007199254740993,"decimal":1.0000000000000001}`)
					result, err := client.CallTool(t.Context(), "lookup", args, inputSchema)
					require.NoError(t, err)
					require.False(t, result.IsError)
					require.Len(t, s.matching("tools/list"), wantLists)
					calls := s.matching("tools/call")
					require.Len(t, calls, wantCalls)
					for _, call := range calls {
						require.Equal(t, string(args), string(call.Params.Arguments))
					}
					require.Equal(t, encoded, calls[len(calls)-1].Headers.Get("Mcp-Param-Account"))
					require.Empty(t, calls[len(calls)-1].Headers.Get("Mcp-Param-Old"))
				})
			}
		}
	}
}

func TestParameterHeadersEmptyAcceptedBySDK(t *testing.T) {
	t.Parallel()
	// The real SDK rejects a raw empty header as missing. Its decoder accepts
	// the explicit empty Base64 sentinel; test both sides over Streamable HTTP.
	server := mcp.NewServer(&mcp.Implementation{Name: "empty-header-fixture", Version: "1"}, nil)
	accepted := make(chan json.RawMessage, 2)
	server.AddTool(&mcp.Tool{Name: "lookup", InputSchema: json.RawMessage(annotatedSchema)}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		accepted <- req.Params.Arguments
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	rejected, err := http.NewRequestWithContext(t.Context(), http.MethodPost, upstream.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lookup","arguments":{"owner":""}}}`))
	require.NoError(t, err)
	rejected.Header.Set("Content-Type", "application/json")
	rejected.Header.Set("Accept", "application/json, text/event-stream")
	rejected.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	rejected.Header.Set("Mcp-Method", "tools/call")
	rejected.Header.Set("Mcp-Name", "lookup")
	rejected.Header.Set("Mcp-Param-Account", "")
	response, err := upstream.Client().Do(rejected)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.NoError(t, response.Body.Close())
	require.Empty(t, accepted)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	args := json.RawMessage(`{"owner":"","large":9007199254740993,"decimal":1.0000000000000001}`)
	for _, schema := range []json.RawMessage{json.RawMessage(annotatedSchema), nil} {
		client, err := NewClient(t.Context(), testenv.NewLogger(t), policy, upstream.URL, types.TransportTypeStreamableHTTP, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })
		result, err := client.CallTool(t.Context(), "lookup", args, schema)
		require.NoError(t, err)
		require.False(t, result.IsError)
	}
	require.Len(t, accepted, 2)
	require.Equal(t, string(args), string(<-accepted))
	require.Equal(t, string(args), string(<-accepted))
}
