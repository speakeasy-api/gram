package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderConsentFinishElsewhere(t *testing.T) {
	t.Parallel()

	callback, err := buildClientRedirect(clientRedirectParams{
		RedirectURI:      "http://127.0.0.1:51734/callback",
		Issuer:           testRedirectIssuer,
		Code:             "auth-code",
		State:            "client-state",
		ErrorCode:        "",
		ErrorDescription: "",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	require.NoError(t, renderConsentFinishElsewhere(w, "Codex", callback))

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
	body := w.Body.String()
	require.Contains(t, body, "Finish on the other machine")
	require.Contains(t, body, "127.0.0.1:51734")
	require.Contains(t, body, "expires in 10 minutes")
	// html/template escapes the `&` and quotes in the markup; the browser
	// shows, and the copy button copies, the unescaped command.
	require.Contains(t, body, "curl -fsS -o /dev/null &#39;"+strings.ReplaceAll(callback, "&", "&amp;")+"&#39;")
}
