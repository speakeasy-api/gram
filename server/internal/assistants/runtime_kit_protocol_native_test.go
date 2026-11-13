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

	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestKitNativeProtocolEndpoints(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		endpoint string
		valid    bool
	}{
		{"http://localhost/acp", true}, {"https://localhost/acp/v2", true},
		{"http://localhost/acp/", false}, {"http://localhost/acp/v2/", false},
		{"http://localhost/acp?", false}, {"http://localhost/acp?x=1", false},
		{"http://localhost/acp#fragment", false}, {"http://localhost/%61cp", false},
		{"http://user@localhost/acp", false}, {"ftp://localhost/acp", false},
		{"/acp", false},
	} {
		t.Run(tc.endpoint, func(t *testing.T) {
			endpoint, err := url.Parse(tc.endpoint)
			require.NoError(t, err)
			_, err = NewKitRuntimeProtocol(KitRuntimeProtocolConfig{Core: &ServiceCore{}, Projector: &KitTranscriptProjector{}, Endpoint: endpoint, CWD: "/workspace"})
			require.Equal(t, tc.valid, err == nil)
		})
	}
}

func TestKitNativeProtocolCloseAcknowledgedBeforeDetach(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/acp", "/acp/v2"} {
		t.Run(path, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var mu sync.Mutex
			streams := map[string]chan string{}
			closeRequested := make(chan struct{})
			closeAck := make(chan struct{})
			deleted := make(chan struct{})
			var deleteOnce sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, path, r.URL.Path)
				session := r.Header.Get("Acp-Session-Id")
				switch r.Method {
				case http.MethodGet:
					ch := make(chan string, 4)
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
					deleteOnce.Do(func() { close(deleted) })
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
						_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{}}`, msg.ID)
						return
					}
					require.Equal(t, "session/close", msg.Method)
					require.JSONEq(t, `{"sessionId":"session"}`, string(msg.Params))
					require.Equal(t, "session", session)
					w.WriteHeader(http.StatusAccepted)
					close(closeRequested)
					go func() {
						select {
						case <-closeAck:
						case <-ctx.Done():
							return
						}
						mu.Lock()
						ch := streams[session]
						mu.Unlock()
						ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{}}\n\n", msg.ID)
					}()
				}
			}))
			defer server.Close()
			endpoint, err := url.Parse(server.URL + path)
			require.NoError(t, err)
			hc := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)).Client(guardian.WithAllowedCIDRBlocks("127.0.0.1/32"))
			client, _, err := kitacp.Dial(ctx, endpoint.String(), "token", hc, struct{}{})
			require.NoError(t, err)
			defer closeKitTransport(client)
			require.NoError(t, client.AttachSession(ctx, "session"))
			protocol := &KitRuntimeProtocol{config: KitRuntimeProtocolConfig{Endpoint: endpoint}}
			done := make(chan error, 1)
			go func() {
				err := protocol.closeSettledSession(ctx, client, "session")
				if err == nil {
					err = client.CloseTransport(ctx)
				}
				done <- err
			}()
			if path == "/acp" {
				select {
				case <-closeRequested:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				select {
				case <-deleted:
					t.Fatal("transport detached before close ACK")
				default:
				}
				select {
				case err := <-done:
					t.Fatalf("close completed before ACK: %v", err)
				default:
				}
				close(closeAck)
			}
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case <-deleted:
			default:
				t.Fatal("transport not detached")
			}
			if path == "/acp/v2" {
				select {
				case <-closeRequested:
					t.Fatal("gateway resident session was closed")
				default:
				}
			}
		})
	}
}
