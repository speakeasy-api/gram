//go:build unix

package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	stdioFixtureEnv = "GRAM_TUNNEL_STDIO_FIXTURE"
	// stdioFixtureReadTokenAtStart makes the fixture exit unless its token
	// file is readable before it reads stdin.
	stdioFixtureReadTokenAtStart = "GRAM_TUNNEL_STDIO_FIXTURE_READ_TOKEN"
)

func TestMain(m *testing.M) {
	if os.Getenv(stdioFixtureEnv) == "1" {
		runStdioFixture()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runStdioFixture() {
	fmt.Fprintln(os.Stderr, "fixture server starting")
	if os.Getenv(stdioFixtureReadTokenAtStart) == "1" {
		// A contract-aware server may read its token while starting up.
		if token, err := os.ReadFile(os.Getenv(AccessTokenFileEnv)); err != nil || len(token) == 0 {
			os.Exit(4)
		}
	}
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
				Name       string         `json:"name"`
				Arguments  map[string]any `json:"arguments"`
				ClientInfo struct {
					Name string `json:"name"`
				} `json:"clientInfo"`
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
			if msg.Params.ClientInfo.Name == "ping-first" {
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": "p0", "method": "ping"})
				pong, err := reader.ReadBytes('\n')
				if err != nil || !bytes.Contains(pong, []byte(`"p0"`)) {
					return
				}
			}
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
			case "token-sha":
				// Only a digest leaves the fixture, never the token.
				token, err := os.ReadFile(os.Getenv(AccessTokenFileEnv))
				text := "unreadable"
				if err == nil {
					sum := sha256.Sum256(token)
					text = hex.EncodeToString(sum[:])
				}
				reply(map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}})
			case "env":
				env := map[string]any{}
				for _, name := range []string{AccessTokenFileEnv, "OKTA_ACCESS_TOKEN_FILE", "HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "TUNNEL_KEY", "INHERITED_SETTING"} {
					if value, ok := os.LookupEnv(name); ok {
						env[name] = value
					}
				}
				encoded, _ := json.Marshal(env)
				reply(map[string]any{"content": []any{map[string]any{"type": "text", "text": string(encoded)}}})
			case "close-stdin":
				// Stops reading requests but keeps running. stdin closes
				// before the reply, so the next request's write fails.
				_ = os.Stdin.Close()
				reply(map[string]any{"content": []any{map[string]any{"type": "text", "text": "closed"}}})
				time.Sleep(time.Hour)
			case "leak":
				// Emulates an SDK error that prints the credential.
				token, _ := os.ReadFile(os.Getenv(AccessTokenFileEnv))
				fmt.Fprintf(os.Stderr, "upstream rejected Authorization: Bearer %s\n", token)
				reply(map[string]any{"content": []any{map[string]any{"type": "text", "text": "leaked"}}})
			case "block":
				// A synchronous server busy with a slow tool: it reads no stdin meanwhile.
				time.Sleep(2 * time.Second)
				reply(map[string]any{"content": []any{map[string]any{"type": "text", "text": "done"}}})
			case "elicit":
				pendingElicit = msg.ID
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": "srv-1", "method": "elicitation/create", "params": map[string]any{"message": "name?"}})
			default:
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]any{"level": "info", "data": "echoing"}})
				reply(map[string]any{"content": []any{map[string]any{"type": "text", "text": fmt.Sprint(msg.Params.Arguments["text"])}}})
			}
		case "":
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
	return newStdioTestServerWithCommand(t, stdioFixtureEnv+"=1 '"+exe+"'", maxSessions)
}

