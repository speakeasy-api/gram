//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// UpdateMCPMetadataToolInput is a patch: every optional field that is omitted
// is left unchanged, and an empty string clears an install-page field.
type UpdateMCPMetadataToolInput struct {
	ProjectSlug               string  `json:"project_slug" jsonschema:"explicit project slug that owns the MCP server"`
	RegistrationID            string  `json:"registration_id,omitempty" jsonschema:"Platform registration ID returned by find_mcp or get_mcp; required to rename, and not used otherwise"`
	MCPID                     string  `json:"mcp_id" jsonschema:"configured MCP ID returned by find_mcp or get_mcp"`
	Name                      *string `json:"name,omitempty" jsonschema:"new project-local MCP display name; 1-256 bytes after trimming and no line breaks; omit to keep the current name; renaming needs registration_id and cannot clear the name"`
	LogoAssetID               *string `json:"logo_asset_id,omitempty" jsonschema:"asset ID of an image uploaded to this organization, shown as the install-page logo; an empty string removes the logo; omit to keep it"`
	ExternalDocumentationURL  *string `json:"external_documentation_url,omitempty" jsonschema:"absolute HTTPS link to the server's documentation, up to 2048 bytes; an empty string removes it; omit to keep it"`
	ExternalDocumentationText *string `json:"external_documentation_text,omitempty" jsonschema:"label for the documentation link, up to 256 bytes and no line breaks; an empty string removes it; omit to keep it"`
	Instructions              *string `json:"instructions,omitempty" jsonschema:"instructions for using the server, up to 16384 bytes; an empty string removes them; omit to keep them"`
	ExpectedVersion           string  `json:"expected_version" jsonschema:"opaque version returned by find_mcp, get_mcp, or a previous update_mcp_metadata result"`
	IdempotencyKey            string  `json:"idempotency_key" jsonschema:"caller-generated idempotency key; reuse only to retry this exact update"`
}

type UpdateMCPMetadataToolOutput struct {
	ProjectSlug    string         `json:"project_slug"`
	RegistrationID string         `json:"registration_id,omitempty"`
	MCPID          string         `json:"mcp_id"`
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Visibility     string         `json:"visibility"`
	InstallPage    MCPInstallPage `json:"install_page"`
	Version        string         `json:"version"`
	ReceiptID      string         `json:"receipt_id"`
	Replayed       bool           `json:"replayed"`
}

const (
	updateMCPMetadataTitle       = "Update MCP Server Details"
	updateMCPMetadataDescription = "Change one MCP server's details in a named project: its name, and the logo, documentation link, and instructions on its install page. " +
		"Only what you send changes: an omitted field stays as it is, and an empty string clears the logo, documentation link, documentation label, or instructions. Send at least one field. " +
		"The logo, documentation link, and instructions can be changed on any MCP server in the project. Renaming works only for a fully set-up MCP server that was added through this platform, and needs its registration_id; the name can never be cleared. " +
		"The logo must be an image uploaded to this organization. Instructions are shown on the install page, and a server hosted from this project's own tools also sends them to apps when they connect. " +
		"Nothing else changes: not its address or provider configuration, not whether it is working or turned on, not which plugins carry it, and not who receives it."
)

func registerLifecycleMetadataTool(reg *Registrar, registrations *RegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "update_mcp_metadata",
		Title:       updateMCPMetadataTitle,
		Description: updateMCPMetadataDescription,
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateMCPMetadataToolInput) (*mcp.CallToolResult, UpdateMCPMetadataToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, UpdateMCPMetadataToolOutput{}, err
		}
		result, err := registrations.UpdateMCPMetadata(ctx, principal, UpdateMCPMetadataInput(input))
		if err != nil {
			if toolResult, ok := operationBudgetToolResult(err); ok {
				return toolResult, UpdateMCPMetadataToolOutput{}, nil
			}
			return nil, UpdateMCPMetadataToolOutput{}, err
		}
		return nil, UpdateMCPMetadataToolOutput{
			ProjectSlug: result.Project.Slug, RegistrationID: result.RegistrationID, MCPID: result.MCPID,
			Name: result.Name, Slug: result.Slug, Visibility: result.Visibility,
			InstallPage: *mcpInstallPageFromLifecycle(result.InstallPage), Version: result.Version,
			ReceiptID: result.Receipt.ID.String(), Replayed: result.Receipt.Replayed,
		}, nil
	})
}
