package mcpmetadata

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/consentpage"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

//go:embed organization_gateway_page.html
var organizationGatewayPageHTML string

// organizationGatewayPageScript is the page's only script, the copy buttons.
//
//go:embed organization_gateway_page.js
var organizationGatewayPageScript string

var organizationGatewayPageTemplate = template.Must(template.Must(template.New("organization-gateway-page").Parse(organizationGatewayPageHTML)).Parse(consentpage.LogoTemplate))

// organizationGatewayClient is one MCP client's setup steps on the
// organization gateway page.
type organizationGatewayClient struct {
	Name        string
	Description string
	Steps       []organizationGatewayStep
}

type organizationGatewayStep struct {
	Number    int
	Text      string
	Code      string
	Link      template.URL
	LinkLabel string
}

type organizationGatewayPageData struct {
	Styles           template.CSS
	Script           template.JS
	ScriptNonce      string
	OrganizationName string
	Available        bool
	URL              string
	DashboardURL     string
	Clients          []organizationGatewayClient
}

// ServeOrganizationGatewayPage serves the browser landing page at an
// organization gateway's URL to the organization's members. A visitor without
// a dashboard session is sent to sign in and brought back. Everyone else, and
// every request on a custom domain, where the gateway is not served, gets the
// same not found page an unknown organization does, so the page never tells
// a non-member which organizations exist or what they are called.
func (s *Service) ServeOrganizationGatewayPage(w http.ResponseWriter, r *http.Request, organizationSlug string) error {
	ctx := r.Context()

	if customdomains.FromContext(ctx) != nil || organizationSlug == "" || strings.Contains(organizationSlug, "/") {
		return s.serveNotFoundPage(w, organizationSlug)
	}

	sessionToken, _ := contextvalues.GetSessionTokenFromContext(ctx)
	ctx, _ = s.auth.Authorize(ctx, sessionToken, &security.APIKeyScheme{
		Name:           constants.SessionSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" {
		if s.serverURL == nil {
			return s.serveNotFoundPage(w, organizationSlug)
		}
		http.Redirect(w, r, s.loginRedirectURL(r), http.StatusFound)
		return nil
	}

	organization, err := s.orgsRepo.GetOrganizationMetadataBySlug(ctx, organizationSlug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return s.serveNotFoundPage(w, organizationSlug)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load organization").LogError(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogOrganizationID(organization.ID))

	// Read from the database rather than the session's cached organization
	// list, so a member added moments ago is not turned away.
	memberships, err := s.orgsRepo.ListOrganizationsForUser(ctx, conv.ToPGText(authCtx.UserID))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list organizations for user").LogError(ctx, logger)
	}
	if !slices.ContainsFunc(memberships, func(m organizations_repo.ListOrganizationsForUserRow) bool { return m.ID == organization.ID }) {
		logger.InfoContext(ctx, "serving not found page: visitor is not an organization member")
		return s.serveNotFoundPage(w, organizationSlug)
	}

	_, unavailable, err := orggateway.Availability(ctx, s.db, organization)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve organization gateway availability").LogError(ctx, logger)
	}
	if unavailable == orggateway.UnavailabilityOrganizationDisabled {
		return s.serveNotFoundPage(w, organizationSlug)
	}

	data := organizationGatewayPageData{
		Styles:           consentpage.Styles,
		Script:           "",
		ScriptNonce:      "",
		OrganizationName: organization.Name,
		Available:        unavailable == orggateway.UnavailabilityNone,
		URL:              "",
		DashboardURL:     s.siteURL.String(),
		Clients:          nil,
	}
	if data.Available {
		base, err := url.Parse(requestorigin.BaseURL(ctx, s.serverURL.String()))
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "resolve organization gateway base url").LogError(ctx, logger)
		}
		data.URL = orggateway.URL(base, organization.Slug)
		data.Clients, err = organizationGatewayClients(organization.Slug, data.URL)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "build organization gateway setup steps").LogError(ctx, logger)
		}
	}

	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate organization gateway page script nonce").LogError(ctx, logger)
	}
	data.ScriptNonce = base64.RawURLEncoding.EncodeToString(nonce)
	// #nosec G203 -- the script is embedded at compile time; no request data
	// reaches it.
	data.Script = template.JS(organizationGatewayPageScript)

	var buf bytes.Buffer
	if err := organizationGatewayPageTemplate.Execute(&buf, data); err != nil {
		return oops.E(oops.CodeUnexpected, err, "render organization gateway page").LogError(ctx, logger)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The page names the organization, so it is per-person.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; font-src 'self'; script-src 'nonce-"+data.ScriptNonce+"'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(buf.Bytes()); err != nil {
		logger.ErrorContext(ctx, "write organization gateway page", attr.SlogError(err))
	}
	return nil
}

