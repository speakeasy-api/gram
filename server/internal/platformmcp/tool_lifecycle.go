//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetSetupHandoffToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit project slug that owns the reviewed MCP registration"`
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

func registerSetupHandoffTool(reg *Registrar, registrations *RegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "get_setup_handoff",
		Title:       "Open Setup in the Dashboard",
		Description: "Open this exact MCP server's Settings > Identity in the authenticated dashboard. Catalogue entries and user-supplied remote MCP servers return a settings URL ending in #authentication; the local test fixture instead returns a POST endpoint in setup_url and a separate single-use handoff token. For that fixture, an authenticated dashboard client must POST JSON containing the handoff field to setup_url using the user's dashboard session, then navigate to the returned authorization_url; do not present the POST endpoint as a browser link. For Slack, select User Identity to reuse a compatible stored client, configure an existing eligible app, or generate a new internal app's configuration. Enter credentials only in the dashboard. Save configures identity, not availability or connectivity; use Availability only if disabled, then Inspect / Connect for personal consent. For catalogue and user-supplied servers, present the exact returned settings URL to the user. Never persist or log handoff tokens or authorization URLs, or share them with another user.",
	}, ToolMeta{
		Authorization: ExternalAuthorizationOrgAdmin,
		// The handoff carries the caller to the dashboard, which completes setup
		// under its own session. A connection-less caller issues a handoff bound
		// to its user rather than to a connection.
		Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetSetupHandoffToolInput) (*mcp.CallToolResult, GetSetupHandoffToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetSetupHandoffToolOutput{}, err
		}
		setupInput := IssueSetupHandoffInput(input)
		if isBrowserCatalogProviderKey(input.ProviderKey) || input.ProviderKey == directRemoteProviderKey {
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
