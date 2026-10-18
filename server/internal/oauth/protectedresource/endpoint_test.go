package protectedresource_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDiscoverEndpointResource(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		pathDoc          bool
		pathOwns         bool
		query            bool
		foreign          bool
		malformed        bool
		canonicalMissing bool
		missing          bool
		noIssuer         bool
		pathFault        bool
		wantError        bool
		wantPath         string
	}{
		{name: "origin metadata owns parent resource"},
		{name: "transport declaration confirmed at canonical location", pathDoc: true},
		{name: "path resource stays exact", pathDoc: true, pathOwns: true, wantPath: "/mcp"},
		{name: "query resource stays exact", pathDoc: true, pathOwns: true, query: true, wantPath: "/mcp?tenant=example"},
		{name: "missing metadata retains explicit endpoint", missing: true, wantPath: "/mcp"},
		{name: "canonical confirmation missing", pathDoc: true, canonicalMissing: true, wantError: true},
		{name: "foreign resource refused", foreign: true, wantError: true},
		{name: "malformed metadata refused", malformed: true, wantError: true},
		{name: "unadvertised authorization refused", noIssuer: true, wantError: true},
		{name: "origin 404 after a failed transport probe is not absence", pathFault: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var transportCalls atomic.Int32
			var canonicalCalls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/.well-known/oauth-protected-resource" {
					canonicalCalls.Add(1)
				}
				if r.URL.Path == "/mcp" {
					transportCalls.Add(1)
					http.Error(w, "transport must not be probed", http.StatusBadRequest)
					return
				}
				if tc.pathFault {
					if r.URL.Path == "/.well-known/oauth-protected-resource/mcp" {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					http.NotFound(w, r)
					return
				}
				if (r.URL.Path != "/.well-known/oauth-protected-resource" && r.URL.Path != "/.well-known/oauth-protected-resource/mcp") || tc.missing || (tc.canonicalMissing && r.URL.Path == "/.well-known/oauth-protected-resource") || (!tc.pathDoc && r.URL.Path != "/.well-known/oauth-protected-resource") {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.malformed {
					_, _ = w.Write([]byte("{"))
					return
				}
				resource := "https://" + r.Host
				if tc.pathOwns {
					resource += "/mcp"
				}
				if tc.query {
					resource += "?tenant=example"
				}
				if tc.foreign {
					resource = "https://foreign.example.test"
				}
				var issuers []string
				if !tc.noIssuer {
					issuers = []string{"https://issuer.example.test"}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"resource": resource, "authorization_servers": issuers})
			}))
			t.Cleanup(server.Close)
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(roots))
			require.NoError(t, err)
			resource, err := protectedresource.DiscoverEndpointResource(t.Context(), policy, server.URL+"/mcp")
			if tc.wantError {
				require.Error(t, err)
				require.Empty(t, resource.Resource)
				require.False(t, resource.Discovered)
			} else {
				require.NoError(t, err)
				require.Equal(t, server.URL+tc.wantPath, resource.Resource)
				require.Equal(t, !tc.missing, resource.Discovered)
			}
			require.Zero(t, transportCalls.Load())
			if tc.pathDoc && !tc.pathOwns && !tc.foreign && !tc.malformed && !tc.noIssuer {
				require.Positive(t, canonicalCalls.Load(), "parent resource must be confirmed at its canonical URL")
			}
		})
	}
}

func TestDiscoverEndpointResourceCancelled(t *testing.T) {
	t.Parallel()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = protectedresource.DiscoverEndpointResource(ctx, policy, "https://127.0.0.1:1/mcp")
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
