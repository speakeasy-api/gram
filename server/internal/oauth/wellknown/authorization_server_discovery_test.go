package wellknown

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestAuthorizationServerDiscoveryRejectsInvalidIssuerURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		issuer  string
		wantErr string
	}{
		{"http", "http://auth.example.com", "HTTPS"},
		{"userinfo", "https://user@auth.example.com", "userinfo"},
		{"query", "https://auth.example.com?x=1", "query"},
		{"empty query", "https://auth.example.com?", "query"},
		{"fragment", "https://auth.example.com#x", "fragment"},
		{"empty fragment", "https://auth.example.com#", "fragment"},
	}

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			metadata, err := DiscoverAuthorizationServerMetadata(t.Context(), policy, tt.issuer)
			require.ErrorContains(t, err, tt.wantErr)
			require.Nil(t, metadata)
		})
	}
}

func TestAuthorizationServerDiscoveryBoundsInitialValidation(t *testing.T) {
	t.Parallel()

	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(ctx context.Context, _, _ string) ([]net.IP, error) {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.LessOrEqual(t, time.Until(deadline), authorizationServerDiscoveryTimeout)
			return nil, errors.New("stop after deadline inspection")
		},
	})
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), guardian.WithResolver(resolver))

	metadata, err := DiscoverAuthorizationServerMetadata(t.Context(), policy, "https://auth.example.com")
	require.ErrorContains(t, err, "stop after deadline inspection")
	require.Nil(t, metadata)
}

func TestAuthorizationServerDiscoveryInitialValidationRespectsCallerCancellation(t *testing.T) {
	t.Parallel()

	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(ctx context.Context, _, _ string) ([]net.IP, error) {
			return nil, ctx.Err()
		},
	})
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), guardian.WithResolver(resolver))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	metadata, err := DiscoverAuthorizationServerMetadata(ctx, policy, "https://auth.example.com")
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, metadata)
}

func TestAuthorizationServerDiscoveryUsesRFC8414PathAndReturnsMetadata(t *testing.T) {
	t.Parallel()

	var requestedPath string
	var issuer string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/authorize",
			"token_endpoint":         issuer + "/token",
			"registration_endpoint":  issuer + "/register",
			"authorization_response_iss_parameter_supported": true,
		})
	}))
	t.Cleanup(server.Close)

	issuer, policy := authorizationServerTestPolicy(t, server, "/tenant%2Fexample")
	metadata, err := DiscoverAuthorizationServerMetadata(t.Context(), policy, issuer)
	require.NoError(t, err)
	require.Equal(t, "/.well-known/oauth-authorization-server/tenant%2Fexample", requestedPath)
	require.Equal(t, issuer, metadata.Issuer)
	require.Equal(t, issuer+"/authorize", metadata.AuthorizationEndpoint)
	require.Equal(t, issuer+"/token", metadata.TokenEndpoint)
	require.Equal(t, issuer+"/register", metadata.RegistrationEndpoint)
	require.True(t, metadata.AuthorizationResponseIssParameterSupported)
	require.JSONEq(t, fmt.Sprintf(`{
		"issuer": %q,
		"authorization_endpoint": %q,
		"token_endpoint": %q,
		"registration_endpoint": %q,
		"authorization_response_iss_parameter_supported": true
	}`, issuer, issuer+"/authorize", issuer+"/token", issuer+"/register"), string(metadata.RawMetadata))
}

func TestAuthorizationServerDiscoveryRemovesTrailingSlashesFromMetadataPath(t *testing.T) {
	t.Parallel()

	var requestedPath string
	var issuer string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.EscapedPath()
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "authorize",
			"token_endpoint":         issuer + "token",
		})
	}))
	t.Cleanup(server.Close)

	issuer, policy := authorizationServerTestPolicy(t, server, "/tenant//")
	metadata, err := DiscoverAuthorizationServerMetadata(t.Context(), policy, issuer)
	require.NoError(t, err)
	require.Equal(t, "/.well-known/oauth-authorization-server/tenant", requestedPath)
	require.Equal(t, issuer, metadata.Issuer)
}

func TestAuthorizationServerDiscoveryRequiresExactIssuerEquality(t *testing.T) {
	t.Parallel()

	var issuer string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 issuer + "/",
			"authorization_endpoint": issuer + "/authorize",
			"token_endpoint":         issuer + "/token",
		})
	}))
	t.Cleanup(server.Close)

	issuer, policy := authorizationServerTestPolicy(t, server, "")
	metadata, err := DiscoverAuthorizationServerMetadata(t.Context(), policy, issuer)
	require.EqualError(t, err, fmt.Sprintf("discovered authorization server issuer %q does not match configured issuer %q", issuer+"/", issuer))
	require.Nil(t, metadata)
}

