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

func TestCanonicalDirectRemoteURLPreservesSafeQuery(t *testing.T) {
	t.Parallel()

	canonical, err := canonicalDirectRemoteURL("HTTPS://Example.TEST:443/mcp?tenant=example&region=us")

	require.NoError(t, err)
	require.Equal(t, "https://example.test/mcp?tenant=example&region=us", canonical)
}

func TestCanonicalDirectRemoteURLRejectsUnsafeShapes(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{
		"http://example.test/mcp",
		"https://user:password@example.test/mcp",
		"https://example.test/mcp?token=value",
		"https://example.test/mcp?X-Amz-Signature=value",
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

func TestDirectRemoteRequestClassifiesSafeFailureCategories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response func(*http.Request) (*http.Response, error)
		want     SetupCategory
		broad    error
	}{
		{
			name: "unreachable",
			response: func(*http.Request) (*http.Response, error) {
				return nil, &net.DNSError{IsTemporary: true}
			},
			want:  SetupCategoryUnreachable,
			broad: ErrDirectRemoteUnavailable,
		},
		{
			name: "timeout",
			response: func(*http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			},
			want:  SetupCategoryTimeout,
			broad: ErrDirectRemoteUnavailable,
		},
		{
			name: "invalid MCP response",
			response: func(request *http.Request) (*http.Response, error) {
				response := directRemoteTestResponse(request, http.StatusOK, `not-json`)
				return response, nil
			},
			want:  SetupCategoryInvalidMCPResponse,
			broad: ErrDirectRemoteRejected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := &http.Client{Transport: directRemoteTestRoundTripper(test.response)}
			_, _, _, _, err := directRemoteRequest(t.Context(), client, "https://remote.example.test/mcp", "initialize", map[string]any{}, "", &directRemoteResponseBudget{remaining: 1024, requestsRemaining: 1})
			require.ErrorIs(t, err, test.broad)
			require.Equal(t, test.want, setupCategoryFromError(err))
		})
	}
}

func TestGuardianDirectRemoteInspectorClassifiesInvalidURL(t *testing.T) {
	t.Parallel()

	_, err := NewGuardianDirectRemoteInspector(directRemoteTestPolicy(t)).Inspect(t.Context(), "http://remote.example.test/mcp")
	require.ErrorIs(t, err, ErrDirectRemoteRejected)
	require.Equal(t, SetupCategoryInvalidURL, setupCategoryFromError(err))
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

func TestDirectRemoteRedirectCheckDistinguishesUnsafeRedirectFromBudgetExhaustion(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://remote.example.test/redirect", nil)
	require.NoError(t, err)

	budgetErr := directRemoteRedirectCheck(directRemoteTestPolicy(t), &directRemoteResponseBudget{remaining: 1024, requestsRemaining: 0}, request, nil)
	require.ErrorIs(t, budgetErr, ErrDirectRemoteUnavailable)
	require.Equal(t, SetupCategoryTemporarilyUnavailable, setupCategoryFromError(budgetErr))

	via := make([]*http.Request, directRemoteProbeMaxRedirects+1)
	redirectErr := directRemoteRedirectCheck(directRemoteTestPolicy(t), &directRemoteResponseBudget{remaining: 1024, requestsRemaining: 1}, request, via)
	require.ErrorIs(t, redirectErr, ErrDirectRemoteRejected)
	require.Equal(t, SetupCategoryUnsafeTargetOrRedirect, setupCategoryFromError(redirectErr))
}

func TestDirectRemoteRequestClassifiesBudgetExhaustionAsTemporarilyUnavailable(t *testing.T) {
	t.Parallel()

	_, _, _, _, err := directRemoteRequest(t.Context(), http.DefaultClient, "https://remote.example.test/mcp", "initialize", map[string]any{}, "", &directRemoteResponseBudget{remaining: 1024, requestsRemaining: 0})

	require.ErrorIs(t, err, ErrDirectRemoteUnavailable)
	require.Equal(t, SetupCategoryTemporarilyUnavailable, setupCategoryFromError(err))
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

	requests := make([]string, 0, 4)
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
	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp", &directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8})

	require.NoError(t, err)
	require.Equal(t, "available_dcr", result)
	require.Equal(t, []string{
		"https://remote.example.test/.well-known/oauth-protected-resource/mcp",
		"https://first.example.test/.well-known/oauth-authorization-server",
		"https://first.example.test/.well-known/openid-configuration",
		"https://second.example.test/.well-known/oauth-authorization-server",
	}, requests)
}

func TestDirectRemoteOAuthDiscoveryUsesOIDCCompatibleCandidate(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		switch request.URL.String() {
		case "https://remote.example.test/.well-known/oauth-protected-resource/mcp":
			return directRemoteTestResponse(request, http.StatusOK, `{"authorization_servers":["https://issuer.example.test/path"]}`)
		case "https://issuer.example.test/.well-known/oauth-authorization-server/path":
			return directRemoteTestResponse(request, http.StatusNotFound, `{}`)
		case "https://issuer.example.test/.well-known/openid-configuration/path":
			return directRemoteTestResponse(request, http.StatusOK, `{"registration_endpoint":"https://issuer.example.test/register"}`)
		default:
			return directRemoteTestResponse(request, http.StatusNotFound, `{}`)
		}
	})

	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp", &directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8})
	require.NoError(t, err)
	require.Equal(t, "available_dcr", result)
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

	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp", &directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8})
	require.NoError(t, err)
	require.Equal(t, "available", result)
}