func newStdioTestServerWithCommand(t *testing.T, command string, maxSessions int) (*httptest.Server, *Agent) {
	t.Helper()
	a, err := New(Config{
		GatewayURL:       "wss://example.test/connect",
		APIKey:           "gram_tunnel_test",
		LocalMCPURL:      "",
		LocalMCPCommand:  command,
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

func TestStdioBridgeKeepsSilentStreamsAlive(t *testing.T) {
	t.Parallel()
	srv, a := newStdioTestServer(t, 0)
	a.stdio.keepalive = 20 * time.Millisecond
	sid := initializeSession(t, srv)

	// No GET stream, so the elicitation goes on the call's own stream, which then waits on the client.
	// A deadline turns a missing keepalive into a failure rather than a hung read.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/", strings.NewReader(`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"elicit"}}`))
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerMCPSessionID, sid)
	call, err := srv.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = call.Body.Close() })
	require.Equal(t, http.StatusOK, call.StatusCode)
	body := bufio.NewReader(call.Body)

	readUntil := func(prefix string) string {
		for {
			line, err := body.ReadString('\n')
			require.NoError(t, err)
			if strings.HasPrefix(line, prefix) {
				return line
			}
		}
	}
	require.Contains(t, readUntil("data: "), "elicitation/create")
	readUntil(": keepalive")
	readUntil(": keepalive")

	answered := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":"srv-1","result":{"action":"accept"}}`)
	require.Equal(t, http.StatusAccepted, answered.StatusCode)
	result := readUntil("data: ")
	require.Contains(t, result, `"id":10`)
	require.Contains(t, result, "accept")
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
	env := childEnv([]string{"PATH=/bin", "TUNNEL_KEY=secret", "tunnel_key=secret", "TUNNEL_GATEWAY_URL=wss://x", "API_TOKEN=keep"})
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

func TestStdioBridgeAnswersPingDuringInitialize(t *testing.T) {
	t.Parallel()
	srv, _ := newStdioTestServer(t, 0)

	resp := mcpRequest(t, srv, http.MethodPost, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"ping-first","version":"1"}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotEmpty(t, resp.Header.Get(headerMCPSessionID))
}

func TestCanonicalRPCIDComparesByValue(t *testing.T) {
	t.Parallel()
	key := func(raw string) string {
		id, err := canonicalRPCID(json.RawMessage(raw))
		require.NoError(t, err)
		return id
	}
	require.Equal(t, key(`"ab"`), key(`"ab"`))
	require.Equal(t, key(`1`), key(`1.0`))
	require.Equal(t, key(`10`), key(`1e1`))
	require.NotEqual(t, key(`1`), key(`"1"`))

	_, err := canonicalRPCID(json.RawMessage(`{"a":1}`))
	require.Error(t, err)
}

func TestCanonicalRPCIDRejectsCostlyNumbers(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`1e999999999`, `1e-999999999`, strings.Repeat("9", 65)} {
		_, err := canonicalRPCID(json.RawMessage(raw))
		require.Error(t, err, raw)
	}
	_, err := canonicalRPCID(json.RawMessage(`12345e3`))
	require.NoError(t, err)
}

func TestReadFrameEnforcesLimit(t *testing.T) {
	t.Parallel()
	reader := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", 100)+"\nok\n"), 16)
	_, err := readFrame(reader, 64)
	require.ErrorIs(t, err, errStdioFrameTooLarge)

	reader = bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", 40)+"\n"), 16)
	frame, err := readFrame(reader, 64)
	require.NoError(t, err)
	require.Len(t, frame, 41)
}

func newRoutingSession() *stdioSession {
	return &stdioSession{
		id:           "test",
		cmd:          nil,
		stdin:        nil,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		stdinStall:   stdioStdinStallTimeout,
		closing:      atomic.Bool{},
		writeSem:     make(chan struct{}, 1),
		exited:       make(chan struct{}),
		terminated:   make(chan struct{}),
		done:         make(chan struct{}),
		closeOnce:    sync.Once{},
		pings:        make(chan json.RawMessage, stdioPingQueue),
		mu:           sync.Mutex{},
		initializing: false,
		pending:      make(map[string]*rpcStream),
		streams:      make(map[*rpcStream]struct{}),
		listener:     nil,
		backlog:      nil,
		backlogBytes: 0,
		inflight:     0,
		lastActive:   time.Now(),
	}
}

func mustParseRPC(t *testing.T, raw string) rpcMessage {
	t.Helper()
	msg, err := parseRPCMessage([]byte(raw))
	require.NoError(t, err)
	return msg
}

