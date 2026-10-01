// Package oktaseed holds the hand-curated starter set of Gram-owned catalog
// entries that map Okta Integration Network applications to MCP servers, and
// applies it idempotently. Okta exposes nothing that links an app to an MCP
// server, so this table is the source until a staff helper replaces it.
package oktaseed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
)

// Remote is one endpoint of a vendor's MCP server.
type Remote struct {
	Type string
	URL  string
}

// Vendor is one seeded catalog entry with its Okta mapping. Every value is
// public vendor data verified against a tenant or the vendor's metadata.
type Vendor struct {
	// Name is the catalog server name; it is immutable once created.
	Name string
	// Title is the display title.
	Title string
	// Description is the catalog description.
	Description string
	// WebsiteURL is the vendor's site.
	WebsiteURL string
	// DocumentationURL is the public documentation for the MCP server.
	DocumentationURL string
	// IconURL is a PNG the vendor hosts for its own product.
	IconURL string
	// SupportsDCR records that the vendor's authorization server metadata
	// publishes a registration endpoint.
	SupportsDCR bool
	// Remotes are the endpoints in preference order; immutable once created.
	Remotes []Remote
	// Mapping is the Okta namespace written to the entry.
	Mapping mcpregistry.OktaMapping
}

const (
	// applyRevision is raised when Apply changes what it writes for the same
	// Vendors, so that deployed environments apply the seed again.
	applyRevision = 1

	// versionHexLength keeps 48 bits of the content hash: enough to tell seed
	// versions apart, short enough to read in a workflow ID.
	versionHexLength = 12
)

// Version identifies what the seed would write. It changes when Vendors or
// applyRevision does, and only then.
func Version() string {
	return versionOf(Vendors)
}

func versionOf(vendors []Vendor) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%d %#v", applyRevision, vendors))
	return hex.EncodeToString(sum[:])[:versionHexLength]
}

