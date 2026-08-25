//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

type GetMCPClientAdmissionToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_catalog_mcp or register_remote_mcp"`
}

type SetMCPClientAdmissionToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_catalog_mcp or register_remote_mcp"`
	Mode           string `json:"mode" jsonschema:"enum=presets,enum=open,enum=disabled,admission policy for MCP clients that identify themselves with a client ID metadata document"`
	Confirmed      bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms this exact admission mode for this exact MCP; the change takes effect for every MCP client of the server"`
}

type MCPClientAdmissionToolOutput struct {
	ProjectSlug      string   `json:"project_slug"`
	RegistrationID   string   `json:"registration_id"`
	Mode             string   `json:"mode"`
	AllowedModes     []string `json:"allowed_modes"`
	CustomClientURLs []string `json:"custom_client_urls"`
	Message          string   `json:"message"`
}

type clientAdmissionErrorResult struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// modeGuidance explains the consequence of each mode in the tool result itself,
// so an agent presenting a choice does not have to guess what it is proposing.
func modeGuidance(mode string) string {
	switch admission.Mode(mode) {
	case admission.ModePresets:
		return "Known clients only: the reviewed preset catalogue plus this MCP's own custom client ID metadata URLs are admitted. Any other client is refused at authorization and cannot fall back."
	case admission.ModeOpen:
		return "Open: every spec-valid client ID metadata document is admitted. Document validation still applies."
	case admission.ModeDisabled:
		return "Disabled: no client ID metadata document is admitted, and the server stops advertising support for them, so clients use dynamic client registration instead."
	case admission.ModeReporting:
		// Not writable, but an unconfigured issuer resolves to it, so a read
		// can legitimately return it.
		return "Not enforced yet: every client is admitted while the platform records what a Known clients policy would have refused."
	default:
		return ""
	}
}

func registerUnavailableClientAdmissionTools(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "get_mcp_client_admission",
		Title:       "Get MCP Client Admission",
		Description: "Read a reviewed MCP's client admission policy. Client admission is not available in the current preview.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("client_admission"))
	addTool(reg, &mcp.Tool{
		Name:        "set_mcp_client_admission",
		Title:       "Set MCP Client Admission",
		Description: "Set a reviewed MCP's client admission policy. Client admission is not available in the current preview.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("client_admission"))
}

// registerClientAdmissionTools serves the CIMD half of MCP Server -> Settings
// -> Authentication. Both tools are connection-less: the registration is
// resolved by user and project, so the managed project assistant reaches the
// same state an external client does.
func registerClientAdmissionTools(reg *Registrar, registrations *RegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "get_mcp_client_admission",
		Title:       "Get MCP Client Admission",
		Description: "Return which MCP clients may authorize against one registered MCP: the effective client ID metadata document admission mode, the modes that can be set, and this MCP's own custom client ID metadata URLs. Read this before proposing a change.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPClientAdmissionToolInput) (*mcp.CallToolResult, MCPClientAdmissionToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, MCPClientAdmissionToolOutput{}, err
		}
		result, err := registrations.GetClientAdmission(ctx, principal, input.ProjectSlug, input.RegistrationID)
		if err != nil {
			if toolResult, ok := clientAdmissionToolError(err); ok {
				return toolResult, MCPClientAdmissionToolOutput{}, nil
			}
			return nil, MCPClientAdmissionToolOutput{}, err
		}
		return nil, clientAdmissionToolOutput(input.ProjectSlug, input.RegistrationID, result, modeGuidance(result.Mode)), nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "set_mcp_client_admission",
		Title:       "Set MCP Client Admission",
		Description: "Set which MCP clients may authorize against one registered MCP. Present the current mode from get_mcp_client_admission and the consequence of the proposed one, ask for explicit user confirmation, then call this with confirmed: true. Known clients (presets) refuses unlisted clients at authorization with no fallback, so confirm the user accepts that. This tool never accepts or returns credentials, OAuth codes, tokens, or client secrets.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input SetMCPClientAdmissionToolInput) (*mcp.CallToolResult, MCPClientAdmissionToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, MCPClientAdmissionToolOutput{}, err
		}
		if !admission.IsValidMode(input.Mode) {
			return clientAdmissionErrorTool("invalid_request", "mode must be one of presets, open, or disabled. Read get_mcp_client_admission for the current mode and do not retry unchanged input."), MCPClientAdmissionToolOutput{}, nil
		}
		if !input.Confirmed {
			return clientAdmissionErrorTool("confirmation_required", "Tell the user which clients "+input.Mode+" admits and which it refuses, ask them to explicitly confirm that mode for this MCP, then call this tool again with confirmed: true."), MCPClientAdmissionToolOutput{}, nil
		}
		result, err := registrations.SetClientAdmission(ctx, principal, input.ProjectSlug, input.RegistrationID, input.Mode)
		if err != nil {
			if toolResult, ok := clientAdmissionToolError(err); ok {
				return toolResult, MCPClientAdmissionToolOutput{}, nil
			}
			return nil, MCPClientAdmissionToolOutput{}, err
		}
		return nil, clientAdmissionToolOutput(input.ProjectSlug, input.RegistrationID, result, "Client admission is now "+result.Mode+". "+modeGuidance(result.Mode)), nil
	})
}

func clientAdmissionToolOutput(projectSlug, registrationID string, result ClientAdmission, message string) MCPClientAdmissionToolOutput {
	return MCPClientAdmissionToolOutput{
		ProjectSlug:      projectSlug,
		RegistrationID:   registrationID,
		Mode:             result.Mode,
		AllowedModes:     result.AllowedModes,
		CustomClientURLs: result.CustomClientURLs,
		Message:          message,
	}
}

func clientAdmissionToolError(err error) (*mcp.CallToolResult, bool) {
	switch {
	case errors.Is(err, ErrClientAdmissionInvalid):
		return clientAdmissionErrorTool("invalid_request", "This registration is not a complete Platform-managed MCP with its own authentication settings, or the request no longer matches its persisted state. Re-read find_mcp or get_mcp and do not retry unchanged input."), true
	case errors.Is(err, ErrClientAdmissionUnavailable):
		return clientAdmissionErrorTool(unavailableCode, "Client admission settings are temporarily unavailable. No admission mode was changed. Retry later or use the MCP server's Authentication settings page."), true
	default:
		return operationBudgetToolResult(err)
	}
}

func clientAdmissionErrorTool(code, message string) *mcp.CallToolResult {
	content, err := json.Marshal(clientAdmissionErrorResult{Code: code, Message: message})
	if err != nil {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: message}}, IsError: true}
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}
}
