package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestNativeThreadCatalog(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, raw, canonical string
		valid                bool
	}{
		{"bound", `{"sessions":[{"sessionId":"s","cwd":"/workspace"}]}`, "s", true},
		{"empty", `{"sessions":[]}`, "", true},
		{"null cursor", `{"sessions":[],"nextCursor":null}`, "", true},
		{"missing", `{}`, "", false}, {"null", `{"sessions":null}`, "", false},
		{"pagination", `{"sessions":[],"nextCursor":"next"}`, "", false},
		{"empty cursor", `{"sessions":[],"nextCursor":""}`, "", false},
		{"unbound", `{"sessions":[{"sessionId":"s","cwd":"/workspace"}]}`, "", false},
		{"missing canonical", `{"sessions":[]}`, "s", false},
		{"wrong session", `{"sessions":[{"sessionId":"other","cwd":"/workspace"}]}`, "s", false},
		{"wrong cwd", `{"sessions":[{"sessionId":"s","cwd":"/other"}]}`, "s", false},
		{"duplicate", `{"sessions":[{"sessionId":"s","cwd":"/workspace"},{"sessionId":"s","cwd":"/workspace"}]}`, "s", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.valid, nativeThreadCatalog(json.RawMessage(tc.raw), "/workspace", tc.canonical) == nil)
		})
	}
}

func TestCloseNativeThreadSessionRequiresFenceAndNativeEndpoint(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/acp", "/acp/v2"} {
		endpoint, err := url.Parse("http://localhost" + path)
		require.NoError(t, err)
		p := &KitRuntimeProtocol{config: KitRuntimeProtocolConfig{Endpoint: endpoint}}
		require.ErrorIs(t, p.CloseNativeThreadSession(t.Context(), assistantRuntimeRecord{}, uuid.New()), ErrKitRuntimeBinding)
	}
}

func TestNativeThreadSessionSettlement(t *testing.T) {
	t.Parallel()
	settled := `{"sessionId":"s","settled":true,"foregroundActive":false,"backgroundActive":false,"pendingBackgroundTerminals":false,"activeSubagents":0}`
	for _, tc := range []struct {
		name, status                                 string
		closeError, changedCatalog, valid, wantClose bool
	}{
		{name: "settled close ACK", status: settled, valid: true, wantClose: true},
		{name: "foreground", status: `{"sessionId":"s","settled":true,"foregroundActive":true,"backgroundActive":false,"pendingBackgroundTerminals":false,"activeSubagents":0}`},
		{name: "background", status: `{"sessionId":"s","settled":true,"foregroundActive":false,"backgroundActive":true,"pendingBackgroundTerminals":false,"activeSubagents":0}`},
		{name: "subagents", status: `{"sessionId":"s","settled":true,"foregroundActive":false,"backgroundActive":false,"pendingBackgroundTerminals":false,"activeSubagents":1}`},
		{name: "pending terminals", status: `{"sessionId":"s","settled":false,"foregroundActive":false,"backgroundActive":false,"pendingBackgroundTerminals":true,"activeSubagents":0}`},
		{name: "missing status", status: `{}`},
		{name: "close error", status: settled, closeError: true, wantClose: true},
		{name: "postclose catalog changed", status: settled, changedCatalog: true, wantClose: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var mu sync.Mutex
			streams := map[string]chan string{}
			var methods []string
			catalogReads := 0
			closeRequested := make(chan struct{})
			closeAck := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				session := r.Header.Get("Acp-Session-Id")
				switch r.Method {
				case http.MethodGet:
					ch := make(chan string, 8)
					mu.Lock()
					streams[session] = ch
					mu.Unlock()
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
					for {
						select {
						case frame := <-ch:
							_, _ = fmt.Fprint(w, frame)
							w.(http.Flusher).Flush()
						case <-r.Context().Done():
							return
						}
					}
				case http.MethodDelete:
					w.WriteHeader(http.StatusNoContent)
				case http.MethodPost:
					var msg struct {
						ID     json.RawMessage `json:"id"`
						Method string          `json:"method"`
						Params json.RawMessage `json:"params"`
					}
					require.NoError(t, json.NewDecoder(r.Body).Decode(&msg))
					if msg.Method == "initialize" {
						w.Header().Set("Acp-Connection-Id", "connection")
						_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}}}}}`, msg.ID)
						return
					}
					mu.Lock()
					methods = append(methods, msg.Method)
					ch := streams[session]
					mu.Unlock()
					result := `{}`
					switch msg.Method {
					case "session/list":
						require.Equal(t, "", session)
						require.JSONEq(t, `{"cwd":"/workspace"}`, string(msg.Params))
						catalogReads++
						result = `{"sessions":[{"sessionId":"s","cwd":"/workspace"}]}`
						if tc.changedCatalog && catalogReads == 2 {
							result = `{"sessions":[]}`
						}
					case "session/resume":
						require.JSONEq(t, `{"sessionId":"s","cwd":"/workspace"}`, string(msg.Params))
					case "kit/session/status":
						result = tc.status
					case "session/close":
						require.JSONEq(t, `{"sessionId":"s"}`, string(msg.Params))
					default:
						t.Errorf("unexpected method %s", msg.Method)
					}
					w.WriteHeader(http.StatusAccepted)
					if msg.Method == "session/close" {
						close(closeRequested)
						go func() {
							select {
							case <-closeAck:
							case <-ctx.Done():
								return
							}
							if tc.closeError {
								ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"error\":{\"code\":-32603,\"message\":\"close failed\"}}\n\n", msg.ID)
								return
							}
							ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{}}\n\n", msg.ID)
						}()
						return
					}
					ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, result)
				}
			}))
			defer server.Close()
			endpoint, err := url.Parse(server.URL + "/acp")
			require.NoError(t, err)
			hc := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)).Client(guardian.WithAllowedCIDRBlocks("127.0.0.1/32"))
			client, _, err := kitacp.Dial(ctx, endpoint.String(), "token", hc, struct{}{})
			require.NoError(t, err)
			defer closeKitTransport(client)
			p := &KitRuntimeProtocol{config: KitRuntimeProtocolConfig{Endpoint: endpoint, CWD: "/workspace"}}
			done := make(chan error, 1)
			go func() { done <- p.closeNativeThreadSession(ctx, client, "s") }()
			if tc.wantClose {
				select {
				case <-closeRequested:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				select {
				case err := <-done:
					t.Fatalf("returned before close ACK: %v", err)
				default:
				}
				mu.Lock()
				require.Equal(t, []string{"session/list", "session/resume", "kit/session/status", "session/close"}, methods)
				mu.Unlock()
				close(closeAck)
			}
			select {
			case err := <-done:
				require.Equal(t, tc.valid, err == nil)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if !tc.wantClose {
				select {
				case <-closeRequested:
					t.Fatal("closed unsettled session")
				default:
				}
			}
		})
	}
}
