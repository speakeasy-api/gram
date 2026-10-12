package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
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
	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), clientWithDirectRemoteBudget(t, client, directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8}), "https://remote.example.test/mcp")

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

	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), clientWithDirectRemoteBudget(t, client, directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8}), "https://remote.example.test/mcp")
	require.NoError(t, err)
	require.Equal(t, "available_dcr", result)
}

// A provider with no registration_endpoint that advertises Client ID Metadata
// Document support for public clients is set up automatically, not by hand.
func TestDirectRemoteOAuthDiscoveryReportsCIMDWithoutDCR(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		switch request.URL.String() {
		case "https://remote.example.test/.well-known/oauth-protected-resource/mcp":
			return directRemoteTestResponse(request, http.StatusOK, `{"authorization_servers":["https://auth.example.test"]}`)
		case "https://auth.example.test/.well-known/oauth-authorization-server":
			return directRemoteTestResponse(request, http.StatusOK, `{"issuer":"https://auth.example.test","authorization_endpoint":"https://auth.example.test/authorize","token_endpoint":"https://auth.example.test/token","client_id_metadata_document_supported":true,"token_endpoint_auth_methods_supported":["none"],"code_challenge_methods_supported":["S256"]}`)
		default:
			return directRemoteTestResponse(request, http.StatusNotFound, `{}`)
		}
	})

	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), clientWithDirectRemoteBudget(t, client, directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8}), "https://remote.example.test/mcp")
	require.NoError(t, err)
	require.Equal(t, "available_cimd", result)
}

func TestDirectRemoteAutomaticRegistration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata string
		want     string
	}{
		{name: "dynamic registration", metadata: `{"registration_endpoint":"https://auth.example.test/register"}`, want: "available_dcr"},
		{name: "dynamic registration preferred over CIMD", metadata: `{"registration_endpoint":"https://auth.example.test/register","client_id_metadata_document_supported":true}`, want: "available_dcr"},
		{name: "CIMD without enumerated methods", metadata: `{"client_id_metadata_document_supported":true}`, want: "available_cimd"},
		{name: "plain http registration endpoint", metadata: `{"registration_endpoint":"http://auth.example.test/register"}`, want: ""},
		{name: "relative registration endpoint", metadata: `{"registration_endpoint":"/register"}`, want: ""},
		{name: "dynamic registration without client_secret_basic prefers CIMD", metadata: `{"registration_endpoint":"https://auth.example.test/register","client_id_metadata_document_supported":true,"token_endpoint_auth_methods_supported":["none"]}`, want: "available_cimd"},
		{name: "dynamic registration without client_secret_basic or CIMD", metadata: `{"registration_endpoint":"https://auth.example.test/register","token_endpoint_auth_methods_supported":["none"]}`, want: "available_dcr"},
		{name: "unusable registration endpoint falls back to CIMD", metadata: `{"registration_endpoint":"http://auth.example.test/register","client_id_metadata_document_supported":true}`, want: "available_cimd"},
		{name: "CIMD refusing public clients", metadata: `{"client_id_metadata_document_supported":true,"token_endpoint_auth_methods_supported":["client_secret_basic"]}`, want: ""},
		{name: "CIMD flag not a boolean", metadata: `{"client_id_metadata_document_supported":"true"}`, want: ""},
		{name: "neither", metadata: `{"token_endpoint_auth_methods_supported":["none"]}`, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var metadata map[string]any
			require.NoError(t, json.Unmarshal([]byte(test.metadata), &metadata))
			require.Equal(t, test.want, directRemoteAutomaticRegistration(metadata))
		})
	}
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

	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), clientWithDirectRemoteBudget(t, client, directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8}), "https://remote.example.test/mcp")
	require.NoError(t, err)
	require.Equal(t, "available", result)
}

func TestDirectRemoteOAuthDiscoveryReportsIncompleteWithoutAuthorizationMetadata(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		return directRemoteTestResponse(request, http.StatusNotFound, `{}`)
	})

	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), clientWithDirectRemoteBudget(t, client, directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8}), "https://remote.example.test/mcp")
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
	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), clientWithDirectRemoteBudget(t, client, directRemoteResponseBudget{remaining: 4096, requestsRemaining: 2}), "https://remote.example.test/mcp")
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
	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), clientWithDirectRemoteBudget(t, client, directRemoteResponseBudget{remaining: 4096, requestsRemaining: 0}), "https://remote.example.test/mcp")
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
		result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), clientWithDirectRemoteBudget(t, client, directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8}), "https://remote.example.test/mcp")
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
	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), clientWithDirectRemoteBudget(t, client, directRemoteResponseBudget{remaining: 4096, requestsRemaining: 8}), "https://remote.example.test/mcp")
	require.NoError(t, err)
	require.Equal(t, "incomplete", result)
}

func TestDirectRemoteOAuthDiscoveryPropagatesByteBudgetExhaustion(t *testing.T) {
	t.Parallel()

	client := directRemoteTestClient(t, func(request *http.Request) *http.Response {
		t.Fatalf("request must not run after the byte budget is exhausted: %s", request.URL)
		return nil
	})
	result, err := directRemoteOAuthDiscovery(t.Context(), directRemoteTestPolicy(t), clientWithDirectRemoteBudget(t, client, directRemoteResponseBudget{remaining: 0, requestsRemaining: 1}), "https://remote.example.test/mcp")
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

func clientWithDirectRemoteBudget(t *testing.T, client *http.Client, budget directRemoteResponseBudget) *http.Client {
	t.Helper()
	transport := &directRemoteRoundTripper{base: client.Transport, policy: directRemoteTestPolicy(t), ctx: t.Context(), budget: budget}
	return &http.Client{Transport: transport, CheckRedirect: transport.checkRedirect}
}

func TestDirectRemoteValidationTimeout(t *testing.T) {
	t.Parallel()
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("lookup_%d", failAt), func(t *testing.T) {
			t.Parallel()
			var lookups atomic.Int32
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{
				LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
					if lookups.Add(1) >= int32(failAt) {
						return nil, context.DeadlineExceeded
					}
					return []net.IP{net.ParseIP("8.8.8.8")}, nil
				},
			})))
			require.NoError(t, err)
			client := directRemoteTestClient(t, func(r *http.Request) *http.Response {
				t.Error("a validation timeout must prevent egress")
				return directRemoteTestResponse(r, http.StatusOK, `{}`)
			})
			_, err = NewGuardianDirectRemoteInspector(policy).inspect(t.Context(), "https://remote.example.test/mcp", client)
			require.Equal(t, SetupCategoryTimeout, setupCategoryFromError(err))
		})
	}
}