func TestStdioRouteCutsOffSlowListenerWithoutBlocking(t *testing.T) {
	t.Parallel()
	sess := newRoutingSession()
	listener, err := sess.attachListener()
	require.NoError(t, err)
	listener.limit = 256

	post, err := sess.openStream([]rpcMessage{mustParseRPC(t, `{"id":7,"method":"x"}`)}, true)
	require.NoError(t, err)

	notification := `{"jsonrpc":"2.0","method":"notifications/message","params":{"data":"` + strings.Repeat("x", 100) + `"}}`
	for range 10 {
		sess.route(mustParseRPC(t, notification))
	}
	select {
	case <-listener.gone:
	default:
		t.Fatal("a listener over its byte limit must be cut off")
	}

	sess.route(mustParseRPC(t, `{"jsonrpc":"2.0","id":7,"result":{}}`))
	notifications := 0
	for {
		event, ok := post.pop()
		require.True(t, ok, "the response must reach its POST stream")
		if event.response {
			break
		}
		notifications++
	}
	require.Positive(t, notifications, "messages the cut-off listener could not take fall back to an open POST stream")
	require.Empty(t, sess.backlog)
}

func TestStdioRouteBacklogsWhenNoStreamCanTakeMessage(t *testing.T) {
	t.Parallel()
	sess := newRoutingSession()
	listener, err := sess.attachListener()
	require.NoError(t, err)
	sess.detachListener(listener)

	sess.route(mustParseRPC(t, `{"jsonrpc":"2.0","id":"srv","method":"ping"}`))
	require.Len(t, sess.backlog, 1)

	next, err := sess.attachListener()
	require.NoError(t, err)
	event, ok := next.pop()
	require.True(t, ok, "the next GET stream receives the backlog")
	require.Contains(t, string(event.msg), `"srv"`)
}

func TestStdioRouteRetiresAnsweredStream(t *testing.T) {
	t.Parallel()
	sess := newRoutingSession()
	post, err := sess.openStream([]rpcMessage{mustParseRPC(t, `{"id":1,"method":"x"}`)}, true)
	require.NoError(t, err)

	sess.route(mustParseRPC(t, `{"jsonrpc":"2.0","id":1,"result":{}}`))
	sess.route(mustParseRPC(t, `{"jsonrpc":"2.0","id":"srv","method":"ping"}`))

	_, ok := post.pop()
	require.True(t, ok)
	_, ok = post.pop()
	require.False(t, ok, "a server request after the final response must not go to the finished stream")
	require.Len(t, sess.backlog, 1)
}

func TestStdioRouteBoundsInitializePings(t *testing.T) {
	t.Parallel()
	sess := newRoutingSession()
	sess.initializing = true

	for i := range 100 {
		sess.route(mustParseRPC(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"ping"}`, i)))
	}
	require.Len(t, sess.pings, stdioPingQueue, "pings past the queue are dropped, not buffered")
	require.Empty(t, sess.backlog)
}

func TestStdioBridgeReleasesSessionWhenServerIgnoresStdin(t *testing.T) {
	t.Parallel()
	srv, a := newStdioTestServerWithCommand(t, "sleep 300", 1)

	// Larger than a pipe buffer, so the write blocks on a server that never reads.
	padding := strings.Repeat("x", 1<<20)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"pad":"`+padding+`"}}`))
	require.NoError(t, err)
	if resp, err := srv.Client().Do(req); err == nil {
		_ = resp.Body.Close()
	}

	require.Eventually(t, func() bool {
		a.stdio.mu.Lock()
		defer a.stdio.mu.Unlock()
		return len(a.stdio.sessions) == 0
	}, 20*time.Second, 100*time.Millisecond, "a cancelled initialize must release its session slot")
}

