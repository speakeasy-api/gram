package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripPrivateIngressHeaders(t *testing.T) {
	t.Parallel()

	var observed http.Header
	handler := StripPrivateIngressHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp/test", nil)
	req.Header.Set("Tailscale-User-Login", "forged@example.com")
	req.Header.Set("tailscale-user-name", "forged")
	req.Header.Set("Tailscale-Capabilities", "forged")
	req.Header.Set(PrivateIngressAttestationHeader, "forged")
	req.Header.Set("Authorization", "Bearer preserved")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, observed.Get("Tailscale-User-Login"))
	require.Empty(t, observed.Get("Tailscale-User-Name"))
	require.Empty(t, observed.Get("Tailscale-Capabilities"))
	require.Empty(t, observed.Get(PrivateIngressAttestationHeader))
	require.Equal(t, "Bearer preserved", observed.Get("Authorization"))
}