func TestAuthorizationServerDiscoveryRevalidatesRedirects(t *testing.T) {
	t.Parallel()

	var redirected atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			redirected.Store(true)
			return
		}
		http.Redirect(w, r, "http://auth.example.com"+r.URL.RequestURI(), http.StatusFound)
	}))
	t.Cleanup(server.Close)

	issuer, policy := authorizationServerTestPolicy(t, server, "")
	metadata, err := DiscoverAuthorizationServerMetadata(t.Context(), policy, issuer)
	require.ErrorContains(t, err, "scheme must be https")
	require.Nil(t, metadata)
	require.False(t, redirected.Load())
}

func TestAuthorizationServerDiscoveryRejectsBlockedRedirectAddress(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://blocked.example.com/metadata", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(serverURL.Host)
	require.NoError(t, err)
	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(_ context.Context, _, host string) ([]net.IP, error) {
			if host == "blocked.example.com" {
				return []net.IP{net.ParseIP("10.0.0.1")}, nil
			}
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		},
	})
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	policy, err := guardian.NewUnsafePolicy(
		testenv.NewTracerProvider(t),
		[]string{"10.0.0.0/8"},
		guardian.WithResolver(resolver),
		guardian.WithTLSRootCAs(roots),
	)
	require.NoError(t, err)

	metadata, err := DiscoverAuthorizationServerMetadata(t.Context(), policy, "https://auth.example.com:"+port)
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
	require.Nil(t, metadata)
}

func TestAuthorizationServerDiscoveryRejectsBadResponses(t *testing.T) {
	t.Parallel()
	require.Equal(t, 1<<20, authorizationServerDiscoveryMaxBodyBytes)

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{"non-2xx", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }, "HTTP 502"},
		{"malformed JSON", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{`)) }, "decode"},
		{"oversized body", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", authorizationServerDiscoveryMaxBodyBytes+1)))
		}, "exceeded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(tt.handler)
			t.Cleanup(server.Close)
			issuer, policy := authorizationServerTestPolicy(t, server, "")

			metadata, err := DiscoverAuthorizationServerMetadata(t.Context(), policy, issuer)
			require.ErrorContains(t, err, tt.wantErr)
			require.Nil(t, metadata)
		})
	}
}

func TestAuthorizationServerDiscoveryRejectsMissingOrInvalidRequiredEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata string
		wantErr  string
	}{
		{"missing authorization endpoint", `{"issuer":%q,"token_endpoint":"https://auth.example.com/token"}`, "missing authorization_endpoint"},
		{"missing token endpoint", `{"issuer":%q,"authorization_endpoint":"https://auth.example.com/authorize"}`, "missing token_endpoint"},
		{"invalid authorization endpoint", `{"issuer":%q,"authorization_endpoint":"http://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token"}`, "invalid authorization_endpoint"},
		{"invalid token endpoint", `{"issuer":%q,"authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"http://auth.example.com/token"}`, "invalid token_endpoint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var response string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, response)
			}))
			t.Cleanup(server.Close)
			issuer, policy := authorizationServerTestPolicy(t, server, "")
			response = fmt.Sprintf(tt.metadata, issuer)

			metadata, err := DiscoverAuthorizationServerMetadata(t.Context(), policy, issuer)
			require.ErrorContains(t, err, tt.wantErr)
			require.Nil(t, metadata)
		})
	}
}

func TestAuthorizationServerDiscoveryValidatesRequiredEndpointsInOrder(t *testing.T) {
	t.Parallel()

	var response string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, response)
	}))
	t.Cleanup(server.Close)
	issuer, policy := authorizationServerTestPolicy(t, server, "")
	response = fmt.Sprintf(`{"issuer":%q,"authorization_endpoint":"http://auth.example.com/authorize","token_endpoint":"http://auth.example.com/token"}`, issuer)

	metadata, err := DiscoverAuthorizationServerMetadata(t.Context(), policy, issuer)
	require.ErrorContains(t, err, "invalid authorization_endpoint")
	require.Nil(t, metadata)
}

func TestAuthorizationServerDiscoveryRejectsBlockedAddresses(t *testing.T) {
	t.Parallel()

	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("10.0.0.1")}, nil
		},
	})
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), guardian.WithResolver(resolver))

	metadata, err := DiscoverAuthorizationServerMetadata(t.Context(), policy, "https://blocked.example.com")
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
	require.Nil(t, metadata)
}

func authorizationServerTestPolicy(t *testing.T, server *httptest.Server, escapedPath string) (string, *guardian.Policy) {
	t.Helper()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(serverURL.Host)
	require.NoError(t, err)

	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		},
	})
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	policy, err := guardian.NewUnsafePolicy(
		testenv.NewTracerProvider(t),
		nil,
		guardian.WithResolver(resolver),
		guardian.WithTLSRootCAs(roots),
	)
	require.NoError(t, err)

	path, err := url.PathUnescape(escapedPath)
	require.NoError(t, err)
	return (&url.URL{Scheme: "https", Host: net.JoinHostPort("auth.example.com", port), Path: path, RawPath: escapedPath}).String(), policy
}
