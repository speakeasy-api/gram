package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const stdioFixtureEnv = "GRAM_TUNNEL_STDIO_FIXTURE"

func TestMain(m *testing.M) {
	if os.Getenv(stdioFixtureEnv) == "1" {
		runStdioFixture()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runStdioFixture is a minimal stdio MCP server driven by the tests below.
func runStdioFixture() {
	fmt.Fprintln(os.Stderr, "fixture server starting")
	out := json.NewEncoder(os.Stdout)
	reader := bufio.NewReader(os.Stdin)
	pendingElicit := json.RawMessage(nil)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			return
		}
		reply := func(result any) {
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
		}
		switch msg.Method {
		case "initialize":
			reply(map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "fixture", "version": "1.0.0"},
			})
		case "tools/list":
			reply(map[string]any{"tools": []any{map[string]any{"name": "echo", "inputSchema": map[string]any{"type": "object"}}}})
		case "tools/call":
			switch msg.Params.Name {
			case "crash":
				os.Exit(3)
			case "elicit":
				pendingElicit = msg.ID
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": "srv-1", "method": "elicitation/create", "params": map[string]any{"message": "name?"}})
			default:
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]any{"level": "info", "data": "echoing"}})
				reply(map[string]any{"content": []any{map[string]any{"type": "text", "text": fmt.Sprint(msg.Params.Arguments["text"])}}})
			}
		case "":
			// A response to our elicitation request.
			if pendingElicit != nil {
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": pendingElicit, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": string(msg.Result)}}}})
				pendingElicit = nil
			}
		}
	}
}

func newStdioTestServer(t *testing.T, maxSessions int) (*httptest.Server, *Agent) {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)

	a, err := New(Config{
		GatewayURL:       "wss://example.test/connect",
		APIKey:           "gram_tunnel_test",
		LocalMCPURL:      "",
		LocalMCPCommand:  stdioFixtureEnv + "=1 '" + exe + "'",
		StdioMaxSessions: maxSessions,
		StdioIdleTimeout: 0,
		ServiceVersion:   "1.0.0",
		Metadata:         map[string]string{},
		MinBackoff:       0,
		MaxBackoff:       0,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	srv := httptest.NewServer(a.handler)
	t.Cleanup(func() {
		srv.Close()
		a.stdio.Close()
	})
	return srv, a
}

func mcpRequest(t *testing.T, srv *httptest.Server, method, sid, body string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+"/", reader)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if sid != "" {
		req.Header.Set(headerMCPSessionID, sid)
	}
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func initializeSession(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	resp := mcpRequest(t, srv, http.MethodPost, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	sid := resp.Header.Get(headerMCPSessionID)
	require.NotEmpty(t, sid)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Contains(t, body, "result")

	notified := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	require.Equal(t, http.StatusAccepted, notified.StatusCode)
	return sid
}

func readSSEEvents(t *testing.T, r io.Reader, n int) []map[string]any {
	t.Helper()
	scanner := bufio.NewScanner(r)
	events := make([]map[string]any, 0, n)
	for len(events) < n && scanner.Scan() {
		line := scanner.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var event map[string]any
		require.NoError(t, json.Unmarshal([]byte(data), &event))
		events = append(events, event)
	}
	require.Len(t, events, n)
	return events
}

func TestStdioBridgeSessionLifecycle(t *testing.T) {
	t.Parallel()
	srv, a := newStdioTestServer(t, 0)
	sid := initializeSession(t, srv)

	list := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, list.StatusCode)
	require.Equal(t, "text/event-stream", list.Header.Get("Content-Type"))
	events := readSSEEvents(t, list.Body, 1)
	require.InDelta(t, 2, events[0]["id"], 0)

	call := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`)
	require.Equal(t, http.StatusOK, call.StatusCode)
	events = readSSEEvents(t, call.Body, 2)
	require.Equal(t, "notifications/message", events[0]["method"])
	require.Equal(t, "call-1", events[1]["id"])
	rest, err := io.ReadAll(call.Body)
	require.NoError(t, err)
	require.Empty(t, bytes.TrimSpace(rest), "stream must end once every request is answered")

	sess := a.stdio.session(sid)
	require.NotNil(t, sess)
	deleted := mcpRequest(t, srv, http.MethodDelete, sid, "")
	require.Equal(t, http.StatusNoContent, deleted.StatusCode)
	select {
	case <-sess.done:
	case <-time.After(10 * time.Second):
		t.Fatal("server process did not exit after DELETE")
	}
	gone := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	require.Equal(t, http.StatusNotFound, gone.StatusCode)
}

func TestStdioBridgeJSONResponseMode(t *testing.T) {
	t.Parallel()
	srv, _ := newStdioTestServer(t, 0)
	sid := initializeSession(t, srv)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/", strings.NewReader(`[{"jsonrpc":"2.0","id":7,"method":"tools/list"}]`))
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")
	req.Header.Set(headerMCPSessionID, sid)
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var batch []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&batch))
	require.Len(t, batch, 1)
	require.InDelta(t, 7, batch[0]["id"], 0)
}

func TestStdioBridgeServerInitiatedRequestUsesGetStream(t *testing.T) {
	t.Parallel()
	srv, _ := newStdioTestServer(t, 0)
	sid := initializeSession(t, srv)

	listen := mcpRequest(t, srv, http.MethodGet, sid, "")
	require.Equal(t, http.StatusOK, listen.StatusCode)

	second := mcpRequest(t, srv, http.MethodGet, sid, "")
	require.Equal(t, http.StatusConflict, second.StatusCode)

	call := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"elicit"}}`)
	require.Equal(t, http.StatusOK, call.StatusCode)

	elicit := readSSEEvents(t, listen.Body, 1)
	require.Equal(t, "elicitation/create", elicit[0]["method"])
	require.Equal(t, "srv-1", elicit[0]["id"])

	answered := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":"srv-1","result":{"action":"accept"}}`)
	require.Equal(t, http.StatusAccepted, answered.StatusCode)

	result := readSSEEvents(t, call.Body, 1)
	require.InDelta(t, 10, result[0]["id"], 0)
	require.Contains(t, fmt.Sprint(result[0]["result"]), "accept")
}

