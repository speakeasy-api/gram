package authn

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBrowserPolicyPreservesConsentOrigin(t *testing.T) {
	t.Parallel()

	service := &Service{}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			service.wrap(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})(response, httptest.NewRequest(method, "https://channels.example.com/channels/oauth/authorize", nil))

			require.Equal(t, http.StatusNoContent, response.Code)
			// Chrome sends Origin: null on form POSTs under no-referrer, even
			// for same-origin forms. Keep the origin without leaking cross-origin referrers.
			require.Equal(t, "same-origin", response.Header().Get("Referrer-Policy"))
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.Equal(t, "no-cache", response.Header().Get("Pragma"))
			require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
		})
	}
}

func TestConsentRejectsOpaqueAndUntrustedOrigins(t *testing.T) {
	t.Parallel()

	service := &Service{origin: "https://channels.example.com", dashboardOrigin: "https://dashboard.example.com"}
	for _, origin := range []string{"", "null", "https://attacker.example.com"} {
		t.Run(origin, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, service.origin+"/channels/oauth/authorize", strings.NewReader("decision=allow"))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Origin", origin)
			response := httptest.NewRecorder()
			service.wrap(service.consent)(response, request)

			require.Equal(t, http.StatusForbidden, response.Code)
			require.JSONEq(t, `{"error":"invalid_request"}`, response.Body.String())
			require.Empty(t, response.Header().Get("Location"))
		})
	}
}
