package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFinishElsewhereRequested(t *testing.T) {
	t.Parallel()

	ticked := url.Values{finishElsewhereField: {"1"}}
	tests := []struct {
		name        string
		form        url.Values
		redirectURI string
		want        bool
	}{
		{name: "loopback ip", form: ticked, redirectURI: "http://127.0.0.1:51734/callback", want: true},
		{name: "localhost", form: ticked, redirectURI: "http://localhost:3000/callback", want: true},
		{name: "ipv6 loopback", form: ticked, redirectURI: "http://[::1]:8080/oauth/callback", want: true},
		{name: "unticked", form: url.Values{}, redirectURI: "http://127.0.0.1:51734/callback", want: false},
		{name: "other value", form: url.Values{finishElsewhereField: {"on"}}, redirectURI: "http://127.0.0.1:51734/callback", want: false},
		{name: "https redirect", form: ticked, redirectURI: "https://client.example.com/callback", want: false},
		{name: "custom scheme", form: ticked, redirectURI: "com.example.app:/callback", want: false},
		{name: "loopback over https", form: ticked, redirectURI: "https://127.0.0.1:51734/callback", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, finishElsewhereRequested(tt.form, tt.redirectURI))
		})
	}
}

// The command must hand the shell exactly the callback URL, including the `&`
// between parameters and any quote a client put in its own redirect URI.
func TestShellSingleQuoteRoundTrips(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"http://127.0.0.1:51734/callback?code=abc&state=x&iss=https%3A%2F%2Fapp.example.com",
		"http://localhost:3000/it's/callback?state=a'b",
		"http://localhost:3000/callback?state=$(touch%20pwned)`id`",
	} {
		out, err := exec.CommandContext(t.Context(), "sh", "-c", "printf %s "+shellSingleQuote(raw)).Output()
		require.NoError(t, err)
		require.Equal(t, raw, string(out))
	}
}

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
