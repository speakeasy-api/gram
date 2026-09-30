// Package oktaseed holds the hand-curated starter set of Gram-owned catalog
// entries that map Okta Integration Network applications to MCP servers, and
// applies it idempotently. Okta exposes nothing that links an app to an MCP
// server, so this table is the source until a staff helper replaces it.
package oktaseed

import (
	"context"
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
	// Remotes are the endpoints in preference order; immutable once created.
	Remotes []Remote
	// Mapping is the Okta namespace written to the entry.
	Mapping mcpregistry.OktaMapping
}

// Vendors is the starter set. Keep entries alphabetical by Name. Okta names
// were read from a tenant's application list; integrator-prefixed names are
// the keys of instances created from an integrator listing and may differ
// from the vendor's public OIN listing. Issuers are the authorization server
// Okta accepted as the identity assertion audience.
var Vendors = []Vendor{
	{
		Name:             "ai.granola/mcp",
		Title:            "Granola",
		Description:      "Granola's hosted MCP server for meeting notes.",
		WebsiteURL:       "https://www.granola.ai",
		DocumentationURL: "https://www.granola.ai/docs/mcp",
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
		Name:             "com.atlassian/mcp",
		Title:            "Atlassian",
		Description:      "Atlassian's hosted MCP server for Jira and Confluence Cloud.",
		WebsiteURL:       "https://www.atlassian.com",
		DocumentationURL: "https://support.atlassian.com/atlassian-rovo-mcp-server/",
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
		Name:             "com.canva/mcp",
		Title:            "Canva",
		Description:      "Canva's hosted MCP server for designs and brand assets.",
		WebsiteURL:       "https://www.canva.com",
		DocumentationURL: "https://www.canva.dev/docs/apps/mcp-server/",
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
		Name:             "com.datadoghq/mcp",
		Title:            "Datadog",
		Description:      "Datadog's hosted MCP server for monitors, logs, and incidents.",
		WebsiteURL:       "https://www.datadoghq.com",
		DocumentationURL: "https://docs.datadoghq.com/bits_ai/mcp_server/",
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
		Name:             "com.github/mcp",
		Title:            "GitHub",
		Description:      "GitHub's hosted MCP server for repositories, issues, and pull requests.",
		WebsiteURL:       "https://github.com",
		DocumentationURL: "https://docs.github.com/en/copilot/how-tos/context/model-context-protocol/using-the-github-mcp-server",
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
		Name:             "com.notion/mcp",
		Title:            "Notion",
		Description:      "Notion's hosted MCP server for pages and databases.",
		WebsiteURL:       "https://www.notion.com",
		DocumentationURL: "https://developers.notion.com/docs/mcp",
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
		Name:             "com.slack/mcp",
		Title:            "Slack",
		Description:      "Slack's hosted MCP server for channels, messages, and search.",
		WebsiteURL:       "https://slack.com",
		DocumentationURL: "https://docs.slack.dev/ai/mcp-server/",
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
		Name:             "com.supabase/mcp",
		Title:            "Supabase",
		Description:      "Supabase's hosted MCP server for projects, databases, and edge functions.",
		WebsiteURL:       "https://supabase.com",
		DocumentationURL: "https://supabase.com/docs/guides/getting-started/mcp",
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
}

// Result counts what Apply did.
type Result struct {
	Created   int
	Updated   int
	Unchanged int
}

// Apply creates missing vendor entries and writes the Okta namespace onto
// existing ones without touching their name, remotes or other metadata. It
// is safe to run repeatedly. With dryRun it reports what it would do and
// writes nothing; the records are still validated.
func Apply(ctx context.Context, logger *slog.Logger, svc *mcpregistry.Service, dryRun bool) (Result, error) {
	var result Result
	for _, v := range Vendors {
		outcome, err := apply(ctx, svc, v, dryRun)
		if err != nil {
			return result, fmt.Errorf("seed %s: %w", v.Name, err)
		}
		logger.InfoContext(ctx, "okta catalog seed applied", attr.SlogRegistryEntryName(v.Name), attr.SlogRegistrySeedOutcome(outcome), attr.SlogRegistrySeedDryRun(dryRun))
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

func apply(ctx context.Context, svc *mcpregistry.Service, v Vendor, dryRun bool) (string, error) {
	existing, err := svc.GetByName(ctx, v.Name)
	switch {
	case errors.Is(err, mcpregistry.ErrNotFound):
		data, err := v.record()
		if err != nil {
			return "", err
		}
		if dryRun {
			if issues := svc.Validate(data); len(issues) > 0 {
				return "", &mcpregistry.InvalidError{Issues: issues}
			}
			return "created", nil
		}
		if _, err := svc.Create(ctx, data); err != nil {
			return "", fmt.Errorf("create: %w", err)
		}
		return "created", nil
	case err != nil:
		return "", fmt.Errorf("lookup: %w", err)
	}
	current, err := mcpregistry.ParseOktaMapping(existing.Data)
	if err == nil && equalMapping(current, v.Mapping) {
		return "unchanged", nil
	}
	data, err := withMapping(existing.Data, v.Mapping)
	if err != nil {
		return "", err
	}
	if dryRun {
		if issues := svc.Validate(data); len(issues) > 0 {
			return "", &mcpregistry.InvalidError{Issues: issues}
		}
		return "updated", nil
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
	meta := map[string]any{mcpregistry.OktaNamespace: v.Mapping}
	if v.DocumentationURL != "" {
		meta["com.speakeasy.ai/catalog"] = map[string]any{"documentationUrl": v.DocumentationURL}
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

func equalMapping(a, b mcpregistry.OktaMapping) bool {
	x, errX := json.Marshal(a)
	y, errY := json.Marshal(b)
	return errX == nil && errY == nil && string(x) == string(y)
}
