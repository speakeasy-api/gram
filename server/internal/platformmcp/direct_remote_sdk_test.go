package platformmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// directRemoteHTTPInspector exercises the production inspection with a TLS
// fixture dialer; the public endpoint identity still passes every URL guard.
type directRemoteHTTPInspector struct {
	inspector *GuardianDirectRemoteInspector
	transport http.RoundTripper
}

func (i *directRemoteHTTPInspector) Inspect(ctx context.Context, rawURL string) (DirectRemoteInspection, error) {
	return i.inspector.inspect(ctx, rawURL, &http.Client{Transport: i.transport})
}

func directRemoteHTTPFixture(t *testing.T, handler http.Handler) *directRemoteHTTPInspector {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	base, ok := server.Client().Transport.(*http.Transport)
	require.True(t, ok)
	transport := base.Clone()
	transport.TLSClientConfig.ServerName = "127.0.0.1"
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &directRemoteHTTPInspector{inspector: NewGuardianDirectRemoteInspector(directRemoteTestPolicy(t)), transport: transport}
}

func directRemoteProtocolFixture(t *testing.T, mode string) (*directRemoteHTTPInspector, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var methods []string
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "example", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	modern := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true})
	inspector := directRemoteHTTPFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("authorization leaked")
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/cleanup" {
				_, _ = io.WriteString(w, `{}`)
				return
			}
			if r.URL.Path == "/.well-known/oauth-protected-resource/mcp" {
				_, _ = io.WriteString(w, `{"authorization_servers":["https://remote.example.test"]}`)
			} else {
				_, _ = io.WriteString(w, `{"registration_endpoint":"https://remote.example.test/register"}`)
			}
			return
		}
		if r.Method == http.MethodDelete {
			if r.Header.Get("Mcp-Session-Id") != "fixture-session" {
				t.Error("cleanup must identify the established session")
			}
			mu.Lock()
			methods = append(methods, "DELETE")
			mu.Unlock()
			if mode == "stateful-auth-cleanup" {
				http.Redirect(w, r, "/cleanup", http.StatusSeeOther)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		methods = append(methods, request.Method)
		mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		if mode == "auth401" || mode == "auth403" || ((mode == "discover-auth" || strings.HasPrefix(mode, "discover-blocked")) && request.Method == "server/discover") || (mode == "tools-auth" && request.Method == "tools/list") {
			status := http.StatusUnauthorized
			if mode == "auth403" {
				status = http.StatusForbidden
			}
			w.WriteHeader(status)
			return
		}
		if mode == "modern" || mode == "tools-auth" || mode == "discover-auth" {
			if request.Method == "initialize" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unknown method"}}`, request.ID)
				return
			}
			modern.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "server/discover":
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unknown method"}}`, request.ID)
		case "initialize":
			if strings.HasPrefix(mode, "stateful") {
				w.Header().Set("Mcp-Session-Id", "fixture-session")
			}
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"legacy","version":"1"}}}`, request.ID)
		case "notifications/initialized":
			if mode == "stateful-auth-cleanup" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			if mode == "legacy-no-content" || mode == "legacy-empty-ok" {
				w.Header().Del("Content-Type")
				if mode == "legacy-no-content" {
					w.WriteHeader(http.StatusNoContent)
				} else {
					w.WriteHeader(http.StatusOK)
				}
				return
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if mode == "discover-blocked-invalid-tools" {
				_, _ = io.WriteString(w, `not-json`)
				return
			}
			if mode == "stateful-rejected" {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {}\n\n")
				return
			}
			if strings.HasPrefix(mode, "stateful") {
				if r.Header.Get("Mcp-Session-Id") != "fixture-session" {
					t.Error("missing session")
				}
			}
			if r.Header.Get("Mcp-Protocol-Version") != "2025-06-18" {
				t.Error("wrong legacy protocol")
			}
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"example","inputSchema":{"type":"object"}}],"nextCursor":"do-not-follow"}}`, request.ID)
		default:
			t.Errorf("unexpected method: %s", request.Method)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	return inspector, func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(methods) }
}