// organizationGatewayClients is each supported MCP client's setup steps,
// matching the dashboard's Organization Gateway install steps. name is the
// server name the client lists the gateway under.
func organizationGatewayClients(name, mcpURL string) ([]organizationGatewayClient, error) {
	cursorURL, err := buildCursorInstallURL(name, mcpURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build cursor install URL: %w", err)
	}
	cursorLink, err := safeTemplateURL(cursorURL, "cursor")
	if err != nil {
		return nil, fmt.Errorf("sanitize cursor install URL: %w", err)
	}
	vsCodeURL, err := buildVSCodeInstallURL(name, mcpURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build vscode install URL: %w", err)
	}
	vsCodeLink, err := safeTemplateURL(vsCodeURL, "vscode")
	if err != nil {
		return nil, fmt.Errorf("sanitize vscode install URL: %w", err)
	}
	openCodeConfig, err := json.MarshalIndent(map[string]any{
		"mcp": map[string]any{name: map[string]any{"type": "remote", "url": mcpURL, "enabled": true}},
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal opencode config: %w", err)
	}

	return []organizationGatewayClient{
		gatewayClient("Claude Code", "",
			codeStep("Run this in your terminal.", "claude mcp add --transport http --scope user "+shellQuote(name)+" \\\n  "+shellQuote(mcpURL)),
			textStep("Open /mcp in Claude Code, select the gateway and choose Authenticate."),
		),
		gatewayClient("Claude", "Claude on the web and desktop, including Cowork",
			codeStep("In Claude, open Settings › Connectors, choose Add custom connector and paste the server URL.", mcpURL),
			textStep("Choose Connect and sign in with your Speakeasy account."),
		),
		gatewayClient("Codex", "",
			codeStep("Run this in your terminal.", "codex mcp add "+shellQuote(name)+" \\\n  --url "+shellQuote(mcpURL)),
			codeStep("Run this to sign in.", "codex mcp login "+shellQuote(name)),
		),
		gatewayClient("Cursor", "",
			linkStep("Open Cursor and add the gateway to your MCP settings.", cursorLink, "Add to Cursor"),
			textStep("In Cursor Settings › Tools & MCP, sign in to the gateway when Cursor asks."),
		),
		gatewayClient("VS Code", "",
			linkStep("Open VS Code and add the gateway to your MCP servers.", vsCodeLink, "Add to VS Code"),
			textStep("Start the server in VS Code and sign in when it asks."),
		),
		gatewayClient("opencode", "",
			codeStep("Merge this into ~/.config/opencode/opencode.json. opencode signs you in the first time it connects.", string(openCodeConfig)),
		),
		gatewayClient("ChatGPT", "ChatGPT on the web",
			textStep("Open Settings › Apps › Advanced settings and turn on developer mode."),
			codeStep("Create an app with this URL and OAuth authentication.", mcpURL),
		),
		gatewayClient("Other clients", "Any client that supports remote MCP servers",
			codeStep("Add the server URL as a Streamable HTTP server and sign in when your client asks.", mcpURL),
		),
	}, nil
}

func gatewayClient(name, description string, steps ...organizationGatewayStep) organizationGatewayClient {
	for i := range steps {
		steps[i].Number = i + 1
	}
	return organizationGatewayClient{Name: name, Description: description, Steps: steps}
}

func textStep(text string) organizationGatewayStep {
	return organizationGatewayStep{Number: 0, Text: text, Code: "", Link: "", LinkLabel: ""}
}

func codeStep(text, code string) organizationGatewayStep {
	return organizationGatewayStep{Number: 0, Text: text, Code: code, Link: "", LinkLabel: ""}
}

func linkStep(text string, link template.URL, label string) organizationGatewayStep {
	return organizationGatewayStep{Number: 0, Text: text, Code: "", Link: link, LinkLabel: label}
}

// shellQuote quotes a value for a POSIX shell so it survives copy-paste as
// one argument.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
