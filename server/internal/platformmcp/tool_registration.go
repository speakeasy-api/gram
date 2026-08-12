//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RegisterCatalogMCPToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit Gram project slug that will own the reviewed MCP"`
	ProviderKey    string `json:"provider_key" jsonschema:"reviewed provider key returned by search_mcp_catalog"`
	CatalogRef     string `json:"catalog_ref" jsonschema:"canonical catalog reference returned by search_mcp_catalog"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-generated idempotency key; reuse only to retry the same project and catalog candidate"`
}

type RegisterCatalogMCPToolOutput struct {
	ProjectSlug    string `json:"project_slug"`
	ProviderKey    string `json:"provider_key"`
	CatalogRef     string `json:"catalog_ref"`
	SetupIntent    string `json:"setup_intent"`
	ReceiptID      string `json:"receipt_id"`
	RegistrationID string `json:"registration_id"`
	Replayed       bool   `json:"replayed"`
}

func registerCatalogRegistrationTool(server *mcp.Server, registrations *RegistrationService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "register_catalog_mcp",
		Title:       "Register Catalog MCP",
		Description: "Register one reviewed catalog MCP in an explicit Gram project. Registration creates private project configuration only; it does not distribute the MCP or publish a plugin package.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RegisterCatalogMCPToolInput) (*mcp.CallToolResult, RegisterCatalogMCPToolOutput, error) {
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
		return nil, RegisterCatalogMCPToolOutput{
			ProjectSlug:    result.Project.Slug,
			ProviderKey:    result.ProviderKey,
			CatalogRef:     result.CatalogRef,
			SetupIntent:    result.SetupIntent,
			ReceiptID:      result.Receipt.ID.String(),
			RegistrationID: result.Registration,
			Replayed:       result.Receipt.Replayed,
		}, nil
	})
}