func TestDirectRemoteSDKProtocolSequence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode    string
		methods []string
	}{
		{"modern", []string{"server/discover", "tools/list"}},
		{"legacy", []string{"server/discover", "initialize", "notifications/initialized", "tools/list"}},
		{"legacy-no-content", []string{"server/discover", "initialize", "notifications/initialized", "tools/list"}},
		{"legacy-empty-ok", []string{"server/discover", "initialize", "notifications/initialized", "tools/list"}},
		{"stateful", []string{"server/discover", "initialize", "notifications/initialized", "tools/list", "DELETE"}},
		{"discover-blocked", []string{"server/discover", "initialize", "notifications/initialized", "tools/list"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			inspector, methods := directRemoteProtocolFixture(t, tc.mode)
			result, err := inspector.Inspect(t.Context(), "https://remote.example.test/mcp")
			require.NoError(t, err)
			require.Equal(t, []string{"example"}, result.ToolNames)
			require.Equal(t, tc.methods, methods())
		})
	}
}

func TestDirectRemoteSDKRejectsSSEWithoutFallback(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	inspector := directRemoteHTTPFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message\ndata: {}\n\n")
	}))
	_, err := inspector.Inspect(t.Context(), "https://remote.example.test/mcp")
	require.Equal(t, SetupCategoryInvalidMCPResponse, setupCategoryFromError(err))
	require.Equal(t, int32(1), requests.Load())
}

func TestDirectRemoteSDKRejectsUnsafeRedirectWithoutFallback(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	inspector := directRemoteHTTPFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "https://remote.example.test/mcp?token=secret", http.StatusTemporaryRedirect)
	}))
	_, err := inspector.Inspect(t.Context(), "https://remote.example.test/mcp")
	require.Equal(t, SetupCategoryUnsafeTargetOrRedirect, setupCategoryFromError(err))
	require.Equal(t, int32(1), requests.Load())
}

func TestDirectRemoteSDKBodyReadDeadline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		var requests atomic.Int32
		inspector := &directRemoteHTTPInspector{
			inspector: NewGuardianDirectRemoteInspector(directRemoteTestPolicy(t)),
			transport: directRemoteTestRoundTripper(func(r *http.Request) (*http.Response, error) {
				requests.Add(1)
				reader, writer := io.Pipe()
				context.AfterFunc(r.Context(), func() { _ = writer.CloseWithError(r.Context().Err()) })
				response := directRemoteTestResponse(r, http.StatusOK, "")
				response.Body = reader
				return response, nil
			}),
		}
		finished := make(chan error, 1)
		go func() { _, err := inspector.Inspect(ctx, "https://remote.example.test/mcp"); finished <- err }()
		// All goroutines are blocked: the response is available and its body read
		// is waiting on the pipe. Advance fake time only after proving that state.
		synctest.Wait()
		require.Equal(t, int32(1), requests.Load())
		require.NoError(t, ctx.Err())
		<-time.After(time.Second)
		require.Equal(t, SetupCategoryTimeout, setupCategoryFromError(<-finished))
	})
}

func TestDirectRemoteTransportAggregateByteBudget(t *testing.T) {
	t.Parallel()
	calls := 0
	client := directRemoteTestClient(t, func(r *http.Request) *http.Response {
		calls++
		return directRemoteTestResponse(r, http.StatusOK, strings.Repeat("x", directRemoteProbeMaxBytes/2))
	})
	client = clientWithDirectRemoteBudget(t, client, directRemoteResponseBudget{remaining: directRemoteProbeMaxBytes, requestsRemaining: directRemoteProbeMaxRequests})
	for range 2 {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://remote.example.test/mcp", nil)
		require.NoError(t, err)
		response, err := client.Do(request)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, "https://remote.example.test/mcp", nil)
	require.NoError(t, err)
	response, err := client.Do(request)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.Equal(t, SetupCategoryTemporarilyUnavailable, setupCategoryFromError(err))
	require.Equal(t, 2, calls)
}

func TestDirectRemoteTransportCountsRedirectsAndCleanup(t *testing.T) {
	t.Parallel()
	calls := 0
	client := directRemoteTestClient(t, func(r *http.Request) *http.Response {
		calls++
		if r.Header.Get("Authorization") != "" {
			t.Error("authorization leaked")
		}
		response := directRemoteTestResponse(r, http.StatusOK, `{}`)
		if r.URL.Path == "/mcp" {
			response.StatusCode = http.StatusTemporaryRedirect
			response.Header.Set("Location", "https://other.example.test/final")
		}
		return response
	})
	client = clientWithDirectRemoteBudget(t, client, directRemoteResponseBudget{remaining: directRemoteProbeMaxBytes, requestsRemaining: directRemoteProbeMaxRequests})
	for range 3 {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://remote.example.test/mcp", nil)
		require.NoError(t, err)
		request.Header.Set("Authorization", "must-not-leave")
		response, err := client.Do(request)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, "https://remote.example.test/mcp", nil)
	require.NoError(t, err)
	response, err := client.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, directRemoteProbeMaxRequests, calls)
	response, err = client.Do(request)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.Equal(t, SetupCategoryTemporarilyUnavailable, setupCategoryFromError(err))
	require.Equal(t, directRemoteProbeMaxRequests, calls)
}

