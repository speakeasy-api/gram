package remotemcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/stretchr/testify/require"
)

func TestDiscovery_InvalidMetadataPreservesLastGoodAndRecordsError(t *testing.T) {
	t.Parallel()
	for _, fallback := range []bool{false, true} {
		name := "identifier mismatch"
		if fallback {
			name = "fallback provenance mismatch"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestServiceForProbe(t)
			var invalid atomic.Bool
			var resource string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path := wellknown.OAuthProtectedResourcePath + "/mcp"
				if invalid.Load() && fallback {
					path = wellknown.OAuthProtectedResourcePath
				}
				if r.URL.Path != path {
					http.NotFound(w, r)
					return
				}
				identifier, scope := resource, "read"
				if invalid.Load() {
					scope = "untrusted"
					if !fallback {
						identifier += "/other"
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"resource": identifier, "scopes_supported": []string{scope}})
			}))
			t.Cleanup(upstream.Close)
			resource = upstream.URL + "/mcp"
			server := seedRemoteMcpServerWithURL(t, ctx, ti, resource)
			discover := func() {
				out, err := ti.service.DiscoverProtectedResourceMetadata(ctx, &gen.DiscoverProtectedResourceMetadataPayload{RemoteMcpServerID: server.ID.String()})
				require.NoError(t, err)
				require.True(t, out.Available, "diagnostic document remains available")
			}
			discover()
			good := loadProtectedResource(t, ctx, ti, resource)
			require.True(t, good.MetadataFetchedAt.Valid)
			invalid.Store(true)
			discover()
			failed := loadProtectedResource(t, ctx, ti, resource)
			require.True(t, failed.MetadataLastError.Valid)
			require.True(t, failed.MetadataLastErrorAt.Valid)
			require.Equal(t, good.MetadataFetchedAt, failed.MetadataFetchedAt)
			require.Equal(t, good.ScopesSupported, failed.ScopesSupported)
			require.Equal(t, good.Metadata, failed.Metadata)
		})
	}
}
