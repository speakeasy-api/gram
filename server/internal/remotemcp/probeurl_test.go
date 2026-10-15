package remotemcp_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	remotemcpproxy "github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newPermissivePolicy(t *testing.T) *guardian.Policy {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return policy
}

func TestProbeURL_RBACForbidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPRead, Selector: authz.NewSelector(authz.ScopeMCPRead, authCtx.ProjectID.String())})

	_, err := ti.service.ProbeURL(ctx, &gen.ProbeURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: "https://mcp.example.com",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestProbeURL_InvalidURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})

	_, err := ti.service.ProbeURL(ctx, &gen.ProbeURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: "ftp://example.com",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestProbeURL_RejectsHostedHTTP(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})

	_, err := ti.service.ProbeURL(ctx, &gen.ProbeURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: "http://8.8.8.8/mcp",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorIs(t, err, remotemcpproxy.ErrInsecureRemoteMCPTransport)
}

func TestProbeURL_RejectsUserinfo(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})

	_, err := ti.service.ProbeURL(ctx, &gen.ProbeURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: "https://user:secret@mcp.example.com/mcp",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorIs(t, err, remotemcpproxy.ErrRemoteMCPURLUserinfo)
}

func TestProbeURL_BlockedHostIsStructuredUnreachable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})

	result, err := ti.service.ProbeURL(ctx, &gen.ProbeURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: "https://" + blockedTestHost,
	})
	require.NoError(t, err)
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonGuardianRejected, requireValue(t, result.Reason))
	require.Nil(t, result.HTTPStatus)
}

func TestVerifyURL_CompatibilityInvalidMCPResponse(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"healthy":true}`))
	}))
	t.Cleanup(upstream.Close)

	ctx, ti := newTestServiceWithPolicy(t, newPermissivePolicy(t))
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})
	result, err := ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: upstream.URL, TransportType: "streamable-http",
	})
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Equal(t, http.StatusOK, requireValue(t, result.HTTPStatus))
	require.Equal(t, "Reachable: although received unexpected MCP response", result.Message)
}

func TestVerifyURL_CompatibilityAuthenticationRequired(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	ctx, ti := newTestServiceWithPolicy(t, newPermissivePolicy(t))
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})
	result, err := ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: upstream.URL, TransportType: "streamable-http",
	})
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Equal(t, http.StatusUnauthorized, requireValue(t, result.HTTPStatus))
	require.Equal(t, "Reachable: received authorization required response", result.Message)
}

// recordMethods passes each request to next and records the JSON-RPC method
// of every POST body it sees.
func recordMethods(next http.Handler) (http.Handler, func() []string) {
	var mu sync.Mutex
	var methods []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var msg struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal(body, &msg)
			mu.Lock()
			methods = append(methods, msg.Method)
			mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		next.ServeHTTP(w, r)
	})
	return handler, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(methods)
	}
}

// jsonrpcHandler answers each JSON-RPC call with respond and acknowledges
// notifications with 202 Accepted, recording every method it receives.
func jsonrpcHandler(respond func(w http.ResponseWriter, r *http.Request, method string, id json.RawMessage)) (http.Handler, func() []string) {
	return recordMethods(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(msg.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		respond(w, r, msg.Method, msg.ID)
	}))
}

func jsonrpcUpstream(t *testing.T, respond func(w http.ResponseWriter, r *http.Request, method string, id json.RawMessage)) (*httptest.Server, func() []string) {
	t.Helper()
	handler, methods := jsonrpcHandler(respond)
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	return upstream, methods
}

func writeJSONRPC(w http.ResponseWriter, status int, id json.RawMessage, member string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,%s}`, id, member)
}

// respondWithInitialize answers initialize at 2025-06-18 and rejects every
// other method, server/discover included, as unknown.
func respondWithInitialize(w http.ResponseWriter, _ *http.Request, method string, id json.RawMessage) {
	if method == "initialize" {
		writeJSONRPC(w, http.StatusOK, id, `"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"initialize-only","version":"1"}}`)
		return
	}
	writeJSONRPC(w, http.StatusOK, id, `"error":{"code":-32601,"message":"method not found"}`)
}