// Vendors is the starter set. Keep entries alphabetical by Name. Okta names
// are the keys of the public OIN catalog listings, checked in the catalog;
// integrator-prefixed keys belong to listings published from an integrator
// account and are just as public. Issuers are the authorization server Okta
// accepted as the identity assertion audience; entries without Cross App
// Access fields have not been checked for it. Endpoints are the ones
// organizations already run.
var Vendors = []Vendor{
	{
		Name:             "ai.granola/mcp",
		Title:            "Granola",
		Description:      "Granola's hosted MCP server for meeting notes.",
		WebsiteURL:       "https://www.granola.ai",
		DocumentationURL: "https://www.granola.ai/docs/mcp",
		IconURL:          "https://www.granola.ai/favicon/apple-touch-icon.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.granola.ai/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"integrator-4080826_granola_1"},
			OINIntegrationID: "",
			XAASignOnModes:   []string{"SAML_2_0"},
			XAAIssuer:        "https://mcp-auth.granola.ai",
		},
	},
	{
		Name:             "app.linear/mcp",
		Title:            "Linear",
		Description:      "Linear's hosted MCP server for issues, projects, and cycles.",
		WebsiteURL:       "https://linear.app",
		DocumentationURL: "https://linear.app/docs/mcp",
		IconURL:          "https://linear.app/static/apple-touch-icon.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.linear.app/mcp"},
			{Type: "sse", URL: "https://mcp.linear.app/sse"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"integrator-4080826_linear_1"},
			OINIntegrationID: "4080826",
			XAASignOnModes:   []string{"SAML_2_0"},
			XAAIssuer:        "https://auth.linear.com",
		},
	},
	{
		Name:             "co.lucid/mcp",
		Title:            "Lucid",
		Description:      "Lucid's hosted MCP server for diagrams and whiteboards.",
		WebsiteURL:       "https://lucid.co",
		DocumentationURL: "",
		IconURL:          "https://cdn-cashy-static-assets.lucidchart.com/marketing/images/LucidSoftwareFavicon.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.lucid.app/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"lucid"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.amplitude/mcp",
		Title:            "Amplitude",
		Description:      "Amplitude's hosted MCP server for product analytics.",
		WebsiteURL:       "https://amplitude.com",
		DocumentationURL: "",
		IconURL:          "https://amplitude.com/nextjs-public/favicon/apple-touch-icon.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.amplitude.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"amplitude"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.atlassian/mcp",
		Title:            "Atlassian",
		Description:      "Atlassian's hosted MCP server for Jira and Confluence Cloud.",
		WebsiteURL:       "https://www.atlassian.com",
		DocumentationURL: "https://support.atlassian.com/atlassian-rovo-mcp-server/",
		IconURL:          "https://wac-cdn.atlassian.com/assets/img/favicons/atlassian/favicon.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.atlassian.com/v1/mcp"},
			{Type: "sse", URL: "https://mcp.atlassian.com/v1/sse"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"atlassian"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.box/mcp",
		Title:            "Box",
		Description:      "Box's hosted MCP server for files and folders.",
		WebsiteURL:       "https://www.box.com",
		DocumentationURL: "",
		IconURL:          "",
		SupportsDCR:      false,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.box.com"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"boxnet"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.brex/mcp",
		Title:            "Brex",
		Description:      "Brex's hosted MCP server for spend and expenses.",
		WebsiteURL:       "https://www.brex.com",
		DocumentationURL: "",
		IconURL:          "https://www.brex.com/apple-touch-icon.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://api.brex.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"brex"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.canva/mcp",
		Title:            "Canva",
		Description:      "Canva's hosted MCP server for designs and brand assets.",
		WebsiteURL:       "https://www.canva.com",
		DocumentationURL: "https://www.canva.dev/docs/apps/mcp-server/",
		IconURL:          "https://static.canva.com/domain-assets/canva/static/images/apple-touch-180x180-1.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.canva.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"canva"},
			OINIntegrationID: "",
			XAASignOnModes:   []string{"SAML_2_0"},
			XAAIssuer:        "https://mcp.canva.com",
		},
	},
	{
		Name:             "com.clickup/mcp",
		Title:            "ClickUp",
		Description:      "ClickUp's hosted MCP server for tasks and docs.",
		WebsiteURL:       "https://clickup.com",
		DocumentationURL: "",
		IconURL:          "https://clickup.com/favicons/apple-touch-icon.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.clickup.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"clickup"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.cloudflare/mcp",
		Title:            "Cloudflare",
		Description:      "Cloudflare's hosted MCP server for its developer platform.",
		WebsiteURL:       "https://www.cloudflare.com",
		DocumentationURL: "",
		IconURL:          "https://dash.cloudflare.com/apple-touch-icon.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.cloudflare.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"cloudflare", "integrator-8026030_cloudflareone_1"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.datadoghq/mcp",
		Title:            "Datadog",
		Description:      "Datadog's hosted MCP server for monitors, logs, and incidents.",
		WebsiteURL:       "https://www.datadoghq.com",
		DocumentationURL: "https://docs.datadoghq.com/bits_ai/mcp_server/",
		IconURL:          "https://corp.dd-static.net/img/favicons/apple-touch-icon.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.datadoghq.com/v1/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"datadog"},
			OINIntegrationID: "",
			XAASignOnModes:   []string{"SAML_2_0"},
			XAAIssuer:        "https://app.datadoghq.com",
		},
	},
	{
		Name:             "com.fullstory/mcp",
		Title:            "Fullstory",
		Description:      "Fullstory's hosted MCP server for behavioral analytics.",
		WebsiteURL:       "https://www.fullstory.com",
		DocumentationURL: "",
		IconURL:          "https://www.fullstory.com/icons/icon-48x48.png?v=7cac1bc6d731746740d72903d25cd5bf",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://api.fullstory.com/mcp/fullstory"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"dev-85865693_fullstory_1"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.github/mcp",
		Title:            "GitHub",
		Description:      "GitHub's hosted MCP server for repositories, issues, and pull requests.",
		WebsiteURL:       "https://github.com",
		DocumentationURL: "https://docs.github.com/en/copilot/how-tos/context/model-context-protocol/using-the-github-mcp-server",
		IconURL:          "https://github.com/fluidicon.png",
		SupportsDCR:      false,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://api.githubcopilot.com/mcp/"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"github"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.hubspot/mcp",
		Title:            "HubSpot",
		Description:      "HubSpot's hosted MCP server for CRM records.",
		WebsiteURL:       "https://www.hubspot.com",
		DocumentationURL: "",
		IconURL:          "https://www.hubspot.com/hubfs/HubSpot_Logos/HubSpot-Inversed-Favicon.png",
		SupportsDCR:      false,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.hubspot.com"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"hubspot", "hubspotsaml"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.intercom/mcp",
		Title:            "Intercom",
		Description:      "Intercom's hosted MCP server for conversations and contacts.",
		WebsiteURL:       "https://www.intercom.com",
		DocumentationURL: "",
		IconURL:          "https://www.intercom.com/intercom-marketing-site/favicons/favicon-32x32.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.intercom.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"intercom"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.miro/mcp",
		Title:            "Miro",
		Description:      "Miro's hosted MCP server for boards.",
		WebsiteURL:       "https://miro.com",
		DocumentationURL: "",
		IconURL:          "https://framerusercontent.com/images/6FBG66PBxjV2QFaDfIdUi5mi9A.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.miro.com/"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"realtime_board"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.monday/mcp",
		Title:            "monday.com",
		Description:      "monday.com's hosted MCP server for boards and items.",
		WebsiteURL:       "https://monday.com",
		DocumentationURL: "",
		IconURL:          "https://cdn.prod.website-files.com/656da6fea306219773d04208/65af6bd6e742d497b5f23f69_645898132bbaac20f1963919_256x256.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.monday.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"mondaycom"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.navan/mcp",
		Title:            "Navan",
		Description:      "Navan's hosted MCP server for travel and expenses.",
		WebsiteURL:       "https://navan.com",
		DocumentationURL: "",
		IconURL:          "https://navan.com/favicon.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.navan.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"navan"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.notion/mcp",
		Title:            "Notion",
		Description:      "Notion's hosted MCP server for pages and databases.",
		WebsiteURL:       "https://www.notion.com",
		DocumentationURL: "https://developers.notion.com/docs/mcp",
		IconURL:          "https://www.notion.com/front-static/logo-ios.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.notion.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"notion"},
			OINIntegrationID: "",
			XAASignOnModes:   []string{"SAML_2_0"},
			XAAIssuer:        "https://mcp.notion.com",
		},
	},
	{
		Name:             "com.pagerduty/mcp",
		Title:            "PagerDuty",
		Description:      "PagerDuty's hosted MCP server for incidents and on-call.",
		WebsiteURL:       "https://www.pagerduty.com",
		DocumentationURL: "",
		IconURL:          "https://www.pagerduty.com/favicon/prod/apple-touch-icon.png",
		SupportsDCR:      false,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.pagerduty.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"pagerduty"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.ramp/mcp",
		Title:            "Ramp",
		Description:      "Ramp's hosted MCP server for spend and expenses.",
		WebsiteURL:       "https://ramp.com",
		DocumentationURL: "",
		IconURL:          "https://ramp.com/apple-touch-icon.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.ramp.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"ramp", "rampcom"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.slack/mcp",
		Title:            "Slack",
		Description:      "Slack's hosted MCP server for channels, messages, and search.",
		WebsiteURL:       "https://slack.com",
		DocumentationURL: "https://docs.slack.dev/ai/mcp-server/",
		IconURL:          "https://a.slack-edge.com/80588/marketing/img/meta/slack_hash_256.png",
		SupportsDCR:      false,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.slack.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"slack"},
			OINIntegrationID: "",
			XAASignOnModes:   []string{"SAML_2_0", "OPENID_CONNECT"},
			XAAIssuer:        "https://mcp.slack.com",
		},
	},
	{
		Name:             "com.stripe/mcp",
		Title:            "Stripe",
		Description:      "Stripe's hosted MCP server for payments and billing.",
		WebsiteURL:       "https://stripe.com",
		DocumentationURL: "",
		IconURL:          "https://images.stripeassets.com/fzn2n1nzq965/4vVgZi0ZMoEzOhkcv7EVwK/8cce6fdcf2733b2ec8e99548908847ed/favicon.png?w=180&h=180",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.stripe.com"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"dev-67337717_stripe_1"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.supabase/mcp",
		Title:            "Supabase",
		Description:      "Supabase's hosted MCP server for projects, databases, and edge functions.",
		WebsiteURL:       "https://supabase.com",
		DocumentationURL: "https://supabase.com/docs/guides/getting-started/mcp",
		IconURL:          "https://supabase.com/favicon/favicon-196x196.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.supabase.com/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"integrator-4080826_supabase_1"},
			OINIntegrationID: "",
			XAASignOnModes:   []string{"SAML_2_0"},
			XAAIssuer:        "https://api.supabase.com",
		},
	},
	{
		Name:             "com.vercel/mcp",
		Title:            "Vercel",
		Description:      "Vercel's hosted MCP server for projects and deployments.",
		WebsiteURL:       "https://vercel.com",
		DocumentationURL: "",
		IconURL:          "https://assets.vercel.com/image/upload/q_auto/front/favicon/vercel/apple-touch-icon-57x57.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.vercel.com"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"vercel"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.zapier/mcp",
		Title:            "Zapier",
		Description:      "Zapier's hosted MCP server for automations.",
		WebsiteURL:       "https://zapier.com",
		DocumentationURL: "",
		IconURL:          "",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.zapier.com/api/v1/connect"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"zapier"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "com.zoom/mcp",
		Title:            "Zoom",
		Description:      "Zoom's hosted MCP server for meetings.",
		WebsiteURL:       "https://www.zoom.com",
		DocumentationURL: "",
		IconURL:          "https://www.zoom.com/apple-touch-icon.png",
		SupportsDCR:      false,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.zoom.us/mcp/zoom/streamable"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"zoomus"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
	{
		Name:             "io.sentry/mcp",
		Title:            "Sentry",
		Description:      "Sentry's hosted MCP server for errors and issues.",
		WebsiteURL:       "https://sentry.io",
		DocumentationURL: "",
		IconURL:          "https://sentry-brand.storage.googleapis.com/sentry-glyph-black.png",
		SupportsDCR:      true,
		Remotes: []Remote{
			{Type: "streamable-http", URL: "https://mcp.sentry.dev/mcp"},
		},
		Mapping: mcpregistry.OktaMapping{
			OINNames:         []string{"sentry"},
			OINIntegrationID: "",
			XAASignOnModes:   nil,
			XAAIssuer:        "",
		},
	},
}

