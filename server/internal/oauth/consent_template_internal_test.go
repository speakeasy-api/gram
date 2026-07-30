package oauth

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func renderConsentPage(t *testing.T, data consentPageData) string {
	t.Helper()

	tmpl, err := template.New("oauth_consent").Parse(consentPageTmplData)
	require.NoError(t, err)

	var page bytes.Buffer
	require.NoError(t, tmpl.Execute(&page, data))

	return page.String()
}

func TestConsentTemplateRendersBothDecisions(t *testing.T) {
	t.Parallel()

	page := renderConsentPage(t, consentPageData{
		ConsentID:    "consent-abc",
		ClientName:   "Test Client",
		MCPSlug:      "my-server",
		MCPURL:       "https://app.example.com/mcp/my-server",
		RedirectURI:  "http://localhost:8080/callback",
		ProviderSlug: "okta",
		Scopes:       []string{"read:things", "write:things"},
	})

	require.Contains(t, page, `action="/oauth/my-server/consent"`, "forms must post back to the slug's consent route")
	require.Contains(t, page, `value="consent-abc"`, "both forms must carry the consent id")
	require.Contains(t, page, `value="approve"`)
	require.Contains(t, page, `value="deny"`, "cancel must always be offered")
	require.Equal(t, 2, strings.Count(page, `name="consent_id"`), "approve and deny each need the consent id")

	require.Contains(t, page, "Test Client")
	require.Contains(t, page, "https://app.example.com/mcp/my-server")
	require.Contains(t, page, "http://localhost:8080/callback")
	require.Contains(t, page, "okta")
	require.Contains(t, page, "read:things")
	require.Contains(t, page, "write:things")
}

// The whole point of parking the authorization server-side is that the code
// never reaches the browser until the user approves. The template has no
// field to leak it, and this pins that shape against future edits.
func TestConsentTemplateHasNoFieldCarryingTheAuthorizationCode(t *testing.T) {
	t.Parallel()

	require.NotContains(t, consentPageTmplData, "ApproveURL")
	require.NotContains(t, consentPageTmplData, "DenyURL")
	require.NotContains(t, consentPageTmplData, ".Code")
}

func TestConsentTemplateEscapesClientName(t *testing.T) {
	t.Parallel()

	page := renderConsentPage(t, consentPageData{
		ConsentID:   "consent-abc",
		ClientName:  `<script>alert(1)</script>`,
		MCPSlug:     "my-server",
		MCPURL:      "https://app.example.com/mcp/my-server",
		RedirectURI: "http://localhost:8080/callback",
	})

	// Client names come from unauthenticated dynamic client registration, so
	// they are fully attacker-controlled.
	require.NotContains(t, page, "<script>alert(1)</script>")
	require.Contains(t, page, "&lt;script&gt;")
}

func TestConsentTemplateOmitsOptionalSections(t *testing.T) {
	t.Parallel()

	page := renderConsentPage(t, consentPageData{
		ConsentID:   "consent-abc",
		ClientName:  "Test Client",
		MCPSlug:     "my-server",
		MCPURL:      "https://app.example.com/mcp/my-server",
		RedirectURI: "http://localhost:8080/callback",
	})

	require.NotContains(t, page, "Requested scopes", "no scopes means no scope block")
	require.NotContains(t, page, "Signed in with", "no provider slug means no provider row")
}
