package mcp

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConsentPagePreview renders every shape of the consent page, asserting
// only that each one executes — the fixtures are the catalogue of states the
// page has to hold up in, and a new state belongs here before it ships.
//
// It doubles as the visual-review aid: point GRAM_CONSENT_PREVIEW_DIR at a
// directory and the rendered pages land there instead of in a temp dir.
//
//	GRAM_CONSENT_PREVIEW_DIR=/tmp/consent go test ./internal/mcp/ -run TestConsentPagePreview
//
// then serve that directory and open index.html.
func TestConsentPagePreview(t *testing.T) {
	t.Parallel()

	outDir := os.Getenv("GRAM_CONSENT_PREVIEW_DIR")
	if outDir == "" {
		outDir = t.TempDir()
	}

	base := consentTemplateData{
		ClientName:     "Claude Code",
		MCPSlug:        "speakeasy-team-datadog",
		MCPRouteBase:   "mcp",
		State:          "12b256a1-a7b9-44f7-af8b-0ce000000000",
		CSRFToken:      "csrf",
		SubjectDisplay: "adam@speakeasy.com",
		RedirectURI:    "http://localhost:3118/callback",
		ScriptURL:      "/consent-page.js",
		ConsentEnabled: true,
		SessionDurationOptions: []sessionDurationOption{
			{Hours: 24, Label: "1 day"},
			{Hours: 336, Label: "2 weeks (maximum)", Selected: true},
		},
	}

	connectedCard := remoteSessionCard{
		ClientID:           "client-datadog",
		IssuerSlug:         "datadog",
		IssuerDisplay:      "mcp.datadoghq.com",
		Connected:          true,
		CanRefresh:         true,
		RefreshExpiresAt:   "2026-09-05T18:00:00Z",
		RefreshExpiresIn:   "31 days",
		AutoRefreshChecked: true,
	}
	expiredCard := remoteSessionCard{
		ClientID:      "client-jira",
		IssuerSlug:    "jira",
		IssuerDisplay: "Atlassian Jira",
		Expired:       true,
	}
	disconnectedCard := remoteSessionCard{
		ClientID:      "client-slack",
		IssuerSlug:    "slack",
		IssuerDisplay: "Slack",
	}

	withIsland := func(d consentTemplateData) consentTemplateData {
		d.ShowToolsIsland = true
		d.ConsentToolsURL = "/mcp/example/connect/mcp"
		d.ConsentToolsScriptURL = "/consent-tools.js"
		return d
	}
	withCards := func(d consentTemplateData, cards ...remoteSessionCard) consentTemplateData {
		d.RemoteSessionCards = cards
		for _, c := range cards {
			if c.Connected {
				d.ConnectedCardCount++
			}
		}
		d.AutoRefreshPolicy = autoRefreshUserControlled
		d.AutoRefreshOn = true
		d.AutoRefreshHasSessions = true
		return d
	}

	variants := []struct {
		name string
		data consentTemplateData
	}{
		{"no-services", base},
		{"no-services-tool-picker", withIsland(base)},
		{"single-service-connected", withIsland(withCards(base, connectedCard))},
		// The only way a single unlinked service renders at all: auto-connect
		// already fired and the upstream leg was denied or failed, so the latch
		// keeps the user on a page they can act on. A first visit 303s straight
		// to the provider and never reaches the template.
		{"single-service-auto-connect-failed", func() consentTemplateData {
			d := withCards(base, disconnectedCard)
			d.ConsentEnabled = false
			return d
		}()},
		{"multi-service-mixed", withIsland(withCards(base, connectedCard, expiredCard, disconnectedCard))},
		{"loopback-warning", func() consentTemplateData {
			d := withIsland(withCards(base, connectedCard))
			d.ClientIDOrigin = "claude.ai"
			d.LoopbackRedirectWarning = true
			return d
		}()},
		{"auto-refresh-enforced", func() consentTemplateData {
			d := withCards(base, connectedCard)
			d.AutoRefreshPolicy = autoRefreshEnforced
			return d
		}()},
		{"auto-refresh-disabled", func() consentTemplateData {
			d := withCards(base, connectedCard)
			d.AutoRefreshPolicy = autoRefreshDisabled
			d.AutoRefreshOn = false
			return d
		}()},
		{"first-party-incomplete", func() consentTemplateData {
			d := withCards(base, connectedCard, disconnectedCard)
			d.FirstParty = true
			d.ClientName = "Gram"
			d.RedirectURI = ""
			d.SessionDurationOptions = nil
			return d
		}()},
		{"first-party-auto-close", func() consentTemplateData {
			d := withCards(base, connectedCard)
			d.FirstParty = true
			d.ClientName = "Gram"
			d.RedirectURI = ""
			d.SessionDurationOptions = nil
			d.AutoClose = true
			return d
		}()},
	}

	// The stylesheet's @font-face urls point at the server's font route, which
	// nothing serves here — copy the embedded files to that path under the
	// output directory so a plain static server resolves them.
	fontDir := filepath.Join(outDir, "mcp", "consent-fonts")
	require.NoError(t, os.MkdirAll(fontDir, 0o750))
	fonts, err := consentPageFontNames()
	require.NoError(t, err)
	for _, name := range fonts {
		data, err := consentPageAssets.ReadFile("consent_assets/page/" + name)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(fontDir, name), data, 0o600))
	}

	var index strings.Builder
	index.WriteString("<!doctype html><meta charset=\"utf-8\"><title>Consent page variants</title>")
	index.WriteString("<style>body{font-family:system-ui;margin:3rem;line-height:2}</style><h1>Consent page variants</h1><ul>")

	for _, v := range variants {
		data := v.data
		data.Styles = consentPageStyles
		var page bytes.Buffer
		require.NoError(t, consentTemplate.Execute(&page, data), v.name)
		require.NoError(t, os.WriteFile(filepath.Join(outDir, v.name+".html"), page.Bytes(), 0o600))
		fmt.Fprintf(&index, `<li><a href="%s.html">%s</a></li>`, template.HTMLEscapeString(v.name), template.HTMLEscapeString(v.name))
	}
	index.WriteString("</ul>")
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "index.html"), []byte(index.String()), 0o600))

	t.Logf("wrote %d consent page variants to %s", len(variants), outDir)
}