func TestStdioBridgeRejectsDuplicateIDsInBatch(t *testing.T) {
	t.Parallel()
	srv, _ := newStdioTestServer(t, 0)
	sid := initializeSession(t, srv)

	resp := mcpRequest(t, srv, http.MethodPost, sid, `[{"jsonrpc":"2.0","id":5,"method":"tools/list"},{"jsonrpc":"2.0","id":5.0,"method":"tools/list"}]`)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestStdioBridgeAnswersRequestsWhenServerExits(t *testing.T) {
	t.Parallel()
	srv, _ := newStdioTestServer(t, 0)
	sid := initializeSession(t, srv)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/", strings.NewReader(`[{"jsonrpc":"2.0","id":"c","method":"tools/call","params":{"name":"crash"}},{"jsonrpc":"2.0","id":"l","method":"tools/list"}]`))
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")
	req.Header.Set(headerMCPSessionID, sid)
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var batch []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&batch))
	require.Len(t, batch, 2, "every request gets a response even though the server exited")
	ids := make([]any, 0, len(batch))
	for _, item := range batch {
		ids = append(ids, item["id"])
		if item["id"] == "c" {
			require.Contains(t, item, "error")
		}
	}
	require.ElementsMatch(t, []any{"c", "l"}, ids)
}

func TestAcceptsSSEHonorsQualityZero(t *testing.T) {
	t.Parallel()
	accepts := func(header string) bool {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("Accept", header)
		return acceptsSSE(req)
	}
	require.True(t, accepts("application/json, text/event-stream"))
	require.True(t, accepts("text/event-stream;q=0.5"))
	require.True(t, accepts("TEXT/EVENT-STREAM"))
	require.False(t, accepts("application/json, text/event-stream;q=0"))
	require.False(t, accepts("application/json"))
}

func TestStdioBridgeReapsQuietSessionWithOpenGetStream(t *testing.T) {
	t.Parallel()
	srv, a := newStdioTestServer(t, 0)
	a.stdio.idleTimeout = 300 * time.Millisecond
	go a.stdio.reap(t.Context())
	sid := initializeSession(t, srv)

	listen := mcpRequest(t, srv, http.MethodGet, sid, "")
	require.Equal(t, http.StatusOK, listen.StatusCode)

	require.Eventually(t, func() bool {
		return a.stdio.session(sid) == nil
	}, 20*time.Second, 50*time.Millisecond, "an open GET stream alone must not keep a quiet session alive")
}

func TestStdioReapTickerToleratesTinyIdleTimeout(t *testing.T) {
	t.Parallel()
	b := newStdioBridge("true", 1, time.Nanosecond, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	require.NotPanics(t, func() { b.reap(ctx) })
}

// stdioTestInitializes is a shell command prefix that answers initialize.
const stdioTestInitializes = `read l; printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"x","version":"1"}}}'; `

func TestStdioBridgeCancelledWriteKeepsSession(t *testing.T) {
	t.Parallel()
	srv, _ := newStdioTestServer(t, 0)
	sid := initializeSession(t, srv)

	blocked := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"block"}}`)
	require.Equal(t, http.StatusOK, blocked.StatusCode)

	// Larger than a pipe buffer, so the write waits on the busy server; the client gives up first.
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"text":"` + strings.Repeat("x", 1<<20) + `"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(headerMCPSessionID, sid)
	if resp, err := srv.Client().Do(req); err == nil {
		_ = resp.Body.Close()
	}

	done, err := io.ReadAll(blocked.Body)
	require.NoError(t, err)
	require.Contains(t, string(done), `"text":"done"`)
	list := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":4,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, list.StatusCode, "a client giving up on its request must not end the session")
	// The echo's notification may land on this stream too; the stream ends after the answer.
	answered, err := io.ReadAll(list.Body)
	require.NoError(t, err)
	require.Contains(t, string(answered), `"id":4`)
}

func TestStdioBridgeBackloggedMessagesDoNotKeepSessionAlive(t *testing.T) {
	t.Parallel()
	srv, a := newStdioTestServerWithCommand(t, stdioTestInitializes+`while :; do printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/message","params":{"data":"tick"}}'; sleep 0.05; done`, 0)
	a.stdio.idleTimeout = 300 * time.Millisecond
	go a.stdio.reap(t.Context())
	sid := initializeSession(t, srv)

	require.Eventually(t, func() bool {
		return a.stdio.session(sid) == nil
	}, 20*time.Second, 50*time.Millisecond, "notifications nobody receives must not count as activity")
}

