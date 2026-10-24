package platformmcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/stretchr/testify/require"
)

func TestAHPOAuthTrustedOriginMetadata(t *testing.T) {
	t.Parallel()
	s, _ := newAHPProtocolHTTP(t, nil)
	for _, tc := range []struct{ base, resource, metadata string }{
		{"https://api.example.test", "https://api.example.test/ahp", "https://api.example.test/.well-known/oauth-protected-resource/ahp"},
		{"https://api.example.test/prefix/", "https://api.example.test/prefix/ahp", "https://api.example.test/prefix/.well-known/oauth-protected-resource/ahp"},
		{"https://api.example.test/api%2Fv1?ignored=1#ignored", "https://api.example.test/api%2Fv1/ahp", "https://api.example.test/api%2Fv1/.well-known/oauth-protected-resource/ahp"},
	} {
		t.Run(tc.base, func(t *testing.T) {
			t.Parallel()
			base, err := url.Parse(tc.base)
			require.NoError(t, err)
			require.Equal(t, tc.resource, s.resourceAt(base))
			require.Equal(t, tc.metadata, s.protectedResourceURL(base))
			req := httptest.NewRequest(http.MethodGet, "https://forged.invalid/.well-known/oauth-protected-resource/ahp", nil)
			req = req.WithContext(requestorigin.WithContext(req.Context(), requestorigin.Origin{BaseURL: tc.base, Surface: requestorigin.SurfacePlatform}))
			w := httptest.NewRecorder()
			s.ProtectedResourceHandler().ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
			require.Contains(t, w.Body.String(), tc.resource)
			require.NotContains(t, w.Body.String(), "forged.invalid")
		})
	}
}
