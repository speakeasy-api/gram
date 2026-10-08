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
	"strings"
	"sync"
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
				// Stops reading requests but keeps running.
				reply(map[string]any{"content": []any{map[string]any{"type": "text", "text": "closed"}}})
				_ = os.Stdin.Close()
				time.Sleep(time.Hour)
			case "leak":
				// Emulates an SDK error that prints the credential.
				token, _ := os.ReadFile(os.Getenv(AccessTokenFileEnv))
				fmt.Fprintf(os.Stderr, "upstream rejected Authorization: Bearer %s\n", token)
				reply(map[string]any{"content": []any{map[string]any{"type": "text", "text": "leaked"}}})
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