func TestStdioBridgeProcessCrashEndsSession(t *testing.T) {
	t.Parallel()
	srv, _ := newStdioTestServer(t, 0)
	sid := initializeSession(t, srv)

	call := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"crash"}}`)
	require.Equal(t, http.StatusOK, call.StatusCode)
	_, err := io.ReadAll(call.Body)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		resp := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":5,"method":"tools/list"}`)
		return resp.StatusCode == http.StatusNotFound
	}, 10*time.Second, 50*time.Millisecond)
}

func TestStdioBridgeEnforcesSessionCap(t *testing.T) {
	t.Parallel()
	srv, _ := newStdioTestServer(t, 1)
	sid := initializeSession(t, srv)

	resp := mcpRequest(t, srv, http.MethodPost, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Empty(t, resp.Header.Get(headerMCPSessionID))

	require.Equal(t, http.StatusNoContent, mcpRequest(t, srv, http.MethodDelete, sid, "").StatusCode)
	require.Eventually(t, func() bool {
		resp := mcpRequest(t, srv, http.MethodPost, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
		return resp.StatusCode == http.StatusOK
	}, 10*time.Second, 50*time.Millisecond)
}

func TestStdioBridgeRejectsBadRequests(t *testing.T) {
	t.Parallel()
	srv, _ := newStdioTestServer(t, 0)

	noSession := mcpRequest(t, srv, http.MethodPost, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusBadRequest, noSession.StatusCode)

	unknown := mcpRequest(t, srv, http.MethodPost, "deadbeef", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusNotFound, unknown.StatusCode)

	malformed := mcpRequest(t, srv, http.MethodPost, "", `{not json`)
	require.Equal(t, http.StatusBadRequest, malformed.StatusCode)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/oauth/token", strings.NewReader("grant_type=x"))
	require.NoError(t, err)
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestStdioChildEnvDropsTunnelSettings(t *testing.T) {
	t.Parallel()
	env := childEnv([]string{"PATH=/bin", "TUNNEL_KEY=secret", "TUNNEL_GATEWAY_URL=wss://x", "API_TOKEN=keep"})
	require.Equal(t, []string{"PATH=/bin", "API_TOKEN=keep"}, env)
}

func TestNewRequiresExactlyOneUpstream(t *testing.T) {
	t.Parallel()
	base := Config{
		GatewayURL:       "wss://example.test/connect",
		APIKey:           "gram_tunnel_test",
		LocalMCPURL:      "",
		LocalMCPCommand:  "",
		StdioMaxSessions: 0,
		StdioIdleTimeout: 0,
		ServiceVersion:   "1.0.0",
		Metadata:         map[string]string{},
		MinBackoff:       0,
		MaxBackoff:       0,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	_, err := New(base, logger)
	require.Error(t, err)

	both := base
	both.LocalMCPURL = "http://localhost:3000/mcp"
	both.LocalMCPCommand = "npx server"
	_, err = New(both, logger)
	require.Error(t, err)
}
