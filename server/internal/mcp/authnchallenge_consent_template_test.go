package mcp

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConsentTemplateCompletedFirstPartyConnectionAutoCloses(t *testing.T) {
	t.Parallel()

	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:     "Gram",
		MCPSlug:        "example",
		MCPRouteBase:   "x/mcp",
		State:          "state",
		CSRFToken:      "csrf",
		SubjectDisplay: "user@example.com",
		RedirectURI:    "",
		ScriptURL:      "/mcp/consent-page-test.js",
		RemoteSessionCards: []remoteSessionCard{{
			ClientID:     "client-id",
			IssuerSlug:   "example-issuer",
			Connected:    true,
			Expired:      false,
			ChallengeURL: "https://issuer.example.com/authorize",
		}},
		ConsentEnabled: true,
		FirstParty:     true,
		AutoClose:      true,
	})
	require.NoError(t, err)

	require.Contains(t, page.String(), "<body data-auto-close>")
	require.Contains(t, page.String(), "Connection complete. This tab will close automatically.")
	require.NotContains(t, page.String(), "When you've connected the services above")
}

func TestConsentTemplateIncompleteFirstPartyConnectionStaysOpen(t *testing.T) {
	t.Parallel()

	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:     "Gram",
		MCPSlug:        "example",
		MCPRouteBase:   "x/mcp",
		State:          "state",
		CSRFToken:      "csrf",
		SubjectDisplay: "user@example.com",
		RedirectURI:    "",
		ScriptURL:      "/mcp/consent-page-test.js",
		RemoteSessionCards: []remoteSessionCard{{
			ClientID:     "client-id",
			IssuerSlug:   "example-issuer",
			Connected:    false,
			Expired:      false,
			ChallengeURL: "https://issuer.example.com/authorize",
		}},
		ConsentEnabled: false,
		FirstParty:     true,
		AutoClose:      false,
	})
	require.NoError(t, err)

	require.NotContains(t, page.String(), "data-auto-close")
	require.Contains(t, page.String(), "When you've connected the services above, you can close this tab.")
	require.NotContains(t, page.String(), "Connection complete")
}

func TestShouldAutoCloseFirstParty(t *testing.T) {
	t.Parallel()

	connected := remoteSessionCard{Connected: true}
	disconnected := remoteSessionCard{Connected: false}
	expired := remoteSessionCard{Connected: false, Expired: true}

	require.False(t, shouldAutoCloseFirstParty(false, []remoteSessionCard{connected}), "client consent must stay open")
	require.False(t, shouldAutoCloseFirstParty(true, nil), "a connection with no cards must stay open")
	require.True(t, shouldAutoCloseFirstParty(true, []remoteSessionCard{connected}))
	require.False(t, shouldAutoCloseFirstParty(true, []remoteSessionCard{disconnected}))
	require.False(t, shouldAutoCloseFirstParty(true, []remoteSessionCard{expired}))
	require.True(t, shouldAutoCloseFirstParty(true, []remoteSessionCard{connected, connected}))
	require.False(t, shouldAutoCloseFirstParty(true, []remoteSessionCard{connected, disconnected}), "partially connected flows must stay open")
	require.False(t, shouldAutoCloseFirstParty(true, []remoteSessionCard{connected, expired}), "flows with expired sessions must stay open")
}

func TestConsentScriptClosesOnlyMarkedPages(t *testing.T) {
	t.Parallel()

	script := string(consentScriptData)
	require.Contains(t, script, `document.body.hasAttribute("data-auto-close")`)
	require.Contains(t, script, "window.close();")
	require.Contains(t, script, "}, 3000);")
}