func newSDKServer() *mcp.Server {
	return mcp.NewServer(&mcp.Implementation{Name: "upstream", Version: "1"}, nil)
}

// newDiscoverOnlyUpstream serves 2026-07-28 and nothing earlier. That
// revision removes initialize, so the server answers the unimplemented method
// with 404 Not Found and a -32601 body.
func newDiscoverOnlyUpstream(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	server := newSDKServer()
	sdkHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	handler, methods := recordMethods(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(body, &msg) == nil && msg.Method == "initialize" {
			writeJSONRPC(w, http.StatusNotFound, msg.ID, `"error":{"code":-32601,"message":"method not found"}`)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		sdkHandler.ServeHTTP(w, r)
	}))
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	return upstream, methods
}

func TestProbeRemoteMcpURL_FallsBackToInitializeWhenDiscoverIsUnknown(t *testing.T) {
	t.Parallel()
	var discoverHeaders http.Header
	var headersMu sync.Mutex
	upstream, methods := jsonrpcUpstream(t, func(w http.ResponseWriter, r *http.Request, method string, id json.RawMessage) {
		if method == "server/discover" {
			headersMu.Lock()
			discoverHeaders = r.Header.Clone()
			headersMu.Unlock()
		}
		respondWithInitialize(w, r, method, id)
	})

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome)
	require.Nil(t, result.HTTPStatus)
	require.Equal(t, []string{"server/discover", "initialize", "notifications/initialized"}, methods())

	headersMu.Lock()
	defer headersMu.Unlock()
	require.Equal(t, "2026-07-28", discoverHeaders.Get("Mcp-Protocol-Version"))
	require.Equal(t, "server/discover", discoverHeaders.Get("Mcp-Method"))
	require.Contains(t, discoverHeaders.Get("Accept"), "text/event-stream")
}

func TestProbeRemoteMcpURL_AcceptsStatefulSDKUpstreamThroughInitialize(t *testing.T) {
	t.Parallel()
	server := newSDKServer()
	// A stateful SDK server does not advertise 2026-07-28 from
	// server/discover, so the probe has to complete the initialize handshake.
	handler, methods := recordMethods(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome)
	require.Equal(t, []string{"server/discover", "initialize", "notifications/initialized"}, methods())
}

func TestProbeRemoteMcpURL_AcceptsDiscoverOnlyUpstream(t *testing.T) {
	t.Parallel()
	upstream, methods := newDiscoverOnlyUpstream(t)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome)
	require.Equal(t, []string{"server/discover"}, methods(), "the upstream answers server/discover, so initialize is never sent")
}

func TestVerifyURL_AcceptsDiscoverOnlyUpstream(t *testing.T) {
	t.Parallel()
	upstream, _ := newDiscoverOnlyUpstream(t)

	ctx, ti := newTestServiceWithPolicy(t, newPermissivePolicy(t))
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})
	result, err := ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: upstream.URL, TransportType: "streamable-http",
	})
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Equal(t, "Success", result.Message)
}

// statefulUpstream issues a session on initialize, answering each call with
// respond, each notification with notificationStatus, and the session DELETE
// with deleteStatus.
func statefulUpstream(t *testing.T, respond func(w http.ResponseWriter, r *http.Request, method string, id json.RawMessage), notificationStatus int, deleteStatus int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var deletes atomic.Int32
	calls, _ := jsonrpcHandler(func(w http.ResponseWriter, r *http.Request, method string, id json.RawMessage) {
		if method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "probe-session")
		}
		respond(w, r, method, id)
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes.Add(1)
			w.WriteHeader(deleteStatus)
			return
		}
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		if !strings.Contains(string(body), `"id":`) {
			w.WriteHeader(notificationStatus)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		calls.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)
	return upstream, &deletes
}

func TestProbeRemoteMcpURL_SessionDeleteDoesNotMaskInitializeError(t *testing.T) {
	t.Parallel()
	upstream, deletes := statefulUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ string, id json.RawMessage) {
		writeJSONRPC(w, http.StatusOK, id, `"error":{"code":-32602,"message":"invalid params"}`)
	}, http.StatusAccepted, http.StatusInternalServerError)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome, "the session DELETE status must not replace the initialize answer")
	require.EqualValues(t, 1, deletes.Load())
}

