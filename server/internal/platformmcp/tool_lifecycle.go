//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetSetupHandoffToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_catalog_mcp"`
	ProviderKey    string `json:"provider_key" jsonschema:"reviewed provider key returned by register_catalog_mcp"`
	CatalogRef     string `json:"catalog_ref" jsonschema:"reviewed catalog reference returned by register_catalog_mcp"`
}

type GetSetupHandoffToolOutput struct {
	ProjectID      string `json:"project_id"`
	RegistrationID string `json:"registration_id"`
	ProviderKey    string `json:"provider_key"`
	CatalogRef     string `json:"catalog_ref"`
	SetupURL       string `json:"setup_url"`
	Intent         string `json:"intent"`
	Handoff        string `json:"handoff,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
}

func registerSetupHandoffTool(server *mcp.Server, registrations *RegistrationService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_setup_handoff",
		Title:       "Get Setup Handoff",
		Description: "Get the secure dashboard continuation for one reviewed MCP registration. Browser Catalogue entries return a server-owned dashboard Inspect URL, which contains the available setup and authorization actions; the local synthetic fixture returns a single-use setup handoff. Never persist, log, or share a handoff.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetSetupHandoffToolInput) (*mcp.CallToolResult, GetSetupHandoffToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetSetupHandoffToolOutput{}, err
		}
		setupInput := IssueSetupHandoffInput(input)
		if isBrowserCatalogProviderKey(input.ProviderKey) {
			if err := registrations.budgets.Handoff.Allow(ctx, principal); err != nil {
				if budgetResult, ok := operationBudgetToolResult(err); ok {
					return budgetResult, GetSetupHandoffToolOutput{}, nil
				}
				return nil, GetSetupHandoffToolOutput{}, err
			}
			setupURL, err := registrations.DashboardSetupURL(ctx, principal, setupInput)
			if err != nil {
				return nil, GetSetupHandoffToolOutput{}, err
			}
			return nil, GetSetupHandoffToolOutput{
				RegistrationID: input.RegistrationID,
				ProviderKey:    input.ProviderKey,
				CatalogRef:     input.CatalogRef,
				SetupURL:       setupURL,
				Intent:         "dashboard_source_settings",
			}, nil
		}
		issued, err := registrations.IssueSetupHandoff(ctx, principal, setupInput)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, GetSetupHandoffToolOutput{}, nil
			}
			return nil, GetSetupHandoffToolOutput{}, err
		}
		return nil, GetSetupHandoffToolOutput{
			ProjectID:      issued.ProjectID.String(),
			RegistrationID: issued.RegistrationID.String(),
			ProviderKey:    issued.ProviderKey,
			CatalogRef:     issued.CatalogReference,
			SetupURL:       providerSetupStartPath,
			Intent:         issued.Intent,
			Handoff:        issued.Value,
			ExpiresAt:      issued.ExpiresAt.UTC().Format(time.RFC3339),
		}, nil
	})
}