func TestStdioSessionLogsStderrAfterLongLine(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	sess := newRoutingSession()
	sess.logger = slog.New(slog.NewJSONHandler(&out, nil))
	sess.logStderr(strings.NewReader("before\n" + strings.Repeat("y", 70<<10) + "\nafter\n"))

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)
	require.Contains(t, lines[0], `"line":"before"`)
	require.Contains(t, lines[1], `"truncated":true`)
	require.Less(t, len(lines[1]), 16<<10, "a long line is truncated in the log")
	require.Contains(t, lines[2], `"line":"after"`, "logging continues after a long line")
}

func TestStdioBridgeRefusesRequestsDuringTeardown(t *testing.T) {
	t.Parallel()
	// Ignores stdin closing, so teardown takes until SIGTERM.
	srv, _ := newStdioTestServerWithCommand(t, stdioTestInitializes+`sleep 300`, 0)
	sid := initializeSession(t, srv)

	require.Equal(t, http.StatusNoContent, mcpRequest(t, srv, http.MethodDelete, sid, "").StatusCode)
	require.Equal(t, http.StatusNotFound, mcpRequest(t, srv, http.MethodGet, sid, "").StatusCode)
	require.Equal(t, http.StatusNotFound, mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`).StatusCode)
	require.Equal(t, http.StatusNotFound, mcpRequest(t, srv, http.MethodDelete, sid, "").StatusCode)
}

func TestStdioBridgeReportsServerExitDuringInitialize(t *testing.T) {
	t.Parallel()
	srv, _ := newStdioTestServerWithCommand(t, `echo boom >&2; exit 3`, 0)

	resp := mcpRequest(t, srv, http.MethodPost, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "exited during initialize")
}

func TestStdioSessionRateLimitsStderr(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	sess := newRoutingSession()
	sess.logger = slog.New(slog.NewJSONHandler(&out, nil))
	sess.logStderr(strings.NewReader(strings.Repeat("noise\n", stdioStderrLinesPerMinute+50)))

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, stdioStderrLinesPerMinute+1)
	require.Contains(t, lines[len(lines)-1], `"lines":50`, "lines over the limit are counted, not logged")
}

func TestStdioSessionClaimIdleExcludesAdmittedRequests(t *testing.T) {
	t.Parallel()
	sess := newRoutingSession()
	later := time.Now().Add(time.Hour)

	release, ok := sess.acquire()
	require.True(t, ok)
	require.False(t, sess.claimIdle(later, time.Minute), "a session with a request in flight is not idle")
	release()

	require.True(t, sess.claimIdle(later, time.Minute))
	_, ok = sess.acquire()
	require.False(t, ok, "a request after the session was claimed is refused")
	require.False(t, sess.claimIdle(later, time.Minute), "a session is claimed once")
}

func TestStdioRouteCutOffFreesQueueAndAbortsWriter(t *testing.T) {
	t.Parallel()
	sess := newRoutingSession()
	listener, err := sess.attachListener()
	require.NoError(t, err)
	listener.limit = 256
	aborts := 0
	listener.setAbort(func() { aborts++ })

	notification := `{"jsonrpc":"2.0","method":"notifications/message","params":{"data":"` + strings.Repeat("x", 100) + `"}}`
	for range 10 {
		sess.route(mustParseRPC(t, notification))
	}
	require.Equal(t, 1, aborts, "a cut-off stream fails its handler's write at once")
	_, ok := listener.pop()
	require.False(t, ok, "a cut-off stream frees what it queued")
}

// Not parallel: pushing 32 MiB through the bridge would skew the timing of
// parallel tests.
func TestStdioBridgeStalledReaderReleasesSession(t *testing.T) {
	// 32 MiB of notifications: more than socket buffers hold, under the stream's cut-off.
	notification := filepath.Join(t.TempDir(), "notification")
	require.NoError(t, os.WriteFile(notification, []byte(`{"jsonrpc":"2.0","method":"notifications/message","params":{"data":"`+strings.Repeat("z", 1<<20)+`"}}`+"\n"), 0o600))
	command := stdioTestInitializes + `read l; read l; i=0; while [ $i -lt 32 ]; do cat '` + notification + `'; i=$((i+1)); done; sleep 300`
	srv, a := newStdioTestServerWithCommand(t, command, 0)
	a.stdio.responseWriteTimeout = 200 * time.Millisecond
	sid := initializeSession(t, srv)
	sess := a.stdio.session(sid)
	require.NotNil(t, sess)

	// The client never reads the stream.
	call := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"x"}}`)
	require.Equal(t, http.StatusOK, call.StatusCode)

	require.Eventually(t, func() bool {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return sess.inflight == 0 && len(sess.streams) == 0
	}, 30*time.Second, 50*time.Millisecond, "a stalled reader must not hold the request, and the session, open")
}