func TestProbeRemoteMcpURL_ClassifiesFailedInitializedNotification(t *testing.T) {
	t.Parallel()
	for name, deleteStatus := range map[string]int{
		"delete forbidden":  http.StatusForbidden,
		"delete no content": http.StatusNoContent,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			upstream, deletes := statefulUpstream(t, respondWithInitialize, http.StatusBadRequest, deleteStatus)

			result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
			require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
			require.Equal(t, http.StatusBadRequest, requireValue(t, result.HTTPStatus), "the notification status, not the session DELETE status, is reported")
			require.EqualValues(t, 1, deletes.Load())
		})
	}
}

func TestProbeRemoteMcpURL_RejectsUnknownInitializeProtocolVersion(t *testing.T) {
	t.Parallel()
	upstream, _ := jsonrpcUpstream(t, func(w http.ResponseWriter, _ *http.Request, method string, id json.RawMessage) {
		if method == "initialize" {
			writeJSONRPC(w, http.StatusOK, id, `"result":{"protocolVersion":"2099-01-01","capabilities":{},"serverInfo":{"name":"unknown","version":"1"}}`)
			return
		}
		writeJSONRPC(w, http.StatusOK, id, `"error":{"code":-32601,"message":"method not found"}`)
	})

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
	require.Equal(t, http.StatusOK, requireValue(t, result.HTTPStatus))
}

func TestProbeRemoteMcpURL_UnknownInitializeProtocolVersionDoesNotLeakGoroutines(t *testing.T) { //nolint:paralleltest // counts process goroutines, so no other test may run alongside it
	const probes = 20
	upstream, _ := jsonrpcUpstream(t, func(w http.ResponseWriter, _ *http.Request, method string, id json.RawMessage) {
		if method == "initialize" {
			writeJSONRPC(w, http.StatusOK, id, `"result":{"protocolVersion":"2099-01-01","capabilities":{},"serverInfo":{"name":"unknown","version":"1"}}`)
			return
		}
		writeJSONRPC(w, http.StatusOK, id, `"error":{"code":-32601,"message":"method not found"}`)
	})
	policy := newPermissivePolicy(t)
	baseline := runtime.NumGoroutine()

	for range probes {
		result := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL)
		require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
	}

	// Each leaked connection strands at least one reader goroutine, so a
	// leak leaves the count at least probes above the baseline.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Less(c, runtime.NumGoroutine(), baseline+probes/2)
	}, 5*time.Second, 50*time.Millisecond)
}

func TestProbeRemoteMcpURL_AcceptsCorrelatedJSONRPCError(t *testing.T) {
	t.Parallel()
	upstream, _ := jsonrpcUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ string, id json.RawMessage) {
		writeJSONRPC(w, http.StatusOK, id, `"error":{"code":-32601,"message":"unsupported"}`)
	})

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome)
}

func TestProbeRemoteMcpURL_AcceptsJSONRPCErrorOnBadRequest(t *testing.T) {
	t.Parallel()
	upstream, _ := jsonrpcUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ string, id json.RawMessage) {
		writeJSONRPC(w, http.StatusBadRequest, id, `"error":{"code":-32022,"message":"unsupported protocol version"}`)
	})

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome)
}

