package metamcp

import (
	"strings"
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

func TestConnectionStatusText_UnavailableDoesNotAskForSignIn(t *testing.T) {
	t.Parallel()

	text := ConnectionStatusResult{
		ConnectURL: "https://example.test/connect",
		Servers:    []ConnectionStatusServer{{Slug: "a", Status: SignInUnavailable, Detail: "retry shortly"}},
	}.Text()
	require.Contains(t, text, "temporarily unavailable (retry shortly)")
	require.NotContains(t, text, "To sign in")
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

// The outage words are for the view, so the text for the AI app keeps Detail.
func TestConnectionStatusText_OutageWordsStayOutOfTheAppMessage(t *testing.T) {
	t.Parallel()

	text := ConnectionStatusResult{Servers: []ConnectionStatusServer{{
		Slug: "a", Status: SignInUnavailable, Detail: "retry shortly",
		Outage: &ConnectionStatusOutage{Message: "A's provider isn't responding.", ReconnectURL: "https://example.test/connect"},
	}}}.Text()
	require.Contains(t, text, "temporarily unavailable (retry shortly)")
	require.NotContains(t, text, "A's provider")
}

func TestConnectionStatusViewUsesOneReconnectWording(t *testing.T) {
	t.Parallel()
	require.Equal(t, 2, strings.Count(ConnectionStatusHTML, "Reconnect this gateway in your AI app to review access"))
	require.NotContains(t, ConnectionStatusHTML, "Start authorization again")
	require.NotContains(t, ConnectionStatusHTML, "Give consent again")
}

func TestConnectionStatusText_NeedsConsentClaimsNoVerification(t *testing.T) {
	t.Parallel()
	text := ConnectionStatusResult{ConnectURL: "https://example.com/connect", Servers: []ConnectionStatusServer{
		{Slug: "source", Name: "Source", Status: SignInNeedsConsent, Detail: "Reconnect this gateway"},
		{Slug: "other", Name: "Other", Status: SignInNotRequired},
	}}.Text()
	require.Contains(t, text, "1 of 2 servers ready.")
	require.NotContains(t, text, "verified at consent")
	require.Contains(t, text, "Reconnect this gateway in your AI app")
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

func TestConnectionStatusText_MixedOutageAndCredentialActions(t *testing.T) {
	t.Parallel()

	result := ConnectionStatusResult{
		ConnectURL: "https://example.com/connect",
		Servers: []ConnectionStatusServer{
			{Slug: "public", Status: SignInNotRequired},
			{Slug: "offline", Status: SignInUnavailable, Detail: "retry shortly", Outage: &ConnectionStatusOutage{Message: "Provider unavailable", ReconnectURL: "https://example.com/reconnect"}},
			{Slug: "admin", Status: SignInActionRequired, Detail: "Ask your administrator"},
			{Slug: "consent", Status: SignInNeedsConsent, Detail: "Review access"},
		},
	}
	text := result.Text()
	require.Contains(t, text, "1 of 4 servers ready.")
	require.Contains(t, text, "- offline: temporarily unavailable (retry shortly)")
	require.Contains(t, text, "- admin: needs attention (Ask your administrator)")
	require.Contains(t, text, "Reconnect this gateway in your AI app to review access.")
	require.NotContains(t, text, "verified at consent")
	require.NotContains(t, text, "To sign in")
	require.NotContains(t, text, "Provider unavailable")
	require.NotContains(t, text, "https://example.com/reconnect")

	// An unrelated member that can sign in must still receive its sign-in link.
	result.Servers = append(result.Servers, ConnectionStatusServer{Slug: "signin", Status: SignInNotConnected})
	require.Contains(t, result.Text(), "To sign in, open https://example.com/connect")
}

func TestConnectionStatusView_PreservesOutageAndConsentHints(t *testing.T) {
	t.Parallel()

	// Neither an outage nor a new-consent request should hide the refresh hint.
	require.Contains(t, ConnectionStatusHTML, "hint.hidden = !consented && !needsConsent && needs === 0 && unavailable === 0;")
	require.Contains(t, ConnectionStatusHTML, `: needs > 0 ? "After signing in, ask again to refresh this list."`)
	require.Contains(t, ConnectionStatusHTML, `: "Ask again shortly to refresh this list."`)
}

// An agent cannot sign in itself: its gateway names the owner who reconnects
// the server and the page they do it on, never a gateway administrator.
func TestConnectionStatusText_AgentOwnerReconnects(t *testing.T) {
	t.Parallel()

	link := "https://app.example.com/acme/projects/default/identities/agents/agent-1/edit?step=2"
	result := ConnectionStatusResult{
		ConnectURL: "",
		Servers: []ConnectionStatusServer{
			{Slug: "linear", Name: "Linear", Status: SignInError, Detail: "needs re-authentication"},
		},
		Owner: &ConnectionStatusOwner{Name: "Maya Chen", ReconnectURL: link},
	}
	text := result.Text()
	require.Contains(t, text, "- Linear: needs attention (needs re-authentication)")
	require.Contains(t, text, "Ask its owner, Maya Chen, to open "+link+" and connect or reconnect them, then call connection_status again to refresh.")
	require.NotContains(t, text, "These servers need re-authentication", "a server's own line says what is wrong")
	require.NotContains(t, text, "administrator")
	require.NotContains(t, text, "sign-in page")

	result.Owner = &ConnectionStatusOwner{Name: "", ReconnectURL: ""}
	text = result.Text()
	require.Contains(t, text, "Ask its owner to check them in Speakeasy")
	require.NotContains(t, text, "administrator")

	result.Owner = nil
	require.Contains(t, result.Text(), "ask the gateway's administrator", "a gateway with no agent keeps its wording")
}
