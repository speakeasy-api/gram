package oauthui_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

func TestAuthorizationErrorPagePreservesCallbackAndEscapesText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ uri, destination string }{
		{"https://client.example:8443/cb", "client.example:8443"},
		{"com.example.app://callback", "com.example.app://callback"},
		{"com.example.app:callback", "com.example.app:"},
	} {
		t.Run(tc.uri, func(t *testing.T) {
			t.Parallel()
			description := `unsupported method "<script>alert(1)</script>"`
			callback := tc.uri + "?" + url.Values{
				oauthwire.ParamError:            {oautherr.CodeInvalidRequest},
				oauthwire.ParamErrorDescription: {description},
				oauthwire.ParamState:            {`state&"<payload>`},
				"iss":                           {"https://issuer.example/mcp/server"},
			}.Encode()
			response := httptest.NewRecorder()
			require.NoError(t, oauthui.WriteAuthorizationError(t.Context(), response, callback, oautherr.CodeInvalidRequest, description))
			parsed := oauthtest.RequireAuthorizationErrorCallback(t, response, http.StatusBadRequest)
			require.Equal(t, callback, parsed.String())
			require.Contains(t, response.Body.String(), "Return to "+tc.destination)
			require.NotContains(t, response.Body.String(), "<script>")
			require.NotContains(t, response.Body.String(), "#ZgotmplZ")
			require.Contains(t, response.Body.String(), "&lt;script&gt;")
			require.Contains(t, response.Header().Get("Content-Security-Policy"), "default-src 'none'")
		})
	}
}

func TestAuthorizationErrorPageRejectsUnsafeSchemes(t *testing.T) {
	t.Parallel()
	for _, scheme := range []string{"javascript", "data", "vbscript", "file", "blob", "view-source"} {
		t.Run(scheme, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			err := oauthui.WriteAuthorizationError(t.Context(), response, strings.ToUpper(scheme)+":payload", oautherr.CodeInvalidRequest, "invalid request")
			require.Error(t, err)
			require.Empty(t, response.Body.String())
			require.Empty(t, response.Header().Get("Location"))
		})
	}
}
