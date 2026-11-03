package catalogauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestDiscoverPublicAuthCapabilities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                                    string
		dcr, cimd                                               bool
		methods                                                 []string
		issuerFailure, partial, missingResource, tooManyIssuers bool
		mismatch, missingEndpoints, resourceMismatch            bool
		originFallback                                          bool
		want                                                    Discovery
	}{
		{name: "DCR", dcr: true, want: Discovery{SupportsDCR: true, AuthSetup: "dcr", Status: "complete"}},
		{name: "CIMD", cimd: true, methods: []string{"none"}, want: Discovery{SupportsCIMD: true, AuthSetup: "cimd", Status: "complete"}},
		{name: "both", dcr: true, cimd: true, want: Discovery{SupportsDCR: true, SupportsCIMD: true, AuthSetup: "dcr", Status: "complete"}},
		{name: "manual", want: Discovery{AuthSetup: "manual", Status: "complete"}},
		{name: "incompatible CIMD", cimd: true, methods: []string{"client_secret_basic"}, want: Discovery{AuthSetup: "manual", Status: "complete"}},
		{name: "issuer unavailable", issuerFailure: true, want: Discovery{AuthSetup: "unknown", Status: "partial"}},
		{name: "partially unreadable issuer", partial: true, dcr: true, want: Discovery{AuthSetup: "unknown", Status: "partial"}},
		{name: "resource absent", missingResource: true, want: Unknown()},
		{name: "issuer mismatch", mismatch: true, want: Discovery{AuthSetup: "unknown", Status: "partial"}},
		{name: "missing endpoints", missingEndpoints: true, want: Discovery{AuthSetup: "unknown", Status: "partial"}},
		{name: "speculative origin fallback", originFallback: true, dcr: true, cimd: true, want: Discovery{AuthSetup: "unknown", Status: "partial"}},
		{name: "resource mismatch", resourceMismatch: true, want: Discovery{AuthSetup: "unknown", Status: "partial"}},
		{name: "bounded issuers", tooManyIssuers: true, want: Discovery{AuthSetup: "unknown", Status: "partial"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var unexpected atomic.Bool
			var issuerRequests atomic.Int32
			var endpoint string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					unexpected.Store(true)
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/.well-known/oauth-protected-resource" {
					if tc.missingResource {
						http.NotFound(w, r)
						return
					}
					issuers := []string{endpoint}
					if tc.tooManyIssuers {
						issuers = []string{endpoint + "/a", endpoint + "/b"}
					}
					resource := endpoint
					if tc.originFallback {
						resource += "/mcp"
					}
					if tc.resourceMismatch {
						resource += "/other"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"resource": resource, "authorization_servers": issuers})
					return
				}
				if r.URL.Path == "/.well-known/oauth-authorization-server" || r.URL.Path == "/.well-known/openid-configuration" {
					issuerRequests.Add(1)
					if tc.issuerFailure || (tc.partial && r.URL.Path == "/.well-known/openid-configuration") {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					doc := map[string]any{"issuer": endpoint, "authorization_endpoint": endpoint + "/authorize", "token_endpoint": endpoint + "/token", "client_id_metadata_document_supported": tc.cimd}
					if tc.mismatch {
						doc["issuer"] = endpoint + "/other"
					}
					if tc.missingEndpoints {
						delete(doc, "token_endpoint")
					}
					if tc.dcr {
						doc["registration_endpoint"] = endpoint + "/register"
					}
					if tc.methods != nil {
						doc["token_endpoint_auth_methods_supported"] = tc.methods
					}
					_ = json.NewEncoder(w).Encode(doc)
					return
				}
				if tc.originFallback && r.URL.Path == "/.well-known/oauth-protected-resource/mcp" {
					http.NotFound(w, r)
					return
				}
				unexpected.Store(true)
				http.NotFound(w, r)
			}))
			t.Cleanup(server.Close)
			endpoint = server.URL
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)
			resourceURL := endpoint
			if tc.originFallback {
				resourceURL += "/mcp"
			}
			actual := Discover(t.Context(), policy, resourceURL, "oauth")
			require.Equal(t, tc.want, actual)
			require.False(t, unexpected.Load(), "discovery must only read metadata without credentials or registration")
			if tc.tooManyIssuers || tc.originFallback {
				require.Zero(t, issuerRequests.Load())
			}
			encoded, err := json.Marshal(actual)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), endpoint)
		})
	}
}

func TestDiscoverExplicitNoAuthAndCancellation(t *testing.T) {
	t.Parallel()
	require.Equal(t, Discovery{AuthSetup: "none", Status: "not_required"}, Discover(t.Context(), nil, "https://example.test/mcp", "none"))
	require.Equal(t, Unknown(), Discover(t.Context(), nil, "https://example.test/mcp", ""))
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Equal(t, Unknown(), Discover(ctx, policy, "https://example.test/mcp", "oauth"))
}
