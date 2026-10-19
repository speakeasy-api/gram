package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type kitCheckpointFake struct {
	inventories []string
	status      string
	calls       []string
	err         error
}

func (f *kitCheckpointFake) AttachSession(context.Context, string) error { return f.err }
func (f *kitCheckpointFake) Call(_ context.Context, method string, params any, session string) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	if f.err != nil {
		return nil, f.err
	}
	switch method {
	case "session/list":
		p, ok := params.(map[string]any)
		if !ok || session != "" || len(p) != 0 {
			return nil, errors.New("inventory must include every root")
		}
		raw := f.inventories[0]
		if len(f.inventories) > 1 {
			f.inventories = f.inventories[1:]
		}
		return json.RawMessage(raw), nil
	case "session/resume":
		p, ok := params.(map[string]string)
		if !ok || len(p) != 2 || p["sessionId"] != session {
			return nil, errors.New("unexpected resume overlay or replay")
		}
		return json.RawMessage(`{}`), nil
	case "kit/session/status":
		return json.RawMessage(f.status), nil
	default:
		return nil, errors.New("unexpected method")
	}
}

const kitCheckpointResident = `{"sessions":[{"sessionId":"bound","cwd":"/workspace","_meta":{"kit.gateway.state":"resident"}}]}`
const kitCheckpointRestorable = `{"sessions":[{"sessionId":"bound","cwd":"/workspace","_meta":{"kit.gateway.state":"restorable"}}]}`
const kitCheckpointSettled = `{"sessionId":"bound","settled":true,"foregroundActive":false,"backgroundActive":false,"pendingBackgroundTerminals":false,"activeSubagents":0}`

func TestKitCheckpointInventoryFailsClosed(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{}`, `null`, `{"sessions":null}`, `{"sessions":[],"nextCursor":"more"}`,
		`{"sessions":[{"sessionId":"bound","cwd":"/workspace"}]}`,
		`{"sessions":[{"sessionId":"bound","cwd":"/workspace","_meta":{"kit.gateway.state":"idle"}}]}`,
		`{"sessions":[{"sessionId":"bound","cwd":"/workspace","_meta":{"kit.gateway.state":"exited"}}]}`,
		`{"sessions":[{"sessionId":"bound","cwd":"/workspace","_meta":{"kit.gateway.state":"resident"}},{"sessionId":"bound","cwd":"/workspace","_meta":{"kit.gateway.state":"restorable"}}]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			_, err := kitCheckpointInventory(json.RawMessage(raw))
			require.Error(t, err)
		})
	}
}

