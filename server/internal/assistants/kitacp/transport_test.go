package kitacp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/guardian"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestTransport(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var mu sync.Mutex
	streams := map[string]chan string{}
	var canceled, deleted, prompts atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/acp/v2" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("bad endpoint/auth")
		}
		session := r.Header.Get("Acp-Session-Id")
		if r.Method == http.MethodPost {
			var m envelope
			if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
				t.Error(err)
				return
			}
			if m.Method == "initialize" {
				w.Header().Set("Acp-Connection-Id", "conn")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":2,"info":{"name":"test","version":"0"},"capabilities":{}}}`, m.ID)
				return
			}
			if r.Header.Get("Acp-Connection-Id") != "conn" {
				t.Error("missing connection")
			}
			if m.Method == "session/cancel" {
				if len(m.ID) != 0 {
					t.Error("cancel must be notification")
				}
				canceled.Add(1)
				w.WriteHeader(202)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			if ch == nil {
				t.Error("POST before SSE")
				w.WriteHeader(400)
				return
			}
			w.WriteHeader(202)
			result := `{"sessions":[]}`
			switch m.Method {
			case "session/new":
				result = `{"sessionId":"s1"}`
			case "session/resume":
				result = `{}`
			case "session/prompt":
				prompts.Add(1)
				return
			}
			ch <- fmt.Sprintf("id: ignored\ndata: {\ndata: \"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", m.ID, result)
			return
		}
		if r.Header.Get("Acp-Connection-Id") != "conn" {
			t.Error("missing connection")
		}
		switch r.Method {
		case http.MethodGet:
			if r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("Last-Event-ID") != "" {
				t.Error("bad SSE headers")
			}
			ch := make(chan string, 8)
			mu.Lock()
			streams[session] = ch
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			flushTestResponse(t, w)
			for {
				select {
				case msg := <-ch:
					_, _ = fmt.Fprint(w, msg)
					flushTestResponse(t, w)
				case <-r.Context().Done():
					return
				}
			}
		case http.MethodDelete:
			deleted.Add(1)
			w.WriteHeader(202)
		}
	}))
	defer s.Close()
	setup, stopSetup := context.WithCancel(ctx)
	c, _, err := Dial(setup, s.URL+"/acp/v2", "test-secret", testHTTPClient(t), json.RawMessage(`{"protocolVersion":2,"info":{"name":"test","version":"0"},"capabilities":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseTransport(ctx) }()
	stopSetup() // setup cancellation must not own live streams
	if _, err = c.Call(ctx, "session/list", json.RawMessage(`{}`), ""); err != nil {
		t.Fatal(err)
	}
	result, err := c.Call(ctx, "session/new", json.RawMessage(`{"cwd":"/approved","mcpServers":[]}`), "")
	if err != nil || string(result) != `{"sessionId":"s1"}` {
		t.Fatalf("new: %s %v", result, err)
	}
	if _, err = c.Call(ctx, "session/prompt", json.RawMessage(`{}`), "s1"); err == nil {
		t.Fatal("prompt permitted before attach")
	}
	if err = c.AttachSession(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Call(ctx, "session/resume", json.RawMessage(`{"sessionId":"s1","cwd":"/approved","mcpServers":[]}`), "s1"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	streams["s1"] <- "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"session/update\",\"params\":{\"sessionId\":\"s1\"}}\n\n"
	mu.Unlock()
	select {
	case n := <-c.Notifications():
		if n.SessionID != "s1" || n.Method != "session/update" {
			t.Fatal(n)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	short, stop := context.WithTimeout(ctx, 30*time.Millisecond)
	defer stop()
	_, err = c.Call(short, "session/prompt", json.RawMessage(`{"sessionId":"s1","prompt":[]}`), "s1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if prompts.Load() != 1 || canceled.Load() != 0 {
		t.Fatal("implicit retry or cancellation")
	}
	if err = c.Cancel(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if err = c.CloseTransport(ctx); err != nil {
		t.Fatal(err)
	}
	if deleted.Load() != 1 || canceled.Load() != 1 {
		t.Fatal("teardown changed session work")
	}
}
func TestRedirectAndSanitizedErrors(t *testing.T) {
	t.Parallel()
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(307)
		_, _ = fmt.Fprint(w, "test-secret")
	}))
	defer origin.Close()
	_, _, err := Dial(context.Background(), origin.URL+"/acp/v2", "test-secret", testHTTPClient(t), struct{}{})
	if err == nil || strings.Contains(err.Error(), "test-secret") || leaked.Load() {
		t.Fatalf("unsafe redirect/error: %v", err)
	}
}
func TestSSEBoundsAndCorrelation(t *testing.T) {
	t.Parallel()
	c := &Client{pending: map[string]pending{"2": {session: "right", ch: make(chan reply, 1)}}, notifications: make(chan Notification, 1)}
	if err := c.dispatch([]byte(`{"jsonrpc":"2.0","id":2,"result":{}}`), "wrong"); err == nil {
		t.Fatal("wrong stream accepted")
	}
	if err := c.readSSE(strings.NewReader("data: "+strings.Repeat("x", MaxMessageBytes)+"\n\n"), ""); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if err := c.readSSE(strings.NewReader(strings.Repeat(": comment\n", MaxMessageBytes/9+1)), ""); err == nil {
		t.Fatal("oversized multiline frame accepted")
	}
}

func testHTTPClient(t *testing.T) *guardian.HTTPClient {
	t.Helper()
	return guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)).Client(guardian.WithAllowedCIDRBlocks("127.0.0.1/32"))
}
func flushTestResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	flusher, ok := w.(http.Flusher)
	if !ok {
		t.Error("test server requires HTTP flusher")
		return
	}
	flusher.Flush()
}

type marshalAfterFailure struct{ fail func() }

func (p marshalAfterFailure) MarshalJSON() ([]byte, error) {
	p.fail()
	return []byte(`{}`), nil
}

func TestCallFailureDuringMarshalPreventsDispatch(t *testing.T) {
	t.Parallel()
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	life, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := &Client{http: testHTTPClient(t), endpoint: server.URL, done: life.Done(), cancel: cancel, pending: make(map[string]pending), streams: map[string]bool{"session": true}}
	terminal := errors.New("stream failed")
	_, err := c.Call(t.Context(), "session/prompt", marshalAfterFailure{fail: func() {
		c.fail(terminal)
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, request := range c.pending {
			// fail must cancel each registered HTTP request synchronously, before returning.
			if request.cancel == nil {
				t.Error("request cancellation not registered")
			}
		}
	}}, "session")
	if err == nil {
		t.Fatal("call accepted after stream failure")
	}
	if posts.Load() != 0 {
		t.Fatal("terminal transport dispatched a new request")
	}
}

func TestReplayBackpressureWithSlowConsumer(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	posted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		close(posted)
	}))
	defer server.Close()
	c := &Client{
		http: testHTTPClient(t), endpoint: server.URL, done: ctx.Done(), cancel: cancel,
		pending: make(map[string]pending), streams: map[string]bool{"session": true},
		notifications: make(chan Notification, 256),
	}
	called := make(chan error, 1)
	go func() {
		_, err := c.Call(ctx, "session/resume", struct{}{}, "session")
		called <- err
	}()
	select {
	case <-posted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	const count = 768
	var replay strings.Builder
	for i := range count {
		fmt.Fprintf(&replay, "data: {\"jsonrpc\":\"2.0\",\"method\":\"session/update\",\"params\":{\"index\":%d}}\n\n", i)
	}
	replay.WriteString("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")
	read := make(chan error, 1)
	go func() { read <- c.readSSE(strings.NewReader(replay.String()), "session") }()
	// Hold the consumer until the buffer is full. Replay must wait, not fail.
	for len(c.notifications) != cap(c.notifications) {
		select {
		case err := <-read:
			t.Fatalf("replay failed before consumer started: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	// Backpressure holds no correlation lock: another stream can still reply.
	other := make(chan reply, 1)
	c.mu.Lock()
	c.pending["2"] = pending{session: "other", ch: other}
	c.mu.Unlock()
	correlated := make(chan error, 1)
	go func() {
		correlated <- c.dispatch([]byte(`{"jsonrpc":"2.0","id":2,"result":{}}`), "other")
	}()
	select {
	case err := <-correlated:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("correlation blocked behind replay")
	}
	select {
	case <-other:
	case <-ctx.Done():
		t.Fatal("response on independent stream was not correlated")
	}
	consumerTick := time.NewTicker(time.Millisecond)
	defer consumerTick.Stop()
	for i := range count {
		select {
		case n := <-c.Notifications():
			want := fmt.Sprintf(`{"index":%d}`, i)
			if n.SessionID != "session" || string(n.Params) != want {
				t.Fatalf("notification %d: %+v", i, n)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		select {
		case <-consumerTick.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	select {
	case err := <-called:
		if err != nil {
			t.Fatalf("resume response behind replay: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("resume response blocked behind replay")
	}
	if err := <-read; err == nil || err.Error() != "ACP event stream ended" {
		t.Fatalf("unexpected replay reader result: %v", err)
	}
}

func TestNotificationBackpressureUnblocksOnClose(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := &Client{done: ctx.Done(), cancel: cancel, notifications: make(chan Notification, 1)}
	c.notifications <- Notification{}
	dispatched := make(chan error, 1)
	go func() {
		dispatched <- c.dispatch([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{}}`), "session")
	}()
	c.fail(ErrClosed)
	select {
	case err := <-dispatched:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("dispatch after close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("backpressured dispatch did not stop on close")
	}
}

func TestTransportFailurePreservesSanitizedCause(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://" + listener.Addr().String() + "/acp/v2"
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, _, err = Dial(ctx, endpoint, "private-test-token", testHTTPClient(t), struct{}{})
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("connection refusal cause lost: %v", err)
	}
	if _, ok := errors.AsType[*TransportError](err); !ok {
		t.Fatalf("missing typed transport error: %v", err)
	}
	if strings.Contains(err.Error(), endpoint) || strings.Contains(err.Error(), "private-test-token") {
		t.Fatalf("unsanitized transport error: %v", err)
	}
	secretCause := errors.New("private remote detail")
	wrapped := &TransportError{cause: secretCause}
	if !errors.Is(wrapped, secretCause) || strings.Contains(wrapped.Error(), secretCause.Error()) {
		t.Fatal("cause must be accessible without leaking into error text")
	}
}
