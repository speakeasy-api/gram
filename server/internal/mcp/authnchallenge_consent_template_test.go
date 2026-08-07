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

func TestConsentTemplateToolAccessIsland(t *testing.T) {
	t.Parallel()

	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:            "Demo",
		MCPSlug:               "example",
		MCPRouteBase:          "mcp",
		State:                 "state",
		CSRFToken:             "csrf",
		SubjectDisplay:        "user@example.com",
		RedirectURI:           "http://127.0.0.1/cb",
		ScriptURL:             "/mcp/consent-page-test.js",
		ConsentEnabled:        true,
		FirstParty:            false,
		ShowToolsIsland:       true,
		ConsentToolsURL:       "/mcp/example/connect/tools",
		ConsentToolsScriptURL: "/mcp/consent-tools-test.js",
		ConsentToolsPrefill:   `{"tools":["reader"]}`,
	})
	require.NoError(t, err)

	html := page.String()
	require.Contains(t, html, "Tool access")
	require.Contains(t, html, `id="consent-tools-root"`)
	require.Contains(t, html, `data-tools-url="/mcp/example/connect/tools"`)
	require.Contains(t, html, `data-state="state"`)
	require.Contains(t, html, `data-csrf-token="csrf"`)
	require.Contains(t, html, `data-form-id="consent-approve-form"`)
	require.Contains(t, html, `data-approve-button-id="consent-approve-button"`)
	require.Contains(t, html, `data-consent-enabled="true"`)
	require.Contains(t, html, `data-prefill=`)
	require.Contains(t, html, `src="/mcp/consent-tools-test.js"`)
	// The approve button always renders disabled; only the island enables
	// it, so a missing or failed bundle fails closed. Anchor on the submit
	// value to skip the mount's data-approve-button-id attribute.
	buttonStart := strings.Index(html, `value="approve"`)
	require.NotEqual(t, -1, buttonStart)
	buttonRegion := html[buttonStart:]
	buttonEnd := strings.Index(buttonRegion, ">")
	require.NotEqual(t, -1, buttonEnd)
	require.Contains(t, buttonRegion[:buttonEnd], `id="consent-approve-button"`)
	require.Contains(t, buttonRegion[:buttonEnd], "disabled")
	// The server-rendered picker markup is gone; the island owns the form
	// fields.
	require.NotContains(t, html, `name="tool_filtering"`)
	require.NotContains(t, html, `data-tools-panel`)
	require.NotContains(t, html, `data-scope-tools`)
	// No inline script or JSON bootstrap beyond escaped data attributes.
	require.NotContains(t, html, "<script>")
}

func TestConsentTemplateToolAccessOmittedOnFirstParty(t *testing.T) {
	t.Parallel()

	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:      "Demo",
		MCPSlug:         "example",
		MCPRouteBase:    "mcp",
		State:           "state",
		CSRFToken:       "csrf",
		SubjectDisplay:  "user@example.com",
		ScriptURL:       "/mcp/consent-page-test.js",
		ConsentEnabled:  true,
		FirstParty:      true,
		ShowToolsIsland: false,
	})
	require.NoError(t, err)
	require.NotContains(t, page.String(), "Tool access")
	require.NotContains(t, page.String(), "consent-tools-root")
	require.NotContains(t, page.String(), "consent-tools-")
}