// Result counts what Apply did.
type Result struct {
	Created   int
	Updated   int
	Unchanged int
}

// Apply creates missing vendor entries and writes the Okta namespace onto
// existing ones without touching their name, remotes or other metadata. It
// is safe to run repeatedly.
func Apply(ctx context.Context, logger *slog.Logger, svc *mcpregistry.Service) (Result, error) {
	var result Result
	for _, v := range Vendors {
		outcome, err := apply(ctx, svc, v)
		if err != nil {
			return result, fmt.Errorf("seed %s: %w", v.Name, err)
		}
		logger.InfoContext(ctx, "okta catalog seed applied", attr.SlogRegistryEntryName(v.Name), attr.SlogRegistrySeedOutcome(outcome))
		switch outcome {
		case "created":
			result.Created++
		case "updated":
			result.Updated++
		default:
			result.Unchanged++
		}
	}
	return result, nil
}

func apply(ctx context.Context, svc *mcpregistry.Service, v Vendor) (string, error) {
	existing, err := svc.GetByName(ctx, v.Name)
	switch {
	case errors.Is(err, mcpregistry.ErrNotFound):
		data, err := v.record()
		if err != nil {
			return "", err
		}
		if _, err := svc.Create(ctx, data); err != nil {
			return "", fmt.Errorf("create: %w", err)
		}
		return "created", nil
	case err != nil:
		return "", fmt.Errorf("lookup: %w", err)
	}
	current, err := mcpregistry.ParseOktaMapping(existing.Data)
	iconed, iconAdded, iconErr := withIcon(existing.Data, v.IconURL)
	if iconErr != nil {
		return "", iconErr
	}
	iconed, dcrAdded, dcrErr := withSupportsDCR(iconed, v.SupportsDCR)
	if dcrErr != nil {
		return "", dcrErr
	}
	if err == nil && equalMapping(current, v.Mapping) && !iconAdded && !dcrAdded {
		// Nothing to write, but a stored record that no longer meets the
		// contract is reported rather than silently left alone.
		if issues := svc.ValidateStored(existing.Data); len(issues) > 0 {
			return "", &mcpregistry.InvalidError{Issues: issues}
		}
		return "unchanged", nil
	}
	data, err := withMapping(iconed, v.Mapping)
	if err != nil {
		return "", err
	}
	if _, err := svc.Save(ctx, existing.ID, mcpregistry.Token(existing), data); err != nil {
		return "", fmt.Errorf("save: %w", err)
	}
	return "updated", nil
}

