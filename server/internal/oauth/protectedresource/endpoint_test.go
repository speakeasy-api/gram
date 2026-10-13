package protectedresource_test

import (
	"context"
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
		name      string
		pathDoc   bool
		pathOwns  bool
		query     bool
		foreign   bool
		malformed bool
		missing   bool
		noIssuer  bool
		wantError bool
		wantPath  string
	}{
		{name: "origin metadata owns parent resource"},
		{name: "transport declaration confirmed at canonical location", pathDoc: true},
		{name: "path resource stays exact", pathDoc: true, pathOwns: true, wantPath: "/mcp"},
		{name: "query resource stays exact", pathDoc: true, pathOwns: true, query: true, wantPath: "/mcp?tenant=example"},
		{name: "missing metadata retains explicit endpoint", missing: true, wantPath: "/mcp"},
		{name: "foreign resource refused", foreign: true, wantError: true},
		{name: "malformed metadata refused", malformed: true, wantError: true},
		{name: "unadvertised authorization refused", noIssuer: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var transportCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/mcp" {
					transportCalls.Add(1)
					http.Error(w, "transport must not be probed", http.StatusBadRequest)
					return
				}
				if tc.missing || (!tc.pathDoc && r.URL.Path != "/.well-known/oauth-protected-resource") {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.malformed {
					_, _ = w.Write([]byte("{"))
					return
				}
				resource := "http://" + r.Host
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
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)
			resource, err := protectedresource.DiscoverEndpointResource(t.Context(), policy, server.URL+"/mcp")
			if tc.wantError {
				require.Error(t, err)
				require.Empty(t, resource.Resource)
			} else {
				require.NoError(t, err)
				require.Equal(t, server.URL+tc.wantPath, resource.Resource)
				require.Equal(t, !tc.missing, resource.Discovered)
			}
			require.Zero(t, transportCalls.Load())
		})
	}
}

func TestDiscoverEndpointResourceCancelled(t *testing.T) {
	t.Parallel()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = protectedresource.DiscoverEndpointResource(ctx, policy, "http://127.0.0.1:1/mcp")
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
