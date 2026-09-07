package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	authserver "github.com/speakeasy-api/gram/server/gen/http/auth/server"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

func TestSessionResponseEncodersKeepCredentialsOutOfBodies(t *testing.T) {
	t.Parallel()
	const token = "opaque-access-token-must-stay-in-headers"
	tests := []struct {
		name   string
		encode func(context.Context, http.ResponseWriter, any) error
		result any
		status int
	}{
		{name: "callback", encode: authserver.EncodeCallbackResponse(goahttp.ResponseEncoder), result: &gen.CallbackResult{Location: "https://dashboard.example.com", SessionToken: token}, status: http.StatusTemporaryRedirect},
		{name: "scope", encode: authserver.EncodeSwitchScopesResponse(goahttp.ResponseEncoder), result: &gen.SwitchScopesResult{SessionToken: token}, status: http.StatusOK},
		{name: "demo", encode: authserver.EncodeEnterDemoResponse(goahttp.ResponseEncoder), result: &gen.EnterDemoResult{SessionToken: token}, status: http.StatusOK},
		{name: "refresh", encode: authserver.EncodeRefreshResponse(goahttp.ResponseEncoder), result: &gen.RefreshResult{SessionToken: token}, status: http.StatusNoContent},
		{name: "info", encode: authserver.EncodeInfoResponse(goahttp.ResponseEncoder), result: &gen.InfoResult{UserID: "user", UserEmail: "user@example.com", ActiveOrganizationID: "org", SessionToken: token}, status: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			require.NoError(t, tt.encode(t.Context(), recorder, tt.result))
			require.Equal(t, tt.status, recorder.Code)
			require.Equal(t, token, recorder.Header().Get(constants.SessionHeader))
			require.NotContains(t, recorder.Body.String(), token)
			require.NotContains(t, recorder.Body.String(), constants.SessionCookie)
			require.NotContains(t, recorder.Body.String(), refreshCookieName)
			require.NotContains(t, recorder.Body.String(), constants.SessionHeader)
			require.NotContains(t, recorder.Body.String(), "SessionToken")
			if tt.status == http.StatusNoContent {
				require.Empty(t, recorder.Body.String())
			}
			require.Empty(t, recorder.Header().Values("Set-Cookie"), "only explicit browser session issuance writes cookies")
		})
	}
}
