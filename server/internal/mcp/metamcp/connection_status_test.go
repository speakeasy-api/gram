package metamcp

import (
	"encoding/json"
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
	// An agent cannot sign in itself, so with no row to reconnect it is not
	// told to sign in, and a server not signed in shows who fixes it.
	agentHint := strings.Index(ConnectionStatusHTML, `: needs > 0 && agent ? "This agent cannot fix these servers itself.`)
	require.Positive(t, agentHint)
	require.Less(t, agentHint, strings.Index(ConnectionStatusHTML, `: needs > 0 ? "After signing in`), "the agent branch comes before the caller's")
	require.Contains(t, ConnectionStatusHTML, `(s.detail ? "Not signed in · " + s.detail : "Not signed in")`)
}

// An agent cannot sign in itself: its gateway sends it to someone who can
// edit it, with the page where they reconnect the server, never to a gateway
// administrator. It names no one, since the owner is not the only editor.
func TestConnectionStatusText_AgentEditorReconnects(t *testing.T) {
	t.Parallel()

	link := "https://app.example.com/acme/projects/default/identities/agent:agent-1/mcp-sign-in"
	result := ConnectionStatusResult{
		ConnectURL: link,
		Audience:   ConnectionStatusAudienceAgentEditors,
		Servers: []ConnectionStatusServer{
			{Slug: "linear", Name: "Linear", Status: SignInError, Detail: "needs re-authentication", Reconnect: true},
		},
	}
	text := result.Text()
	require.Contains(t, text, "- Linear: needs attention (needs re-authentication)")
	require.Contains(t, text, "Ask someone who can edit this agent to open "+link+" and connect or reconnect them, then call connection_status again to refresh.")
	require.NotContains(t, text, "owner")
	require.NotContains(t, text, "These servers need re-authentication", "a server's own line says what is wrong")
	require.NotContains(t, text, "administrator")
	require.NotContains(t, text, "sign-in page")
	require.NotContains(t, text, "To sign in, open", "the agent does not open its editors' page")

	result.ConnectURL = ""
	text = result.Text()
	require.Contains(t, text, "Ask someone who can edit this agent to check them in Speakeasy")
	require.NotContains(t, text, "administrator")

	// An agent with nothing a reconnect fixes reads each server's own detail,
	// and a server that names no one goes to an admin of its project.
	result.ConnectURL = link
	result.Servers[0].Reconnect = false
	result.Servers = append(result.Servers,
		ConnectionStatusServer{Slug: "weather", Name: "Weather", Status: SignInNotConnected, Detail: "needs a credential configured for it in Speakeasy; ask an admin of the server's project"},
		ConnectionStatusServer{Slug: "docs", Name: "Docs", Status: SignInError, Detail: "could not check this server"},
	)
	text = result.Text()
	require.Contains(t, text, "This agent cannot fix these servers itself. Ask whoever each server above names, or an admin of the server's project where it names no one, then call connection_status again to refresh.")
	require.Contains(t, text, "- Weather: not signed in (needs a credential configured for it in Speakeasy; ask an admin of the server's project)")
	require.NotContains(t, text, "Each server above says who can")
	require.NotContains(t, text, link)
	require.NotContains(t, text, "administrator")
	result.Servers = result.Servers[:1]

	result.Audience = ConnectionStatusAudienceCaller
	result.ConnectURL = ""
	require.Contains(t, result.Text(), "ask the gateway's administrator", "a gateway with no agent keeps its wording")
}

// The agent's sign-in step does not show a hidden server again, so only the
// caller's own connect page is offered for that.
func TestConnectionStatusText_AgentHiddenServers(t *testing.T) {
	t.Parallel()

	link := "https://app.example.com/acme/projects/default/identities/agent:agent-1/mcp-sign-in"
	servers := []ConnectionStatusServer{
		{Slug: "linear", Name: "Linear", Status: SignInNotConnected, Reconnect: true},
		{Slug: "github", Name: "GitHub", Status: SignInHidden},
	}
	text := ConnectionStatusResult{ConnectURL: link, Audience: ConnectionStatusAudienceAgentEditors, Servers: servers}.Text()
	require.Contains(t, text, "- GitHub: hidden by the user")
	require.NotContains(t, text, "show it again")
	require.Contains(t, text, "Ask someone who can edit this agent to open "+link)

	text = ConnectionStatusResult{ConnectURL: "https://example.com/connect", Audience: ConnectionStatusAudienceCaller, Servers: servers}.Text()
	require.Contains(t, text, "To use a hidden server, show it again at https://example.com/connect.")

	require.Contains(t, ConnectionStatusHTML, `if (data.connectUrl && !agent) {`, "the view offers Show only on the caller's page")
}

// An agent's result names its editors as the audience of the one link and
// marks the rows that link fixes. A person's result has neither, so its shape
// is what it was.
func TestConnectionStatusResult_JSONShape(t *testing.T) {
	t.Parallel()

	person, err := json.Marshal(ConnectionStatusResult{
		ConnectURL: "https://example.com/connect",
		Audience:   ConnectionStatusAudienceCaller,
		Servers:    []ConnectionStatusServer{{Slug: "linear", Status: SignInNotConnected}},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"connectUrl":"https://example.com/connect","servers":[{"slug":"linear","status":"not_connected"}]}`, string(person))

	link := "https://app.example.com/acme/projects/default/identities/agent:agent-1/mcp-sign-in"
	agent, err := json.Marshal(ConnectionStatusResult{
		ConnectURL: link,
		Audience:   ConnectionStatusAudienceAgentEditors,
		Servers: []ConnectionStatusServer{
			{Slug: "linear", Status: SignInError, Detail: "needs re-authentication", Reconnect: true},
			{Slug: "weather", Status: SignInNotConnected},
		},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"connectUrl":"`+link+`","audience":"agent_editors","servers":[{"slug":"linear","status":"error","detail":"needs re-authentication","reconnect":true},{"slug":"weather","status":"not_connected"}]}`, string(agent))
}
