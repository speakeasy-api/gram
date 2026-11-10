package wellknown

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryDiagnosticsPreserveProtocolQuery(t *testing.T) {
	t.Parallel()
	const query = "arbitrary=private%2Fvalue&mode=a&mode=b"
	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			requests := make(chan string, 4)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.URL.RawQuery
				w.WriteHeader(status)
				if status == http.StatusOK {
					_ = json.NewEncoder(w).Encode(map[string]string{"resource": "http://" + r.Host + "?" + query})
				}
			}))
			defer upstream.Close()
			resource := upstream.URL + "?" + query
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)
			doc, _, err := DiscoverProtectedResourceMetadata(t.Context(), policy, resource)
			if status != http.StatusOK {
				_, probeErr := attemptProtectedResourceProbe(t.Context(), policy.Client(), upstream.URL+OAuthProtectedResourcePath+"?"+query)
				err = probeErr
			}
			require.Equal(t, query, <-requests)
			if status == http.StatusOK {
				require.NoError(t, err)
				require.Equal(t, upstream.URL+OAuthProtectedResourcePath+"?"+query, doc.MetadataURL)
				require.True(t, doc.ValidForResource(resource))
				doc.MetadataURL = upstream.URL + OAuthProtectedResourcePath
				require.False(t, doc.ValidForResource(resource))
			} else {
				var discoveryErr *ProtectedResourceDiscoveryError
				require.ErrorAs(t, err, &discoveryErr)
				require.Contains(t, discoveryErr.ProbeURL, query)
				require.NotContains(t, discoveryErr.UserMessage(), query)
				require.NotContains(t, discoveryErr.Error(), "private")
				require.Contains(t, discoveryErr.UserMessage(), upstream.URL+OAuthProtectedResourcePath)
			}
		})
	}
}

func TestDiscoveryDiagnosticsHideWrappedCause(t *testing.T) {
	t.Parallel()
	raw := "https://user:password@example.test/mcp%2Froute?arbitrary=secret#fragment"
	cause := &url.Error{Op: "Get", URL: raw, Err: errors.New("transport secret")}
	for _, status := range []int{0, 200, 404, 500} {
		e := &ProtectedResourceDiscoveryError{ProbeURL: raw, Status: status, cause: fmt.Errorf("fetch: %w", cause)}
		require.ErrorIs(t, e, cause.Err)
		var wrapped *url.Error
		require.ErrorAs(t, e, &wrapped)
		require.Same(t, cause, wrapped)
		require.Equal(t, raw, e.ProbeURL)
		require.Contains(t, e.UserMessage(), "https://example.test/mcp%2Froute")
		for _, secret := range []string{"user", "password", "arbitrary", "secret", "fragment"} {
			require.NotContains(t, e.Error(), secret)
			require.NotContains(t, e.UserMessage(), secret)
		}
	}
	invalid := &ProtectedResourceDiscoveryError{cause: errors.New("invalid secret URL")}
	require.NotContains(t, invalid.Error(), "secret")
}