func TestProbeRemoteMcpURL_NonResponseJSONRPCTimesOut(t *testing.T) {
	t.Parallel()
	// A JSON-RPC request never completes the SDK's pending call.
	upstream, _ := jsonrpcUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ string, id json.RawMessage) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"method":"initialize"}`, id)
	})

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result := remotemcp.ProbeRemoteMcpURL(ctx, newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonTimeout, requireValue(t, result.Reason))
}

func TestProbeRemoteMcpURL_MismatchedResponseIDTimesOut(t *testing.T) {
	t.Parallel()
	// An uncorrelated response never completes the SDK's pending call.
	upstream, _ := jsonrpcUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ string, _ json.RawMessage) {
		writeJSONRPC(w, http.StatusOK, json.RawMessage(`"uncorrelated"`), `"result":{}`)
	})

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result := remotemcp.ProbeRemoteMcpURL(ctx, newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonTimeout, requireValue(t, result.Reason))
}

func TestProbeRemoteMcpURL_RejectsOversizedJSON(t *testing.T) {
	t.Parallel()
	padding := strings.Repeat("a", 1<<20)
	upstream, _ := jsonrpcUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ string, id json.RawMessage) {
		writeJSONRPC(w, http.StatusOK, id, `"result":{"pad":"`+padding+`"}`)
	})

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
	require.Equal(t, http.StatusOK, requireValue(t, result.HTTPStatus))
}

func TestProbeRemoteMcpURL_AcceptsCorrelatedSSEEventWithoutWaitingForEOF(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	upstream, _ := jsonrpcUpstream(t, func(w http.ResponseWriter, r *http.Request, method string, id json.RawMessage) {
		member := `"error":{"code":-32601,"message":"method not found"}`
		if method == "initialize" {
			member = `"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"sse","version":"1"}}`
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%s,%s}\n\n", id, member)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	t.Cleanup(releaseHandler)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result := remotemcp.ProbeRemoteMcpURL(ctx, newPermissivePolicy(t), upstream.URL)
	releaseHandler()
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome)
}

func TestProbeRemoteMcpURL_RejectsNonJSONRPCSSE(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: server is healthy\n\n"))
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
}

func TestProbeRemoteMcpURL_RejectsSSEWithoutResponse(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": keepalive\n\n"))
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
	require.Equal(t, http.StatusOK, requireValue(t, result.HTTPStatus))
}

func TestProbeRemoteMcpURL_RejectsNonJSONResponses(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status      int
		contentType string
	}{
		"html page":              {status: http.StatusOK, contentType: "text/html"},
		"plain-text bad request": {status: http.StatusBadRequest, contentType: "text/plain"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("not an MCP server"))
			}))
			t.Cleanup(upstream.Close)

			result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
			require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
			require.Equal(t, tc.status, requireValue(t, result.HTTPStatus))
		})
	}
}

func TestProbeRemoteMcpURL_Truncated2xxIsInvalidMCP(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The truncation is written by hand: letting the handler under-write a
		// declared Content-Length leaves the server to close the connection on
		// its own terms, which can reach the probe as a reset rather than a
		// short body and reclassify the outcome as unreachable.
		_, _ = io.Copy(io.Discard, r.Body)

		hijacker, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, buf, err := hijacker.Hijack()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 128\r\n\r\n" + `{"jsonrpc":"2.0","id":1`)
		_ = buf.Flush()

		halfCloser, ok := conn.(interface{ CloseWrite() error })
		if !ok {
			return
		}
		_ = halfCloser.CloseWrite()

		// Hold the read side open until the probe hangs up so it sees the
		// half-close mid-body instead of a reset.
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, _ = io.Copy(io.Discard, conn)
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
	require.Equal(t, http.StatusOK, requireValue(t, result.HTTPStatus))
	require.Nil(t, result.Reason)
}

