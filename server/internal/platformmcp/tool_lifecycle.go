//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetSetupHandoffToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit Gram project slug that owns the reviewed MCP registration"`
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
	Handoff        string `json:"handoff"`
	ExpiresAt      string `json:"expires_at"`
}

func registerSetupHandoffTool(server *mcp.Server, registrations *RegistrationService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_setup_handoff",
		Title:       "Get Setup Handoff",
		Description: "Create a single-use setup handoff for one reviewed MCP registration. Return the handoff only to the requesting user and never persist, log, or share it.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetSetupHandoffToolInput) (*mcp.CallToolResult, GetSetupHandoffToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetSetupHandoffToolOutput{}, err
		}
		issued, err := registrations.IssueSetupHandoff(ctx, principal, IssueSetupHandoffInput(input))
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
