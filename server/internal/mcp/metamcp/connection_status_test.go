package metamcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectionStatusText_HiddenServers(t *testing.T) {
	t.Parallel()

	result := ConnectionStatusResult{
		ConnectURL: "https://example.com/connect",
		Servers: []ConnectionStatusServer{
			{Slug: "docs", Name: "Docs", Status: SignInNotRequired, Detail: ""},
			{Slug: "github", Name: "", Status: SignInHidden, Detail: ""},
			{Slug: "linear", Name: "Linear", Status: SignInHidden, Detail: ""},
		},
	}

	text := result.Text()
	require.Contains(t, text, "1 of 1 server ready.")
	require.Contains(t, text, "- github: hidden by the user\n")
	require.Contains(t, text, "- Linear: hidden by the user\n")
	require.Contains(t, text, "To use a hidden server, show it again at https://example.com/connect.")
	require.NotContains(t, text, "To sign in")

	result.ConnectURL = ""
	text = result.Text()
	require.Contains(t, text, "- Linear: hidden by the user\n")
	require.NotContains(t, text, "show it again")
}

func TestConnectionStatusText_HiddenExcludedFromConsentTotal(t *testing.T) {
	t.Parallel()

	result := ConnectionStatusResult{
		ConnectURL: "",
		Servers: []ConnectionStatusServer{
			{Slug: "docs", Name: "Docs", Status: SignInVerifiedAtConsent, Detail: "3 tools"},
			{Slug: "wiki", Name: "Wiki", Status: SignInVerifiedAtConsent, Detail: "2 tools"},
			{Slug: "github", Name: "GitHub", Status: SignInHidden, Detail: ""},
		},
	}

	require.Contains(t, result.Text(), "2 of 2 servers verified at consent")
}

func TestConnectionStatusText_WithoutHiddenOmitsShowLine(t *testing.T) {
	t.Parallel()

	result := ConnectionStatusResult{
		ConnectURL: "https://example.com/connect",
		Servers:    []ConnectionStatusServer{{Slug: "docs", Name: "Docs", Status: SignInConnected, Detail: ""}},
	}

	text := result.Text()
	require.Contains(t, text, "1 of 1 server ready.")
	require.NotContains(t, text, "hidden")
}

func TestIsConnectionStatusResourceURI(t *testing.T) {
	t.Parallel()
	require.True(t, IsConnectionStatusResourceURI(ConnectionStatusResourceURI))
	require.True(t, IsConnectionStatusResourceURI(ConnectionStatusResourcePrefix+"0123456789ab"))
	for _, uri := range []string{
		ConnectionStatusResourcePrefix,
		ConnectionStatusResourcePrefix + "0123456789a",
		ConnectionStatusResourcePrefix + "0123456789abc",
		ConnectionStatusResourcePrefix + "0123456789AB",
		ConnectionStatusResourcePrefix + "0123456789az",
		ConnectionStatusResourcePrefix + "0123456789ab/x",
		"ui://gateway/other/0123456789ab",
	} {
		require.False(t, IsConnectionStatusResourceURI(uri), uri)
	}
}

func TestConnectionStatusText_NeedsConsentClaimsNoVerification(t *testing.T) {
	t.Parallel()
	text := ConnectionStatusResult{ConnectURL: "https://example.com/connect", Servers: []ConnectionStatusServer{
		{Slug: "source", Name: "Source", Status: SignInNeedsConsent, Detail: "Reconnect this gateway"},
		{Slug: "other", Name: "Other", Status: SignInNotRequired},
	}}.Text()
	require.Contains(t, text, "1 of 2 servers ready.")
	require.NotContains(t, text, "verified at consent")
	require.Contains(t, text, "Start authorization again")
	require.NotContains(t, text, "To sign in")
}

func TestConnectionStatusText_ActionRequiredOffersNoSignIn(t *testing.T) {
	t.Parallel()
	text := ConnectionStatusResult{ConnectURL: "https://example.com/connect", Servers: []ConnectionStatusServer{
		{Slug: "source", Name: "Source", Status: SignInActionRequired, Detail: "Ask your administrator"},
	}}.Text()
	require.Contains(t, text, "- Source: needs attention (Ask your administrator)")
	require.NotContains(t, text, "To sign in")
}
