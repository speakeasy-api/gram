package platformmcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/consentpage"
)

// TestOAuthPagePreview renders every state of the Platform MCP OAuth page,
// asserting only that each one executes. The fixtures are the catalogue of
// states the page has to hold up in, and a new state belongs here before it
// ships.
//
// It doubles as the visual-review aid: point GRAM_PLATFORM_MCP_OAUTH_PREVIEW_DIR
// at a directory and the rendered pages land there instead of in a temp dir.
//
//	GRAM_PLATFORM_MCP_OAUTH_PREVIEW_DIR=/tmp/platform-oauth go test ./internal/platformmcp/ -run TestOAuthPagePreview
//
// then serve that directory and open index.html.
func TestOAuthPagePreview(t *testing.T) {
	t.Parallel()

	outDir := os.Getenv("GRAM_PLATFORM_MCP_OAUTH_PREVIEW_DIR")
	if outDir == "" {
		outDir = t.TempDir()
	}

	base := oauthPageData{
		ClientName:  "ChatGPT",
		RedirectURI: "https://chatgpt.com/connector_platform_oauth_redirect",
		State:       "12b256a1-a7b9-44f7-af8b-0ce000000000",
		CSRFToken:   "csrf",
		ScriptNonce: "preview",
		Styles:      consentpage.Styles,
	}
	with := func(edit func(*oauthPageData)) oauthPageData {
		data := base
		edit(&data)
		return data
	}

	pages := []struct {
		name string
		data oauthPageData
	}{
		{"organization", with(func(d *oauthPageData) {
			d.Title, d.Kind = "Choose an organization · Speakeasy", "organization"
			d.Organizations = []OrganizationOption{{ID: "org-1", Name: "Acme"}, {ID: "org-2", Name: "Globex"}}
		})},
		{"workspace", with(func(d *oauthPageData) {
			d.Title, d.Kind, d.WorkspaceName = "Name your organization · Speakeasy", "workspace", "Acme"
		})},
		{"workspace-error", with(func(d *oauthPageData) {
			d.Title, d.Kind, d.WorkspaceError = "Name your organization · Speakeasy", "workspace", "Enter an organization name."
		})},
		{"invitation", with(func(d *oauthPageData) {
			d.Title, d.Kind = "Join your team · Speakeasy", "invitation"
			d.Invitations = []WorkspaceInvitation{{ID: "inv-1", OrganizationName: "Acme"}}
		})},
		{"invitation-mixed", with(func(d *oauthPageData) {
			d.Title, d.Kind = "Join your team · Speakeasy", "invitation"
			d.Invitations = []WorkspaceInvitation{{ID: "inv-1", OrganizationName: "Acme"}, {ID: "inv-2", OrganizationName: "Globex", RequiresSSO: true}}
		})},
		{"sso-required", with(func(d *oauthPageData) {
			d.Title, d.Kind = "Sign in with SSO · Speakeasy", "sso-required"
			d.Invitations = []WorkspaceInvitation{{ID: "inv-1", OrganizationName: "Acme", RequiresSSO: true}}
		})},
		{"sso-required-plural", with(func(d *oauthPageData) {
			d.Title, d.Kind = "Sign in with SSO · Speakeasy", "sso-required"
			d.Invitations = []WorkspaceInvitation{{ID: "inv-1", OrganizationName: "Acme", RequiresSSO: true}, {ID: "inv-2", OrganizationName: "Globex", RequiresSSO: true}}
		})},
		{"connect", with(func(d *oauthPageData) {
			d.Title, d.Kind, d.OrganizationName = "Connect to Speakeasy", "connect", "Acme"
		})},
		{"connect-cimd", with(func(d *oauthPageData) {
			d.Title, d.Kind, d.OrganizationName, d.ClientIDOrigin = "Connect to Speakeasy", "connect", "Acme", "https://chatgpt.com"
		})},
		{"provider-connected", with(func(d *oauthPageData) {
			d.Title, d.Kind = "Sign-in complete · Speakeasy", "provider-complete"
		})},
	}

	// The stylesheet's @font-face urls point at the server's font route, which
	// nothing serves here, so copy the embedded fonts to that path under the
	// output directory for a plain static server to resolve them.
	fontDir := filepath.Join(outDir, "mcp", "consent-fonts")
	require.NoError(t, os.MkdirAll(fontDir, 0o750))
	fonts, err := consentpage.FontNames()
	require.NoError(t, err)
	for _, name := range fonts {
		data, ok := consentpage.Font(name)
		require.True(t, ok, name)
		require.NoError(t, os.WriteFile(filepath.Join(fontDir, name), data, 0o600))
	}

	var index strings.Builder
	index.WriteString("<!doctype html><title>Platform MCP OAuth page states</title><ul>")
	for _, page := range pages {
		var rendered strings.Builder
		require.NoError(t, oauthPageTemplate.Execute(&rendered, page.data), page.name)
		require.NoError(t, os.WriteFile(filepath.Join(outDir, page.name+".html"), []byte(rendered.String()), 0o600))
		fmt.Fprintf(&index, `<li><a href="%s.html">%s</a></li>`, page.name, page.name)
	}
	index.WriteString("</ul>")
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "index.html"), []byte(index.String()), 0o600))
}