func TestStdioBridgeStopsServerThatStopsReadingStdin(t *testing.T) {
	t.Parallel()
	srv, a := newStdioTestServerWithCommand(t, stdioTestInitializes+`read l; sleep 300`, 0)
	a.stdio.stdinStallTimeout = 200 * time.Millisecond
	sid := initializeSession(t, srv)

	// Larger than a pipe buffer, so the write stalls on a server that no longer reads.
	resp := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","method":"notifications/message","params":{"data":"`+strings.Repeat("x", 1<<20)+`"}}`)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Eventually(t, func() bool {
		return a.stdio.session(sid) == nil
	}, 10*time.Second, 50*time.Millisecond, "a server that stops reading stdin is stopped")
}

func TestStdioBridgeEvictsIdlestSessionAtCapacity(t *testing.T) {
	t.Parallel()
	srv, a := newStdioTestServer(t, 1)
	a.stdio.evictIdleAfter = 100 * time.Millisecond
	first := initializeSession(t, srv)
	time.Sleep(200 * time.Millisecond)

	second := initializeSession(t, srv)
	require.NotEqual(t, first, second)
	require.Equal(t, http.StatusNotFound, mcpRequest(t, srv, http.MethodPost, first, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`).StatusCode,
		"the evicted client gets 404 and starts over")
	require.Equal(t, http.StatusOK, mcpRequest(t, srv, http.MethodPost, second, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`).StatusCode)
}

func TestStdioBridgeWaitsForClosingSessionBeforeEvicting(t *testing.T) {
	t.Parallel()
	// Ignores stdin closing, so a deleted session holds its slot until SIGTERM.
	srv, a := newStdioTestServerWithCommand(t, stdioTestInitializes+`sleep 300`, 2)
	a.stdio.evictIdleAfter = time.Millisecond
	idle := initializeSession(t, srv)
	deleted := initializeSession(t, srv)
	time.Sleep(10 * time.Millisecond)

	require.Equal(t, http.StatusNoContent, mcpRequest(t, srv, http.MethodDelete, deleted, "").StatusCode)
	initializeSession(t, srv)
	require.NotNil(t, a.stdio.session(idle), "a slot about to free is waited for rather than evicting a live session")
}

func TestStdioBridgeDoesNotEvictBusySessionAtCapacity(t *testing.T) {
	t.Parallel()
	srv, a := newStdioTestServer(t, 1)
	a.stdio.evictIdleAfter = 100 * time.Millisecond
	sid := initializeSession(t, srv)

	// Stays in flight: the elicitation waits on an answer this test never sends.
	call := mcpRequest(t, srv, http.MethodPost, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"elicit"}}`)
	require.Equal(t, http.StatusOK, call.StatusCode)
	time.Sleep(200 * time.Millisecond)

	resp := mcpRequest(t, srv, http.MethodPost, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Equal(t, "5", resp.Header.Get("Retry-After"))
	require.NotNil(t, a.stdio.session(sid))
}