func TestKitCheckpointSettlement(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		inventory string
		next      string
		status    string
		bound     map[string]bool
		wantError bool
	}{
		{name: "bound settled", inventory: kitCheckpointResident, status: kitCheckpointSettled, bound: map[string]bool{"bound": true}},
		{name: "durable stopped session needs no process", inventory: kitCheckpointRestorable, bound: map[string]bool{"bound": true}},
		{name: "empty inventory", inventory: `{"sessions":[]}`},
		{name: "unbound durable history", inventory: kitCheckpointRestorable},
		{name: "unbound resident", inventory: kitCheckpointResident, wantError: true},
		{name: "missing canonical binding", inventory: `{"sessions":[]}`, bound: map[string]bool{"bound": true}, wantError: true},
		{name: "wrong root", inventory: `{"sessions":[{"sessionId":"bound","cwd":"/other","_meta":{"kit.gateway.state":"resident"}}]}`, bound: map[string]bool{"bound": true}, wantError: true},
		{name: "background job", inventory: kitCheckpointResident, status: `{"sessionId":"bound","settled":true,"foregroundActive":false,"backgroundActive":true,"pendingBackgroundTerminals":false,"activeSubagents":0}`, bound: map[string]bool{"bound": true}, wantError: true},
		{name: "pending terminal", inventory: kitCheckpointResident, status: `{"sessionId":"bound","settled":true,"foregroundActive":false,"backgroundActive":false,"pendingBackgroundTerminals":true,"activeSubagents":0}`, bound: map[string]bool{"bound": true}, wantError: true},
		{name: "active subagent", inventory: kitCheckpointResident, status: `{"sessionId":"bound","settled":true,"foregroundActive":false,"backgroundActive":false,"pendingBackgroundTerminals":false,"activeSubagents":1}`, bound: map[string]bool{"bound": true}, wantError: true},
		{name: "incomplete status", inventory: kitCheckpointResident, status: `{"sessionId":"bound","settled":true}`, bound: map[string]bool{"bound": true}, wantError: true},
		{name: "inventory changes", inventory: kitCheckpointResident, next: kitCheckpointRestorable, status: kitCheckpointSettled, bound: map[string]bool{"bound": true}, wantError: true},
		{name: "new unbound process", inventory: `{"sessions":[]}`, next: kitCheckpointResident, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &kitCheckpointFake{inventories: []string{tt.inventory}, status: tt.status}
			if tt.next != "" {
				f.inventories = append(f.inventories, tt.next)
			}
			err := verifyKitCheckpointSessions(context.Background(), f, "/workspace", tt.bound)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tt.inventory == kitCheckpointRestorable {
				require.Equal(t, []string{"session/list", "session/list"}, f.calls)
			}
		})
	}
}

func TestKitCheckpointSettlementTransportFailure(t *testing.T) {
	t.Parallel()
	f := &kitCheckpointFake{err: errors.New("authentication or gateway unavailable")}
	require.Error(t, verifyKitCheckpointSessions(context.Background(), f, "/workspace", nil))
}

// Exercise connection-level inventory and session-level observations over the
// same authenticated HTTP/SSE transport used by the canonical factory.
func TestKitCheckpointAuthenticatedGateway(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	streams := map[string]chan string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		session := r.Header.Get("Acp-Session-Id")
		switch r.Method {
		case http.MethodPost:
			var msg struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.NewDecoder(r.Body).Decode(&msg) != nil {
				w.WriteHeader(400)
				return
			}
			if msg.Method == "initialize" {
				w.Header().Set("Acp-Connection-Id", "connection")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}}}}}`, msg.ID)
				return
			}
			result := "{}"
			switch msg.Method {
			case "session/list":
				if session != "" {
					w.WriteHeader(400)
					return
				}
				result = kitCheckpointResident
			case "session/resume":
			case "kit/session/status":
				result = kitCheckpointSettled
			default:
				w.WriteHeader(400)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			if ch == nil {
				w.WriteHeader(400)
				return
			}
			w.WriteHeader(202)
			ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, result)
		case http.MethodGet:
			ch := make(chan string, 8)
			mu.Lock()
			streams[session] = ch
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			flushKitTestResponse(t, w)
			for {
				select {
				case data := <-ch:
					_, _ = fmt.Fprint(w, data)
					flushKitTestResponse(t, w)
				case <-r.Context().Done():
					return
				}
			}
		case http.MethodDelete:
			w.WriteHeader(202)
		}
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL + "/acp/v2")
	require.NoError(t, err)
	p := &KitRuntimeProtocol{config: KitRuntimeProtocolConfig{Endpoint: endpoint, GatewayToken: "fixture-token", HTTPClient: kitProtocolTestHTTPClient(t)}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, err := p.dial(ctx, assistantRuntimeRecord{RuntimeVersion: 3})
	require.NoError(t, err)
	defer closeKitTransport(c)
	require.NoError(t, verifyKitCheckpointSessions(ctx, c, "/workspace", map[string]bool{"bound": true}))
	p.config.GatewayToken = "wrong-token"
	_, err = p.dial(ctx, assistantRuntimeRecord{RuntimeVersion: 3})
	require.Error(t, err)
}