func TestDirectRemoteSDKRejectsOversizedBodyWithoutFallback(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	inspector := directRemoteHTTPFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, strings.Repeat("x", directRemoteProbeMaxBytes+1))
	}))
	_, err := inspector.Inspect(t.Context(), "https://remote.example.test/mcp")
	require.Equal(t, SetupCategoryTemporarilyUnavailable, setupCategoryFromError(err))
	require.Equal(t, int32(1), requests.Load())
}

func TestDirectRemoteSDKRedirectLimitIsTerminal(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	inspector := directRemoteHTTPFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "/loop", http.StatusTemporaryRedirect)
	}))
	_, err := inspector.Inspect(t.Context(), "https://remote.example.test/mcp")
	require.Equal(t, SetupCategoryUnsafeTargetOrRedirect, setupCategoryFromError(err))
	require.Equal(t, int32(directRemoteProbeMaxRedirects+1), requests.Load())
}

func TestDirectRemoteTransportCleanupCancellationPreservesObservation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	transport := &directRemoteRoundTripper{
		base: directRemoteTestRoundTripper(func(r *http.Request) (*http.Response, error) {
			close(started)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}),
		policy: directRemoteTestPolicy(t), ctx: ctx,
		budget:   directRemoteResponseBudget{remaining: directRemoteProbeMaxBytes, requestsRemaining: directRemoteProbeMaxRequests},
		finalURL: "https://remote.example.test/mcp", status: http.StatusForbidden,
	}
	finished := make(chan error, 1)
	request, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodDelete, "https://remote.example.test/mcp", nil)
	require.NoError(t, err)
	go func() {
		response, err := transport.RoundTrip(request)
		if response != nil {
			_ = response.Body.Close()
		}
		finished <- err
	}()
	<-started
	cancel()
	select {
	case err := <-finished:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("detached cleanup ignored inspection cancellation")
	}
	_, status, _, failure := transport.observation()
	require.Equal(t, http.StatusForbidden, status)
	require.NoError(t, failure)
}

func TestDirectRemoteTransportSafeFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		response func(*http.Request) (*http.Response, error)
		category SetupCategory
	}{
		{"network", func(*http.Request) (*http.Response, error) { return nil, &net.DNSError{IsTemporary: true} }, SetupCategoryUnreachable},
		{"deadline", func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded }, SetupCategoryTimeout},
		{"oversized session", func(r *http.Request) (*http.Response, error) {
			response := directRemoteTestResponse(r, http.StatusOK, `{}`)
			response.Header.Set("Mcp-Session-Id", strings.Repeat("x", directRemoteSessionIDMaxBytes+1))
			return response, nil
		}, SetupCategoryInvalidMCPResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := clientWithDirectRemoteBudget(t, &http.Client{Transport: directRemoteTestRoundTripper(tc.response)}, directRemoteResponseBudget{remaining: directRemoteProbeMaxBytes, requestsRemaining: directRemoteProbeMaxRequests})
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://remote.example.test/mcp", nil)
			require.NoError(t, err)
			response, err := client.Do(request)
			if response != nil {
				require.NoError(t, response.Body.Close())
			}
			require.Equal(t, tc.category, setupCategoryFromError(err))
		})
	}
}

func TestDirectRemoteTransportRejectsOversizedOutboundSession(t *testing.T) {
	t.Parallel()
	client := clientWithDirectRemoteBudget(t, directRemoteTestClient(t, func(*http.Request) *http.Response {
		t.Error("oversized session must not leave the transport")
		return nil
	}), directRemoteResponseBudget{remaining: directRemoteProbeMaxBytes, requestsRemaining: directRemoteProbeMaxRequests})
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://remote.example.test/mcp", nil)
	require.NoError(t, err)
	request.Header.Set("Mcp-Session-Id", strings.Repeat("x", directRemoteSessionIDMaxBytes+1))
	response, err := client.Do(request)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.Equal(t, SetupCategoryInvalidMCPResponse, setupCategoryFromError(err))
}

