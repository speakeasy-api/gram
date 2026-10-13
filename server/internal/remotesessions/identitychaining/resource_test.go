package identitychaining

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestGovernor_ResourceMetadataRestrictsAuthorizationServer(t *testing.T) {
	t.Parallel()
	for _, allowed := range []bool{true, false} {
		name := "unadvertised issuer"
		if allowed {
			name = "advertised issuer"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newChainStoreFixture(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/.well-known/oauth-protected-resource" {
					http.NotFound(w, r)
					return
				}
				issuer := "https://another-issuer.example.test"
				if allowed {
					issuer = f.sel.issuer
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"resource": "http://" + r.Host, "authorization_servers": []string{issuer}})
			}))
			t.Cleanup(server.Close)
			bindResource(t, f.db, f.req, f.sel.remoteIssuerID, f.sel.clientID, server.URL, remotesessions.PreparationStateReady)
			f.setBinding(t, remotesessions.PreparationStateUnlinked)
			f.req.UpstreamResource = server.URL + "/mcp"
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)
			manager := remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), f.db, f.chainer.enc, policy, nil, nil, nil)
			f.chainer.SetResourceResolver(manager.ResolveEMAResource)
			selected, outcome := f.chainer.selectBinding(t.Context(), f.chainer.logger, f.req)
			if allowed {
				require.True(t, outcome.Succeeded(), "%+v", outcome)
				require.Equal(t, server.URL, selected.resource)
			} else {
				require.Equal(t, ReasonConfigurationRequired, outcome.Reason)
				require.Empty(t, selected.resource)
			}
			require.Equal(t, server.URL+"/mcp", f.req.UpstreamResource, "transport identity is preserved")
		})
	}
}
