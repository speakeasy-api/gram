package platformmcp

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCanonicalDirectRemoteURL(t *testing.T) {
	t.Parallel()

	canonical, err := canonicalDirectRemoteURL(" HTTPS://Example.TEST:443 ")

	require.NoError(t, err)
	require.Equal(t, "https://example.test/", canonical)
}

func TestCanonicalDirectRemoteURLRejectsUnsafeShapes(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{
		"http://example.test/mcp",
		"https://user:password@example.test/mcp",
		"https://example.test/mcp?token=value",
		"https://example.test/mcp#fragment",
		"https://example.test:8443/mcp",
		"https://example.test/{tenant}/mcp",
		"https://example.test/mcp\nX-Header: value",
	} {
		_, err := canonicalDirectRemoteURL(rawURL)
		require.ErrorIs(t, err, ErrDirectRemoteRejected, rawURL)
	}
}

func TestDirectRemoteRequestAcceptsJSONAndSSE(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		require.Equal(t, "application/json, text/event-stream", request.Header.Get("Accept"))
		return directRemoteTestResponse(request, http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	})

	_, _, _, status, err := directRemoteRequest(t.Context(), client, "https://remote.example.test/mcp", "initialize", map[string]any{}, "", &directRemoteResponseBudget{remaining: 1024, requestsRemaining: 1})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
}

func TestDirectRemoteNotificationAcceptsJSONAndSSE(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		require.Equal(t, "application/json, text/event-stream", request.Header.Get("Accept"))
		require.Equal(t, "session-id", request.Header.Get("Mcp-Session-Id"))
		return directRemoteTestResponse(request, http.StatusAccepted, "")
	})

	_, status, err := directRemoteNotification(t.Context(), client, "https://remote.example.test/mcp", "notifications/initialized", map[string]any{}, "session-id", &directRemoteResponseBudget{remaining: 1024, requestsRemaining: 1})

	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, status)
}

func TestDirectRemoteRequestRejectsOversizedSessionID(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		response := directRemoteTestResponse(request, http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{}}`)
		response.Header.Set("Mcp-Session-Id", strings.Repeat("x", directRemoteSessionIDMaxBytes+1))
		return response
	})

	_, _, _, _, err := directRemoteRequest(t.Context(), client, "https://remote.example.test/mcp", "initialize", map[string]any{}, "", &directRemoteResponseBudget{remaining: 1024, requestsRemaining: 1})

	require.ErrorIs(t, err, ErrDirectRemoteRejected)
}

func TestDirectRemoteRequestRejectsOversizedOutboundSessionID(t *testing.T) {
	t.Parallel()

	_, _, _, _, err := directRemoteRequest(context.Background(), http.DefaultClient, "https://remote.example.test/mcp", "tools/list", map[string]any{}, strings.Repeat("x", directRemoteSessionIDMaxBytes+1), &directRemoteResponseBudget{remaining: 1024, requestsRemaining: 1})

	require.ErrorIs(t, err, ErrDirectRemoteRejected)
}

func TestDirectRemoteOAuthDiscoveryScansForDCR(t *testing.T) {
	t.Parallel()

	requests := make([]string, 0, 3)
	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		requests = append(requests, request.URL.String())
		switch request.URL.String() {
		case "https://remote.example.test/.well-known/oauth-protected-resource/mcp":
			return directRemoteTestResponse(request, http.StatusOK, `{"authorization_servers":["https://first.example.test","https://second.example.test"]}`)
		case "https://first.example.test/.well-known/oauth-authorization-server":
			return directRemoteTestResponse(request, http.StatusOK, `{}`)
		case "https://second.example.test/.well-known/oauth-authorization-server":
			return directRemoteTestResponse(request, http.StatusOK, `{"registration_endpoint":"https://second.example.test/register"}`)
		default:
			return directRemoteTestResponse(request, http.StatusNotFound, `{}`)
		}
	})
	result := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp", &directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8})

	require.Equal(t, "available_dcr", result)
	require.Len(t, requests, 3)
}

func TestDirectRemoteOAuthDiscoveryReportsAvailableWithoutDCR(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		switch request.URL.String() {
		case "https://remote.example.test/.well-known/oauth-protected-resource/mcp":
			return directRemoteTestResponse(request, http.StatusOK, `{"authorization_servers":["https://first.example.test","https://second.example.test"]}`)
		case "https://first.example.test/.well-known/oauth-authorization-server", "https://second.example.test/.well-known/oauth-authorization-server":
			return directRemoteTestResponse(request, http.StatusOK, `{}`)
		default:
			return directRemoteTestResponse(request, http.StatusNotFound, `{}`)
		}
	})

	require.Equal(t, "available", directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp", &directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8}))
}

func TestDirectRemoteOAuthDiscoveryReportsIncompleteWithoutAuthorizationMetadata(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		return directRemoteTestResponse(request, http.StatusNotFound, `{}`)
	})

	require.Equal(t, "incomplete", directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp", &directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8}))
}

func directRemoteTestPolicy(t *testing.T) *guardian.Policy {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{}, guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		},
	})))
	require.NoError(t, err)
	return policy
}

func TestValidDirectRemoteRegistrationURLRequiresCanonicalForm(t *testing.T) {
	t.Parallel()

	require.True(t, validDirectRemoteRegistrationURL("https://example.test/mcp"))
	require.False(t, validDirectRemoteRegistrationURL("https://Example.test/mcp"))
	require.False(t, validDirectRemoteRegistrationURL("https://example.test:443/mcp"))
}

type directRemoteTestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip directRemoteTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func directRemoteTestClient(t *testing.T, response func(*http.Request) *http.Response) *http.Client {
	t.Helper()
	return &http.Client{Transport: directRemoteTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		return response(request), nil
	})}
}

func directRemoteTestResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}