// record builds a fresh catalog record in the registry's ServerResponse shape.
func (v Vendor) record() (json.RawMessage, error) {
	remotes := make([]map[string]any, 0, len(v.Remotes))
	for _, r := range v.Remotes {
		remotes = append(remotes, map[string]any{"type": r.Type, "url": r.URL})
	}
	server := map[string]any{
		"name":        v.Name,
		"title":       v.Title,
		"description": v.Description,
		"version":     "1.0.0",
		"remotes":     remotes,
	}
	if v.WebsiteURL != "" {
		server["websiteUrl"] = v.WebsiteURL
	}
	if v.IconURL != "" {
		server["icons"] = []map[string]any{iconEntry(v.IconURL)}
	}
	meta := map[string]any{mcpregistry.OktaNamespace: v.Mapping}
	catalog := map[string]any{}
	if v.DocumentationURL != "" {
		catalog["documentationUrl"] = v.DocumentationURL
	}
	if v.SupportsDCR {
		catalog["supportsDcr"] = true
	}
	if len(catalog) > 0 {
		meta[catalogNamespace] = catalog
	}
	data, err := json.Marshal(map[string]any{"server": server, "_meta": meta})
	if err != nil {
		return nil, fmt.Errorf("encode record: %w", err)
	}
	return data, nil
}

