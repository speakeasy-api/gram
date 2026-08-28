package mcp

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// normalizeWhitespace collapses the template's formatting-driven line breaks so
// assertions can target rendered copy rather than indentation.
func normalizeWhitespace(html string) string {
	return strings.Join(strings.Fields(html), " ")
}

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
			ClientID:           "client-id",
			IssuerSlug:         "example-issuer",
			Connected:          true,
			Expired:            false,
			CanRefresh:         true,
			AccessExpiresAt:    "2026-08-05T18:00:00Z",
			AccessExpiresIn:    "3 hours",
			RefreshExpiresAt:   "",
			RefreshExpiresIn:   "",
			AutoRefreshChecked: true,
		}},
		ConsentEnabled: true,
		FirstParty:     true,
		AutoClose:      true,
	})
	require.NoError(t, err)

	require.Contains(t, normalizeWhitespace(page.String()), "<body class=\"bg-background text-foreground min-h-screen\" data-auto-close >")
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
			ClientID:           "client-id",
			IssuerSlug:         "example-issuer",
			Connected:          false,
			Expired:            false,
			CanRefresh:         false,
			AccessExpiresAt:    "",
			AccessExpiresIn:    "",
			RefreshExpiresAt:   "",
			RefreshExpiresIn:   "",
			AutoRefreshChecked: true,
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
			ClientID:           "client-id",
			IssuerSlug:         "example-issuer",
			Connected:          true,
			Expired:            false,
			CanRefresh:         true,
			AccessExpiresAt:    "2026-08-05T18:00:00Z",
			AccessExpiresIn:    "3 hours 12 minutes",
			RefreshExpiresAt:   "2026-09-05T18:00:00Z",
			RefreshExpiresIn:   "31 days",
			AutoRefreshChecked: true,
		}},
		ConsentEnabled:         true,
		AutoRefreshPolicy:      autoRefreshUserControlled,
		AutoRefreshOn:          true,
		AutoRefreshHasSessions: true,
	})
	require.NoError(t, err)

	html := page.String()
	normalizedHTML := strings.Join(strings.Fields(html), " ")
	require.Contains(t, html, "Auto refresh")
	require.NotContains(t, html, "Keep alive")
	require.NotContains(t, html, "About auto refresh")
	require.Contains(t, normalizedHTML, "don't expire from inactivity")
	require.Contains(t, normalizedHTML, "idle connections lapse and need reconnecting")
	// The refresh lifetime is stated inline on the card rather than hidden
	// behind a hover tooltip, which is unreachable on touch.
	require.Contains(t, normalizedHTML, "Renews on use. If unused, expires in")
	require.Contains(t, html, `datetime="2026-09-05T18:00:00Z"`)
	require.Contains(t, html, "31 days")
	// The access lifetime is not a connection lifetime, so it stays off the page.
	require.NotContains(t, html, `datetime="2026-08-05T18:00:00Z"`)
	require.NotContains(t, html, "3 hours 12 minutes")
	require.NotContains(t, html, "Current access expires in")
	require.Contains(t, normalizedHTML, "Connected · auto refresh on")
	require.Contains(t, html, `data-refresh-link`)
}

func TestConsentTemplateLocksAutoRefreshWhenOrganizationRequires(t *testing.T) {
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
			ClientID:           "client-id",
			IssuerSlug:         "example-issuer",
			Connected:          true,
			Expired:            false,
			CanRefresh:         true,
			AccessExpiresAt:    "2026-08-05T18:00:00Z",
			AccessExpiresIn:    "3 hours",
			AutoRefreshChecked: true,
		}},
		ConsentEnabled:         true,
		AutoRefreshPolicy:      autoRefreshEnforced,
		AutoRefreshOn:          true,
		AutoRefreshHasSessions: true,
	})
	require.NoError(t, err)

	html := page.String()
	// The value is shown read-only, managed by the org — no editable control
	// and no user-driven persistence form.
	require.Contains(t, normalizeWhitespace(html), "On · managed by your organization")
	require.Contains(t, html, `data-auto-refresh-managed`)
	require.NotContains(t, html, `data-auto-refresh-select`)
	require.NotContains(t, html, `id="auto-refresh-form"`)
	// The per-card hidden input still carries the required "on" value so the
	// connect action persists auto_refresh=on.
	require.Contains(t, html, `value="on"`)
	require.Contains(t, normalizeWhitespace(html), "Connected · auto refresh on")
}

func TestConsentTemplateShowsAutoRefreshOffWhenOrganizationDisables(t *testing.T) {
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
			ClientID:           "client-id",
			IssuerSlug:         "example-issuer",
			Connected:          true,
			Expired:            false,
			CanRefresh:         true,
			AccessExpiresAt:    "2026-08-05T18:00:00Z",
			AccessExpiresIn:    "3 hours",
			RefreshExpiresAt:   "",
			RefreshExpiresIn:   "",
			AutoRefreshChecked: false,
		}},
		ConsentEnabled:    true,
		AutoRefreshPolicy: autoRefreshDisabled,
		AutoRefreshOn:     false,
	})
	require.NoError(t, err)

	html := page.String()
	// A disabled organization policy is stated rather than hidden, so the
	// subject knows idle connections will lapse.
	require.Contains(t, normalizeWhitespace(html), "Off · managed by your organization")
	require.Contains(t, html, `data-auto-refresh-managed`)
	require.NotContains(t, normalizeWhitespace(html), "auto refresh on")
	// Nothing to change and nothing to persist.
	require.NotContains(t, html, `data-auto-refresh-select`)
	require.NotContains(t, html, `id="auto-refresh-form"`)
	// The connect action carries the organization's "off" value explicitly.
	require.Contains(t, html, `value="off"`)
}

