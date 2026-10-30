package consentpage_test

import (
	"net/url"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/consentpage"
)

func TestFinishElsewhereRequested(t *testing.T) {
	t.Parallel()

	ticked := url.Values{consentpage.FinishElsewhereField: {"1"}}
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
		{name: "other value", form: url.Values{consentpage.FinishElsewhereField: {"on"}}, redirectURI: "http://127.0.0.1:51734/callback", want: false},
		{name: "https redirect", form: ticked, redirectURI: "https://client.example.com/callback", want: false},
		{name: "custom scheme", form: ticked, redirectURI: "com.example.app:/callback", want: false},
		{name: "loopback over https", form: ticked, redirectURI: "https://127.0.0.1:51734/callback", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, consentpage.FinishElsewhereRequested(tt.form, tt.redirectURI))
		})
	}
}

// The command must hand curl exactly the callback URL, including the `&`
// between parameters and any quote a client put in its own redirect URI.
func TestCallbackCommandQuotesTheURL(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"http://127.0.0.1:51734/callback?code=abc&state=x&iss=https%3A%2F%2Fapp.example.com",
		"http://localhost:3000/it's/callback?state=a'b",
		"http://localhost:3000/callback?state=$(touch%20pwned)`id`",
	} {
		command := consentpage.CallbackCommand(raw)
		quoted, found := strings.CutPrefix(command, "curl -fsS -o /dev/null ")
		require.True(t, found, command)
		out, err := exec.CommandContext(t.Context(), "sh", "-c", "printf %s "+quoted).Output()
		require.NoError(t, err)
		require.Equal(t, raw, string(out))
	}
}
