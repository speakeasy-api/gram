package mcp

import (
	"bytes"
	"strings"
	"testing"
	"time"

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
			ClientID:             "client-id",
			IssuerSlug:           "example-issuer",
			Connected:            true,
			Expired:              false,
			CanRefresh:           true,
			AccessExpiresAt:      "2026-08-05T18:00:00Z",
			AccessExpiresIn:      "3 hours",
			RefreshExpiresAt:     "",
			RefreshExpiresIn:     "",
			AutoRefreshSupported: true,
			AutoRefreshChecked:   true,
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
			ClientID:             "client-id",
			IssuerSlug:           "example-issuer",
			Connected:            false,
			Expired:              false,
			CanRefresh:           false,
			AccessExpiresAt:      "",
			AccessExpiresIn:      "",
			RefreshExpiresAt:     "",
			RefreshExpiresIn:     "",
			AutoRefreshSupported: true,
			AutoRefreshChecked:   true,
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

func TestConsentTemplateShowsAutoRefreshAndServiceExpiry(t *testing.T) {
	t.Parallel()

	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:     "Gram",
		MCPSlug:        "example",
		MCPRouteBase:   "mcp",
		State:          "state",
		CSRFToken:      "csrf",
		SubjectDisplay: "user@example.com",
		ScriptURL:      "/mcp/consent-page-test.js",
		RemoteSessionCards: []remoteSessionCard{{
			ClientID:             "client-id",
			IssuerSlug:           "example-issuer",
			Connected:            true,
			Expired:              false,
			CanRefresh:           true,
			AccessExpiresAt:      "2026-08-05T18:00:00Z",
			AccessExpiresIn:      "3 hours 12 minutes",
			RefreshExpiresAt:     "2026-09-05T18:00:00Z",
			RefreshExpiresIn:     "31 days",
			AutoRefreshSupported: true,
			AutoRefreshChecked:   true,
		}},
		ConsentEnabled:         true,
		AutoRefreshSupported:   true,
		AutoRefreshOn:          true,
		AutoRefreshHasSessions: true,
	})
	require.NoError(t, err)

	html := page.String()
	normalizedHTML := strings.Join(strings.Fields(html), " ")
	require.Contains(t, html, "Auto refresh")
	require.NotContains(t, html, "Keep alive")
	require.NotContains(t, html, "About auto refresh")
	require.Contains(t, html, `class="tooltip"`)
	require.Contains(t, html, `tabindex="0"`)
	require.Equal(t, 2, strings.Count(html, `role="tooltip"`))
	require.Contains(t, normalizedHTML, "don't expire from inactivity")
	require.Contains(t, normalizedHTML, "idle connections lapse and need reconnecting")
	require.Contains(t, html, `aria-describedby="refresh-expiry-client-id"`)
	require.Contains(t, html, `id="refresh-expiry-client-id"`)
	require.NotContains(t, html, `datetime="2026-08-05T18:00:00Z"`)
	require.NotContains(t, html, "3 hours 12 minutes")
	require.Contains(t, html, `datetime="2026-09-05T18:00:00Z"`)
	require.Contains(t, html, "31 days")
	require.Contains(t, html, "✓ Auto refresh on")
	require.Contains(t, html, "Refresh now")
	require.Contains(t, html, `data-refresh-link`)

	metaStart := strings.Index(html, `<div class="meta">`)
	actionsStart := strings.Index(html, `<div class="remote-actions">`)
	require.NotEqual(t, -1, metaStart)
	require.Greater(t, actionsStart, metaStart)
	require.NotContains(t, html[metaStart:actionsStart], "expires in")
	require.Contains(t, html[actionsStart:], "Renews on use. If unused, this connection expires in")
	require.NotContains(t, html, "Current access expires in")
}

func TestConsentTemplateHidesAutoRefreshWhenOrganizationFeatureDisabled(t *testing.T) {
	t.Parallel()

	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:     "Gram",
		MCPSlug:        "example",
		MCPRouteBase:   "mcp",
		State:          "state",
		CSRFToken:      "csrf",
		SubjectDisplay: "user@example.com",
		ScriptURL:      "/mcp/consent-page-test.js",
		RemoteSessionCards: []remoteSessionCard{{
			ClientID:             "client-id",
			IssuerSlug:           "example-issuer",
			Connected:            true,
			Expired:              false,
			CanRefresh:           true,
			AccessExpiresAt:      "2026-08-05T18:00:00Z",
			AccessExpiresIn:      "3 hours",
			RefreshExpiresAt:     "",
			RefreshExpiresIn:     "",
			AutoRefreshSupported: false,
			AutoRefreshChecked:   false,
		}},
		ConsentEnabled:       true,
		AutoRefreshSupported: false,
	})
	require.NoError(t, err)

	html := page.String()
	require.NotContains(t, html, `aria-label="Auto refresh"`)
	require.NotContains(t, html, "Auto refresh on")
	require.Contains(t, html, "Refresh now")
	// Refresh lifetime unknown: no expiry tooltip at all.
	require.NotContains(t, html, `role="tooltip"`)
	require.NotContains(t, html, "Renews on use")
	require.NotContains(t, html, "The provider did not report")
}

func TestConsentTemplateOmitsExpiryTooltipWhenNoExpiryReported(t *testing.T) {
	t.Parallel()

	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:     "Gram",
		MCPSlug:        "example",
		MCPRouteBase:   "mcp",
		State:          "state",
		CSRFToken:      "csrf",
		SubjectDisplay: "user@example.com",
		ScriptURL:      "/mcp/consent-page-test.js",
		RemoteSessionCards: []remoteSessionCard{{
			ClientID:             "client-id",
			IssuerSlug:           "example-issuer",
			Connected:            true,
			Expired:              false,
			CanRefresh:           true,
			AccessExpiresAt:      "",
			AccessExpiresIn:      "",
			RefreshExpiresAt:     "",
			RefreshExpiresIn:     "",
			AutoRefreshSupported: false,
			AutoRefreshChecked:   false,
		}},
		ConsentEnabled:       true,
		AutoRefreshSupported: false,
	})
	require.NoError(t, err)

	html := page.String()
	require.Contains(t, html, "Refresh now")
	require.NotContains(t, html, `aria-describedby="refresh-expiry-client-id"`)
	require.NotContains(t, html, `id="refresh-expiry-client-id"`)
	require.NotContains(t, html, "Renews on use")
	require.NotContains(t, html, "The provider did not report")
	require.NotContains(t, html, `role="tooltip"`)
}

func TestFormatTimeRemaining(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	require.Equal(t, "Expired", formatTimeRemaining(now, now))
	require.Equal(t, "1 minute", formatTimeRemaining(now, now.Add(5*time.Second)))
	require.Equal(t, "1 hour 1 minute", formatTimeRemaining(now, now.Add(time.Hour+time.Minute)))
	require.Equal(t, "2 days 3 hours", formatTimeRemaining(now, now.Add(51*time.Hour)))
}

func TestConsentScriptClosesOnlyMarkedPages(t *testing.T) {
	t.Parallel()

	script := string(consentScriptData)
	require.Contains(t, script, `document.body.hasAttribute("data-auto-close")`)
	require.Contains(t, script, "window.close();")
	require.Contains(t, script, "}, 3000);")
	require.Contains(t, script, `guardActionButtons("button[data-refresh-link]", "Refreshing…")`)
}
