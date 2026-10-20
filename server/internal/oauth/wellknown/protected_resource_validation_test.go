package wellknown_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/stretchr/testify/require"
)

func TestDiscoveredMetadataValidForResource(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path, metadataPath string
		valid                    bool
	}{
		{"root", "", "", true},
		{"root slash", "/", "", true},
		{"path", "/mcp", "/mcp", true},
		{"path slash", "/mcp/", "/mcp/", true},
		{"escaped path", "/mcp%2Ftenant", "/mcp%2Ftenant", true},
		{"query", "/mcp?tenant=a", "/mcp?tenant=a", true},
		{"invalid fallback", "/mcp", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RequestURI() != wellknown.OAuthProtectedResourcePath+tc.metadataPath {
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"resource": "http://" + r.Host + tc.path})
			}))
			t.Cleanup(server.Close)
			resource := server.URL + tc.path
			doc, _, err := wellknown.DiscoverProtectedResourceMetadata(context.Background(), newProbeTestPolicy(t), resource)
			require.NoError(t, err, "invalid fallback remains available for diagnostics")
			require.Equal(t, resource, doc.Resource)
			require.Equal(t, tc.valid, doc.ValidForResource(resource))
		})
	}
}