// withMapping replaces only the Okta namespace, keeping every other byte of
// the stored record as raw JSON.
func withMapping(data json.RawMessage, mapping mcpregistry.OktaMapping) (json.RawMessage, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode record: %w", err)
	}
	var meta map[string]json.RawMessage
	if raw, ok := root["_meta"]; ok {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	// A stored "_meta": null decodes to a nil map; repair it rather than panic.
	if meta == nil {
		meta = map[string]json.RawMessage{}
	}
	encoded, err := json.Marshal(mapping)
	if err != nil {
		return nil, fmt.Errorf("encode mapping: %w", err)
	}
	meta[mcpregistry.OktaNamespace] = encoded
	rawMeta, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("encode metadata: %w", err)
	}
	root["_meta"] = rawMeta
	out, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode record: %w", err)
	}
	return out, nil
}

const catalogNamespace = "com.speakeasy.ai/catalog"

func iconEntry(src string) map[string]any {
	return map[string]any{"src": src, "mimeType": "image/png"}
}

// withIcon gives a stored record the seed icon only when it has none, so an
// icon staff chose is never replaced.
func withIcon(data json.RawMessage, iconURL string) (json.RawMessage, bool, error) {
	if iconURL == "" {
		return data, false, nil
	}
	var root, server map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, false, fmt.Errorf("decode record: %w", err)
	}
	if err := json.Unmarshal(root["server"], &server); err != nil {
		return nil, false, fmt.Errorf("decode server: %w", err)
	}
	var icons []json.RawMessage
	if raw, ok := server["icons"]; ok {
		if err := json.Unmarshal(raw, &icons); err != nil {
			return nil, false, fmt.Errorf("decode icons: %w", err)
		}
	}
	if len(icons) > 0 {
		return data, false, nil
	}
	encoded, err := json.Marshal([]map[string]any{iconEntry(iconURL)})
	if err != nil {
		return nil, false, fmt.Errorf("encode icons: %w", err)
	}
	server["icons"] = encoded
	if root["server"], err = json.Marshal(server); err != nil {
		return nil, false, fmt.Errorf("encode server: %w", err)
	}
	out, err := json.Marshal(root)
	if err != nil {
		return nil, false, fmt.Errorf("encode record: %w", err)
	}
	return out, true, nil
}

// withSupportsDCR records registration support only when the stored record
// says nothing either way, so a value staff set is never replaced.
func withSupportsDCR(data json.RawMessage, supported bool) (json.RawMessage, bool, error) {
	if !supported {
		return data, false, nil
	}
	var root, meta, catalog map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, false, fmt.Errorf("decode record: %w", err)
	}
	if raw, ok := root["_meta"]; ok {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, false, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if meta == nil {
		meta = map[string]json.RawMessage{}
	}
	if raw, ok := meta[catalogNamespace]; ok {
		if err := json.Unmarshal(raw, &catalog); err != nil {
			return nil, false, fmt.Errorf("decode catalog metadata: %w", err)
		}
	}
	if catalog == nil {
		catalog = map[string]json.RawMessage{}
	}
	if _, ok := catalog["supportsDcr"]; ok {
		return data, false, nil
	}
	catalog["supportsDcr"] = json.RawMessage("true")
	var err error
	if meta[catalogNamespace], err = json.Marshal(catalog); err != nil {
		return nil, false, fmt.Errorf("encode catalog metadata: %w", err)
	}
	if root["_meta"], err = json.Marshal(meta); err != nil {
		return nil, false, fmt.Errorf("encode metadata: %w", err)
	}
	out, err := json.Marshal(root)
	if err != nil {
		return nil, false, fmt.Errorf("encode record: %w", err)
	}
	return out, true, nil
}

func equalMapping(a, b mcpregistry.OktaMapping) bool {
	x, errX := json.Marshal(a)
	y, errY := json.Marshal(b)
	return errX == nil && errY == nil && string(x) == string(y)
}
