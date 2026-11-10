package assistants

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	"github.com/stretchr/testify/require"
)

func TestKitLocalEnsureWaitsForListenerBeforeAuthenticatedProbe(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"ready", "unauthorized", "uncertain POST"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			k, engine, runtime, _ := kitLocalFixture(t)
			reserved, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			address := reserved.Addr().String()
			addr, ok := reserved.Addr().(*net.TCPAddr)
			require.True(t, ok)
			engine.nextPort = addr.Port
			require.NoError(t, reserved.Close())
			var posts atomic.Int32
			server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/acp/v2" || r.Header.Get("Authorization") != "Bearer readiness-test-token" {
					t.Error("readiness must use authenticated ACP")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				switch r.Method {
				case http.MethodPost:
					posts.Add(1)
					if outcome == "unauthorized" {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					if outcome == "uncertain POST" {
						// The server accepted initialization but its response was lost.
						hijacker, ok := w.(http.Hijacker)
						if !ok {
							t.Error("response writer does not support hijacking")
							return
						}
						conn, _, err := hijacker.Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = conn.Close()
						return
					}
					w.Header().Set("Acp-Connection-Id", "readiness-connection")
					_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"_meta":{"kit/session/status":{}}}}}`)
				case http.MethodGet:
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(http.StatusOK)
					flusher, ok := w.(http.Flusher)
					if !ok {
						t.Error("response writer does not support flushing")
						return
					}
					flusher.Flush()
					<-r.Context().Done()
				case http.MethodDelete:
					w.WriteHeader(http.StatusAccepted)
				}
			})}
			defer func() { require.NoError(t, server.Close()) }()
			dialer := &net.Dialer{Timeout: time.Second}
			attempts := 0
			k.listenerDial = func(ctx context.Context, network, target string) (net.Conn, error) {
				attempts++
				conn, dialErr := dialer.DialContext(ctx, network, target)
				if attempts == 1 {
					require.ErrorIs(t, dialErr, syscall.ECONNREFUSED)
					// Docker has returned from Start, but the guest binds only after the
					// first refused TCP probe. No ACP request has been sent yet.
					require.Zero(t, posts.Load())
					listener, err := net.Listen("tcp", address)
					require.NoError(t, err)
					go func() { _ = server.Serve(listener) }()
				}
				if dialErr != nil {
					return conn, fmt.Errorf("dial readiness listener: %w", dialErr)
				}
				return conn, nil
			}
			readyCalls := 0
			k.config.Ready = func(ctx context.Context, r assistantRuntimeRecord, endpoint *url.URL) error {
				readyCalls++
				_, bounded := ctx.Deadline()
				require.True(t, bounded)
				protocol := &KitRuntimeProtocol{config: KitRuntimeProtocolConfig{Endpoint: endpoint, GatewayToken: "readiness-test-token", HTTPClient: kitProtocolTestHTTPClient(t)}}
				_, err := protocol.Status(ctx, r)
				return err
			}
			result, err := k.Ensure(t.Context(), runtime)
			require.GreaterOrEqual(t, attempts, 2)
			require.Equal(t, 1, readyCalls)
			require.EqualValues(t, 1, posts.Load(), "never retry authenticated ACP POST")
			if outcome == "ready" {
				require.NoError(t, err)
				require.NotEmpty(t, result.BackendMetadataJSON)
				require.NotContains(t, engine.calls, "stop")
			} else {
				require.Error(t, err)
				require.Empty(t, result.BackendMetadataJSON)
				require.Contains(t, engine.calls, "stop")
				if outcome == "uncertain POST" {
					var transportErr *kitacp.TransportError
					require.ErrorAs(t, err, &transportErr)
					require.Error(t, errors.Unwrap(transportErr))
				}
			}
		})
	}
}

func TestKitLocalEnsureListenerWaitDeadline(t *testing.T) {
	t.Parallel()
	k, engine, runtime, _ := kitLocalFixture(t)
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr, ok := reserved.Addr().(*net.TCPAddr)
	require.True(t, ok)
	engine.nextPort = addr.Port
	require.NoError(t, reserved.Close())
	k.listenerDial = (&net.Dialer{Timeout: time.Second}).DialContext
	readyCalls := 0
	k.config.Ready = func(context.Context, assistantRuntimeRecord, *url.URL) error { readyCalls++; return nil }
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	result, err := k.Ensure(ctx, runtime)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, syscall.ECONNREFUSED)
	require.Zero(t, readyCalls)
	require.Empty(t, result.BackendMetadataJSON)
	require.Contains(t, engine.calls, "stop")
	require.False(t, engine.info.Running)
}