func TestDirectRemoteTransportCleanupRedirectPreservesObservation(t *testing.T) {
	t.Parallel()
	calls := 0
	transport := &directRemoteRoundTripper{
		base: directRemoteTestRoundTripper(func(r *http.Request) (*http.Response, error) {
			calls++
			response := directRemoteTestResponse(r, http.StatusBadGateway, `{}`)
			if r.Method == http.MethodDelete {
				response.StatusCode = http.StatusSeeOther
				response.Header.Set("Location", "https://remote.example.test/cleanup")
			}
			return response, nil
		}),
		policy: directRemoteTestPolicy(t), ctx: t.Context(),
		budget:   directRemoteResponseBudget{remaining: directRemoteProbeMaxBytes, requestsRemaining: directRemoteProbeMaxRequests},
		finalURL: "https://remote.example.test/mcp", status: http.StatusForbidden,
	}
	client := &http.Client{Transport: transport, CheckRedirect: transport.checkRedirect}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, "https://remote.example.test/mcp", nil)
	require.NoError(t, err)
	response, err := client.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 2, calls)
	finalURL, status, _, failure := transport.observation()
	require.Equal(t, "https://remote.example.test/mcp", finalURL)
	require.Equal(t, http.StatusForbidden, status)
	require.NoError(t, failure)
}

func TestDirectRemoteSDKRejectsRedirectedSSE(t *testing.T) {
	t.Parallel()
	inspector := directRemoteHTTPFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				return
			}
			http.Redirect(w, r, "/stream?method="+request.Method+"&id="+string(request.ID), http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		result := `{"type":"complete","supportedVersions":["2026-07-28"],"capabilities":{"tools":{}}}`
		if r.URL.Query().Get("method") == "tools/list" {
			result = `{"type":"complete","tools":[]}`
		}
		_, _ = fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", r.URL.Query().Get("id"), result)
	}))
	_, err := inspector.Inspect(t.Context(), "https://remote.example.test/mcp")
	require.Equal(t, SetupCategoryInvalidMCPResponse, setupCategoryFromError(err))
}

func TestDirectRemoteSDKRejectedToolsStillClosesSession(t *testing.T) {
	t.Parallel()
	inspector, methods := directRemoteProtocolFixture(t, "stateful-rejected")
	_, err := inspector.Inspect(t.Context(), "https://remote.example.test/mcp")
	require.Equal(t, SetupCategoryInvalidMCPResponse, setupCategoryFromError(err))
	require.Equal(t, []string{"server/discover", "initialize", "notifications/initialized", "tools/list", "DELETE"}, methods())
}

func TestDirectRemoteOAuthDiscoveryRecoversFromMetadataTransportFailure(t *testing.T) {
	t.Parallel()
	var requests []string
	base := &http.Client{Transport: directRemoteTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			return nil, &net.DNSError{IsTemporary: true}
		case "/.well-known/oauth-protected-resource":
			return directRemoteTestResponse(r, http.StatusOK, `{"authorization_servers":["https://remote.example.test"]}`), nil
		default:
			return directRemoteTestResponse(r, http.StatusOK, `{"registration_endpoint":"https://remote.example.test/register"}`), nil
		}
	})}
	client := clientWithDirectRemoteBudget(t, base, directRemoteResponseBudget{remaining: directRemoteProbeMaxBytes, requestsRemaining: directRemoteProbeMaxRequests})
	discovery, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp")
	require.NoError(t, err)
	require.Equal(t, "available_dcr", discovery)
	require.Equal(t, []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server"}, requests)
}

func TestDirectRemoteSDKSuccessfulFallbackDiscardsDiscoveryAuthentication(t *testing.T) {
	t.Parallel()
	inspector, methods := directRemoteProtocolFixture(t, "discover-blocked-invalid-tools")
	_, err := inspector.Inspect(t.Context(), "https://remote.example.test/mcp")
	require.Equal(t, SetupCategoryInvalidMCPResponse, setupCategoryFromError(err))
	require.Equal(t, []string{"server/discover", "initialize", "notifications/initialized", "tools/list"}, methods())
}

func TestDirectRemoteSDKRejectsNonJSONBodyWithoutFallback(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusAccepted} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int32
			inspector := directRemoteHTTPFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{}`)
			}))
			_, err := inspector.Inspect(t.Context(), "https://remote.example.test/mcp")
			require.Equal(t, SetupCategoryInvalidMCPResponse, setupCategoryFromError(err))
			require.Equal(t, int32(1), requests.Load())
		})
	}
}

func TestDirectRemoteSDKRejectsEmptyCallResponses(t *testing.T) {
	t.Parallel()
	inspector := directRemoteHTTPFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	_, err := inspector.Inspect(t.Context(), "https://remote.example.test/mcp")
	require.Equal(t, SetupCategoryInvalidMCPResponse, setupCategoryFromError(err))
}