func TestConsentTemplateOmitsAutoRefreshRowWithoutRemoteSessions(t *testing.T) {
	t.Parallel()

	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:         "Gram",
		MCPSlug:            "example",
		MCPRouteBase:       "mcp",
		State:              "state",
		CSRFToken:          "csrf",
		SubjectDisplay:     "user@example.com",
		ScriptURL:          "/mcp/consent-page-test.js",
		RemoteSessionCards: nil,
		ConsentEnabled:     true,
	})
	require.NoError(t, err)

	html := page.String()
	// With no services to connect there is no refresh behavior to describe.
	require.NotContains(t, html, "Auto refresh")
	require.NotContains(t, html, "managed by your organization")
	require.NotContains(t, html, `data-auto-refresh-select`)
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
			ClientID:           "client-id",
			IssuerSlug:         "example-issuer",
			Connected:          true,
			Expired:            false,
			CanRefresh:         true,
			AccessExpiresAt:    "",
			AccessExpiresIn:    "",
			RefreshExpiresAt:   "",
			RefreshExpiresIn:   "",
			AutoRefreshChecked: true,
		}},
		ConsentEnabled:    true,
		AutoRefreshPolicy: autoRefreshUserControlled,
		AutoRefreshOn:     true,
	})
	require.NoError(t, err)

	html := page.String()
	require.Contains(t, html, `data-refresh-link`)
	require.NotContains(t, html, "Renews on use")
	require.NotContains(t, html, "The provider did not report")
	require.NotContains(t, html, "<time")
}

// A branded issuer renders its display name and logo; the disconnect
// control is labeled with the same display name.
func TestConsentTemplateRendersIssuerBranding(t *testing.T) {
	t.Parallel()

	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:     "Example Client",
		MCPSlug:        "example",
		MCPRouteBase:   "mcp",
		State:          "state",
		CSRFToken:      "csrf",
		SubjectDisplay: "user@example.com",
		ScriptURL:      "/mcp/consent-page-test.js",
		RemoteSessionCards: []remoteSessionCard{{
			ClientID:      "client-id",
			IssuerSlug:    "corp-okta",
			IssuerDisplay: "Corporate Okta",
			IssuerLogoURL: "https://app.getgram.ai/rpc/assets.serveImage?id=00000000-0000-0000-0000-000000000001",
			Connected:     true,
		}},
		ConsentEnabled: true,
	})
	require.NoError(t, err)

	html := page.String()
	require.Contains(t, html, "Corporate Okta")
	require.Contains(t, html, `data-issuer-logo`)
	require.Contains(t, html, `src="https://app.getgram.ai/rpc/assets.serveImage?id=00000000-0000-0000-0000-000000000001"`)
	// The logo is decorative next to the visible display name, so it must
	// carry an explicitly empty alt.
	require.Contains(t, html, `alt=""`)
	require.Contains(t, html, `aria-label="Disconnect Corporate Okta"`)
}

// An unbranded issuer keeps the slug-only rendering with no logo element.
func TestConsentTemplateOmitsLogoWhenIssuerUnbranded(t *testing.T) {
	t.Parallel()

	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:     "Example Client",
		MCPSlug:        "example",
		MCPRouteBase:   "mcp",
		State:          "state",
		CSRFToken:      "csrf",
		SubjectDisplay: "user@example.com",
		ScriptURL:      "/mcp/consent-page-test.js",
		RemoteSessionCards: []remoteSessionCard{{
			ClientID:      "client-id",
			IssuerSlug:    "example-issuer",
			IssuerDisplay: "example-issuer",
			IssuerLogoURL: "",
			Connected:     true,
		}},
		ConsentEnabled: true,
	})
	require.NoError(t, err)

	html := page.String()
	require.Contains(t, html, "example-issuer")
	// The stylesheet always mentions .issuer-logo; only the element itself
	// must be absent.
	require.NotContains(t, html, `data-issuer-logo`)
	require.Contains(t, html, `aria-label="Disconnect example-issuer"`)
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

// TestConsentTemplateDisabledWithoutIslandWhenConsentDisabled pins the
// non-island path's gate: disconnected required services must still disable
// Give Access when another policy hides the island.
func TestConsentTemplateDisabledWithoutIslandWhenConsentDisabled(t *testing.T) {
	t.Parallel()

	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:      "Demo",
		MCPSlug:         "example",
		MCPRouteBase:    "mcp",
		State:           "state",
		CSRFToken:       "csrf",
		SubjectDisplay:  "user@example.com",
		RedirectURI:     "http://127.0.0.1/cb",
		ScriptURL:       "/mcp/consent-page-test.js",
		ConsentEnabled:  false,
		FirstParty:      false,
		ShowToolsIsland: false,
	})
	require.NoError(t, err)

	html := page.String()
	buttonStart := strings.Index(html, `value="approve"`)
	require.NotEqual(t, -1, buttonStart)
	buttonRegion := html[buttonStart:]
	buttonEnd := strings.Index(buttonRegion, ">")
	require.NotEqual(t, -1, buttonEnd)
	require.Contains(t, buttonRegion[:buttonEnd], "disabled")
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
