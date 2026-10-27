package auth_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestCallbackBrowserFailuresRedirectToLogin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ query, reason string }{
		{"error=access_denied&error_description=private-provider-detail", "signin_canceled"},
		{"", "signin_link_invalid"},
		{"code=&state=expired", "signin_link_invalid"},
		{"code=unused&state=invalid", "signin_link_invalid"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()
			_, instance := newTestAuthService(t, defaultMockUserInfo())
			mux := goahttp.NewMuxer()
			auth.Attach(mux, instance.service)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rpc/auth.callback?"+tc.query, nil))
			require.Equal(t, http.StatusTemporaryRedirect, response.Code)
			location, err := url.Parse(response.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, url.Values{"signin_error": {tc.reason}}, location.Query())
			require.NotContains(t, response.Body.String(), "private-provider-detail")
			for _, cookie := range response.Result().Cookies() {
				require.Empty(t, cookie.Value, "failed sign-in must not issue a credential")
			}
		})
	}
}
