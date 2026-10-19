package kitacp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/guardian"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