func TestProbeRemoteMcpURL_AuthenticationRequiredWithMetadata(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("WWW-Authenticate", `Bearer realm="mcp", resource_metadata="https://auth.example.com/.well-known/oauth-protected-resource/mcp"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeAuthenticationRequired, result.Outcome)
	require.Equal(t, "https://auth.example.com/.well-known/oauth-protected-resource/mcp", requireValue(t, result.ProtectedResourceMetadataURL))
	require.Nil(t, result.HTTPStatus)
}

func TestProbeRemoteMcpURL_IgnoresInvalidAuthenticationMetadata(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("WWW-Authenticate", `Bearer resource_metadata="/relative"`)
		w.Header().Add("WWW-Authenticate", `Bearer resource_metadata="https://user:secret@auth.example.com/metadata"`)
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeAuthenticationRequired, result.Outcome)
	require.Nil(t, result.ProtectedResourceMetadataURL)
}

func TestProbeRemoteMcpURL_AuthenticationRequiredWithJSONRPCErrorBody(t *testing.T) {
	t.Parallel()
	upstream, _ := jsonrpcUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ string, id json.RawMessage) {
		w.Header().Add("WWW-Authenticate", `Bearer resource_metadata="https://auth.example.com/metadata"`)
		writeJSONRPC(w, http.StatusUnauthorized, id, `"error":{"code":-32001,"message":"unauthorized"}`)
	})

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeAuthenticationRequired, result.Outcome, "the auth status outranks the JSON-RPC error body")
	require.Equal(t, "https://auth.example.com/metadata", requireValue(t, result.ProtectedResourceMetadataURL))
}

func TestProbeRemoteMcpURL_DiscoverRejectionDoesNotMaskInitializeSuccess(t *testing.T) {
	t.Parallel()
	upstream, _ := jsonrpcUpstream(t, func(w http.ResponseWriter, r *http.Request, method string, id json.RawMessage) {
		// A WAF that blocks the server/discover request shape must not turn a
		// server that completes initialize into an authentication failure.
		if method == "server/discover" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		respondWithInitialize(w, r, method, id)
	})

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome)
}

func TestProbeRemoteMcpURL_ClassifiesHTTPFailures(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/timeout":
			w.WriteHeader(http.StatusRequestTimeout)
		case "/rate-limit":
			w.WriteHeader(http.StatusTooManyRequests)
		case "/server-error":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)
	policy := newPermissivePolicy(t)

	timeout := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL+"/timeout")
	rateLimit := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL+"/rate-limit")
	serverError := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL+"/server-error")
	notFound := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL+"/not-found")
	require.Equal(t, remotemcp.ProbeReasonTimeout, requireValue(t, timeout.Reason))
	require.Equal(t, remotemcp.ProbeReasonRateLimited, requireValue(t, rateLimit.Reason))
	require.Equal(t, remotemcp.ProbeReasonServerError, requireValue(t, serverError.Reason))
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, notFound.Outcome, "a plain 404 is not an MCP server")
	require.Equal(t, http.StatusNotFound, requireValue(t, notFound.HTTPStatus))
}

func TestProbeRemoteMcpURL_ResponseBodyTimeoutDoesNotHang(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(releaseHandler)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	result := remotemcp.ProbeRemoteMcpURL(ctx, newPermissivePolicy(t), upstream.URL)
	releaseHandler()
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonTimeout, requireValue(t, result.Reason))
}

func TestProbeRemoteMcpURL_DNSFailure(t *testing.T) {
	t.Parallel()
	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(_ context.Context, _, host string) ([]net.IP, error) {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		},
	})
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithResolver(resolver))
	require.NoError(t, err)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, "https://unresolvable.test/mcp")
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonDNSError, requireValue(t, result.Reason))
}

func TestProbeRemoteMcpURL_TLSFailure(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonTLSError, requireValue(t, result.Reason))
}

func TestProbeRemoteMcpURL_FollowsSeeOtherRedirectsAsGETLikeTheProxy(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	// The probe does not force POST back onto a 302 hop, because the runtime
	// proxy does not either. An endpoint reachable only through such a hop
	// therefore fails the probe, as it would fail through the proxy.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/0":
			http.Redirect(w, r, "/1", http.StatusFound)
		case "/1":
			http.Redirect(w, r, "/2", http.StatusFound)
		case "/2":
			http.Redirect(w, r, "/3", http.StatusFound)
		default:
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, http.MethodGet, r.Method, "net/http converts 302 to GET and the probe leaves that alone")
			assert.Empty(t, string(body))
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL+"/0")
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
	require.Equal(t, http.StatusMethodNotAllowed, requireValue(t, result.HTTPStatus))
	require.Equal(t, int32(8), requests.Load(), "server/discover and the initialize fallback each follow three redirects")
}

func TestProbeRemoteMcpURL_RedirectMethodMatrixMatchesProxy(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status      int
		wantMethod  string
		wantBody    bool
		wantOutcome string
	}{
		"moved permanently converts to GET": {status: http.StatusMovedPermanently, wantMethod: http.MethodGet, wantBody: false, wantOutcome: remotemcp.ProbeOutcomeInvalidMCPResponse},
		"found converts to GET":             {status: http.StatusFound, wantMethod: http.MethodGet, wantBody: false, wantOutcome: remotemcp.ProbeOutcomeInvalidMCPResponse},
		"see other converts to GET":         {status: http.StatusSeeOther, wantMethod: http.MethodGet, wantBody: false, wantOutcome: remotemcp.ProbeOutcomeInvalidMCPResponse},
		"temporary keeps POST on-origin":    {status: http.StatusTemporaryRedirect, wantMethod: http.MethodPost, wantBody: true, wantOutcome: remotemcp.ProbeOutcomeMCPAvailable},
		"permanent keeps POST on-origin":    {status: http.StatusPermanentRedirect, wantMethod: http.MethodPost, wantBody: true, wantOutcome: remotemcp.ProbeOutcomeMCPAvailable},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			moved, _ := jsonrpcHandler(respondWithInitialize)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/moved" {
					http.Redirect(w, r, "/moved", tc.status)
					return
				}
				assert.Equal(t, tc.wantMethod, r.Method)
				if !tc.wantBody {
					body, err := io.ReadAll(r.Body)
					assert.NoError(t, err)
					assert.Empty(t, string(body))
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				moved.ServeHTTP(w, r)
			}))
			t.Cleanup(upstream.Close)

			result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
			require.Equal(t, tc.wantOutcome, result.Outcome)
		})
	}
}

func TestProbeRemoteMcpURL_RejectsCrossOriginBodyReplayingRedirect(t *testing.T) {
	t.Parallel()
	for name, status := range map[string]int{
		"temporary": http.StatusTemporaryRedirect,
		"permanent": http.StatusPermanentRedirect,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var targetHits atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				targetHits.Add(1)
				w.WriteHeader(http.StatusAccepted)
			}))
			t.Cleanup(target.Close)

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, status)
			}))
			t.Cleanup(upstream.Close)

			result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
			require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
			require.Equal(t, int32(0), targetHits.Load(), "the probe body must not reach an upstream-chosen host")
		})
	}
}

func TestProbeRemoteMcpURL_ApprovedRedirectPathInitializesThroughTheProxy(t *testing.T) {
	t.Parallel()
	// The point of aligning the two: whatever the probe calls available at
	// setup time, the runtime proxy must be able to initialize through.
	canonical, _ := jsonrpcHandler(respondWithInitialize)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/canonical" {
			http.Redirect(w, r, "/canonical", http.StatusTemporaryRedirect)
			return
		}
		canonical.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)

	policy := newPermissivePolicy(t)
	result := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome)

	p := &remotemcpproxy.Proxy{
		GuardianPolicy:       policy,
		Logger:               testenv.NewLogger(t),
		Tracer:               testenv.NewTracerProvider(t).Tracer("probe-alignment-test"),
		NonStreamingTimeout:  5 * time.Second,
		StreamingTimeout:     5 * time.Second,
		MaxBufferedBodyBytes: remotemcpproxy.DefaultMaxBufferedBodyBytes,
		RemoteURL:            upstream.URL,
		Identity:             remotemcpproxy.ServerIdentity{RemoteMCPServerID: "probe-alignment"},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req), "a probe-approved redirect path must initialize through the proxy")
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `"protocolVersion":"2025-06-18"`)
}

func TestProbeRemoteMcpURL_RejectsFourthRedirect(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Location", fmt.Sprintf("/%d", requests.Load()))
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL+"/0")
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonTransportError, requireValue(t, result.Reason))
	require.Equal(t, int32(8), requests.Load(), "server/discover and the initialize fallback each stop after the fourth hop")
}

func TestProbeRemoteMcpURL_RejectsHTTPSRedirectToHostedHTTP(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "http://8.8.8.8/mcp")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(upstream.Close)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonTransportError, requireValue(t, result.Reason))
	require.Nil(t, result.HTTPStatus)
}

func TestProbeRemoteMcpURL_RejectsRedirectToBlockedHost(t *testing.T) {
	t.Parallel()
	const blockedHost = "blocked.test"
	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(_ context.Context, _, host string) ([]net.IP, error) {
			if host == blockedHost {
				return []net.IP{net.ParseIP("203.0.113.1")}, nil
			}
			return nil, fmt.Errorf("unexpected host: %s", host)
		},
	})
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{"203.0.113.0/24"}, guardian.WithResolver(resolver))
	require.NoError(t, err)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://"+blockedHost+"/initialize")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonGuardianRejected, requireValue(t, result.Reason))
}

func requireValue[T any](t *testing.T, value *T) T {
	t.Helper()
	require.NotNil(t, value)
	return *value
}
