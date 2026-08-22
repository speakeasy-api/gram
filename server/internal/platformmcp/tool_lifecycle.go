//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetSetupHandoffToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_catalog_mcp or register_remote_mcp_for_project"`
	ProviderKey    string `json:"provider_key" jsonschema:"provider key returned by register_catalog_mcp or register_remote_mcp_for_project"`
	CatalogRef     string `json:"catalog_ref" jsonschema:"catalog reference returned by register_catalog_mcp or register_remote_mcp_for_project"`
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

func registerSetupHandoffTool(reg *Registrar, registrations *RegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "get_setup_handoff",
		Title:       "Get Setup Handoff",
		Description: "Get the secure dashboard continuation for one MCP registration. Browser Catalogue entries and remote URL registrations return the server-owned Authentication settings dashboard URL, where headers and authentication are configured; the local synthetic fixture returns a single-use setup handoff. Never persist, log, or share a handoff.",
	}, ToolMeta{
		// The handoff carries the caller to the dashboard, which completes setup
		// under its own session. A connection-less caller issues a handoff bound
		// to its user rather than to a connection.
		Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetSetupHandoffToolInput) (*mcp.CallToolResult, GetSetupHandoffToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetSetupHandoffToolOutput{}, err
		}
		setupInput := IssueSetupHandoffInput(input)
		if registrationUsesDashboardSetup(input.ProviderKey) {
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