func TestDirectRemoteOAuthDiscoveryReportsIncompleteWithoutAuthorizationMetadata(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		return directRemoteTestResponse(request, http.StatusNotFound, `{}`)
	})

	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp", &directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8})
	require.NoError(t, err)
	require.Equal(t, "incomplete", result)
}

func TestDirectRemoteOAuthDiscoveryPreservesAvailableResultWhenLaterProbeExhaustsBudget(t *testing.T) {
	t.Parallel()

	requests := 0
	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		requests++
		switch requests {
		case 1:
			return directRemoteTestResponse(request, http.StatusOK, `{"authorization_servers":["https://issuer.example.test"]}`)
		case 2:
			return directRemoteTestResponse(request, http.StatusOK, `{}`)
		default:
			t.Fatalf("request must not run after the budget is exhausted: %s", request.URL)
			return nil
		}
	})
	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp", &directRemoteResponseBudget{remaining: 4096, requestsRemaining: 2})
	require.NoError(t, err)
	require.Equal(t, "available", result)
	require.Equal(t, 2, requests)
}

func TestDirectRemoteOAuthDiscoveryPropagatesRequestBudgetExhaustion(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		t.Fatalf("request must not run after the budget is exhausted: %s", request.URL)
		return nil
	})
	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp", &directRemoteResponseBudget{remaining: 4096, requestsRemaining: 0})
	require.Empty(t, result)
	require.ErrorIs(t, err, ErrDirectRemoteUnavailable)
	require.Equal(t, SetupCategoryTemporarilyUnavailable, setupCategoryFromError(err))
}

func TestDirectRemoteOAuthDiscoveryPropagatesTransientMetadataStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
			return directRemoteTestResponse(request, status, `{}`)
		})
		result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp", &directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8})
		require.Empty(t, result)
		require.ErrorIs(t, err, ErrDirectRemoteUnavailable)
		require.Equal(t, SetupCategoryTemporarilyUnavailable, setupCategoryFromError(err))
	}
}

func TestDirectRemoteOAuthDiscoveryKeepsNonTransientMissingMetadataIncomplete(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		return directRemoteTestResponse(request, http.StatusNotFound, `{}`)
	})
	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp", &directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8})
	require.NoError(t, err)
	require.Equal(t, "incomplete", result)
}

func TestDirectRemoteOAuthDiscoveryPropagatesByteBudgetExhaustion(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		t.Fatalf("request must not run after the byte budget is exhausted: %s", request.URL)
		return nil
	})
	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), client, "https://remote.example.test/mcp", &directRemoteResponseBudget{remaining: 0, requestsRemaining: 1})
	require.Empty(t, result)
	require.ErrorIs(t, err, ErrDirectRemoteUnavailable)
	require.Equal(t, SetupCategoryTemporarilyUnavailable, setupCategoryFromError(err))
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
	require.True(t, validDirectRemoteRegistrationURL("https://example.test/mcp?tenant=example"))
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
