//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RegisterCatalogMCPToolInput struct {
	ProjectSlug     string                     `json:"project_slug" jsonschema:"explicit AICP project slug that will own the reviewed MCP"`
	ProviderKey     string                     `json:"provider_key" jsonschema:"server-issued catalogue source identity returned by search_mcp_catalog"`
	CatalogRef      string                     `json:"catalog_ref" jsonschema:"exact catalogue reference returned by search_mcp_catalog"`
	NonSecretConfig CatalogConfigurationValues `json:"non_secret_config,omitempty" jsonschema:"only declared non-secret configuration values keyed by inspect_mcp_catalog_candidate configuration field key; do not include API keys, tokens, passwords, OAuth codes, client secrets, or secret headers"`
	IdempotencyKey  string                     `json:"idempotency_key" jsonschema:"caller-generated idempotency key; reuse only to retry the same project, catalogue candidate, and non-secret configuration"`
}

type RegisterCatalogMCPToolOutput struct {
	ProjectSlug         string                      `json:"project_slug"`
	ProviderKey         string                      `json:"provider_key"`
	CatalogRef          string                      `json:"catalog_ref"`
	SetupIntent         string                      `json:"setup_intent"`
	ReceiptID           string                      `json:"receipt_id"`
	RegistrationID      string                      `json:"registration_id"`
	Replayed            bool                        `json:"replayed"`
	NextAction          string                      `json:"next_action"`
	DashboardSetupURL   string                      `json:"dashboard_setup_url,omitempty"`
	SecretFieldsPending []CatalogConfigurationField `json:"secret_fields_pending,omitempty"`
}

type RegisterRemoteMCPToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit AICP project slug that will own the user-supplied MCP"`
	RemoteURL      string `json:"remote_url" jsonschema:"HTTPS Streamable HTTP MCP URL; query parameters, fragments, userinfo, headers, and credentials are not accepted"`
	DisplayName    string `json:"display_name,omitempty" jsonschema:"optional project-local display name for the MCP"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-generated idempotency key; reuse only to retry the same project and canonical remote URL"`
}

type RegisterRemoteMCPToolOutput struct {
	ProjectSlug       string `json:"project_slug"`
	CanonicalURL      string `json:"canonical_url"`
	ReceiptID         string `json:"receipt_id"`
	RegistrationID    string `json:"registration_id"`
	Replayed          bool   `json:"replayed"`
	NextAction        string `json:"next_action"`
	DashboardSetupURL string `json:"dashboard_setup_url,omitempty"`
}

func registerCatalogRegistrationTool(reg *Registrar, registrations *RegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "register_catalog_mcp",
		Title:       "Register Catalog MCP",
		Description: "Register one reviewed catalog MCP in an explicit AICP project. Registration creates private project configuration only; it does not distribute the MCP or publish a plugin package.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input RegisterCatalogMCPToolInput) (*mcp.CallToolResult, RegisterCatalogMCPToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, RegisterCatalogMCPToolOutput{}, err
		}
		result, err := registrations.RegisterCatalogMCP(ctx, principal, RegisterCatalogMCPInput(input))
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, RegisterCatalogMCPToolOutput{}, nil
			}
			return nil, RegisterCatalogMCPToolOutput{}, err
		}
		nextAction := "start_setup"
		if isBrowserCatalogProviderKey(result.ProviderKey) {
			nextAction = "continue_dashboard_setup"
		}
		if len(result.SecretFieldsPending) > 0 {
			nextAction = "secure_dashboard_setup_required"
		}
		dashboardSetupURL := ""
		if isBrowserCatalogProviderKey(result.ProviderKey) {
			dashboardSetupURL, err = registrations.DashboardSetupURL(ctx, principal, IssueSetupHandoffInput{
				ProjectSlug:    result.Project.Slug,
				RegistrationID: result.Registration,
				ProviderKey:    result.ProviderKey,
				CatalogRef:     result.CatalogRef,
			})
			if err != nil {
				return nil, RegisterCatalogMCPToolOutput{}, err
			}
		}
		return nil, RegisterCatalogMCPToolOutput{
			ProjectSlug:         result.Project.Slug,
			ProviderKey:         result.ProviderKey,
			CatalogRef:          result.CatalogRef,
			SetupIntent:         result.SetupIntent,
			ReceiptID:           result.Receipt.ID.String(),
			RegistrationID:      result.Registration,
			Replayed:            result.Receipt.Replayed,
			NextAction:          nextAction,
			DashboardSetupURL:   dashboardSetupURL,
			SecretFieldsPending: append([]CatalogConfigurationField(nil), result.SecretFieldsPending...),
		}, nil
	})
}

func registerRemoteRegistrationTool(reg *Registrar, registrations *RegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "register_remote_mcp",
		Title:       "Register Remote MCP",
		Description: "Register one user-supplied Streamable HTTP MCP URL in an explicit AICP project. The URL is revalidated and re-inspected before private registration. Registration does not distribute the MCP or publish a plugin package.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input RegisterRemoteMCPToolInput) (*mcp.CallToolResult, RegisterRemoteMCPToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, RegisterRemoteMCPToolOutput{}, err
		}
		result, err := registrations.RegisterRemoteMCP(ctx, principal, RegisterRemoteMCPInput(input))
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, RegisterRemoteMCPToolOutput{}, nil
			}
			return nil, RegisterRemoteMCPToolOutput{}, err
		}
		return nil, RegisterRemoteMCPToolOutput{
			ProjectSlug:       result.Project.Slug,
			CanonicalURL:      result.RemoteURL,
			ReceiptID:         result.Receipt.ID.String(),
			RegistrationID:    result.Registration,
			Replayed:          result.Receipt.Replayed,
			NextAction:        result.NextAction,
			DashboardSetupURL: result.DashboardSetupURL,
		}, nil
	})
}
