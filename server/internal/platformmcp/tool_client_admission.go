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
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_catalog_mcp or register_remote_mcp"`
}

type SetMCPClientAdmissionToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_catalog_mcp or register_remote_mcp"`
	Mode           string `json:"mode" jsonschema:"which MCP clients that identify themselves with a client ID metadata document may sign in; one of presets, open, or disabled"`
	Confirmed      bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms this exact setting for this exact MCP server; it takes effect for every app that connects to it"`
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
		return "Known apps only: the reviewed list of apps, plus any custom client ID metadata URLs you add for this MCP server. Every other app is turned away when it tries to sign in, with no way through."
	case admission.ModeOpen:
		return "Open: any app with a valid client ID metadata document can sign in. The document is still checked."
	case admission.ModeDisabled:
		return "Off: client ID metadata documents are not accepted at all, and the server stops advertising them, so apps use dynamic client registration instead."
	case admission.ModeReporting:
		// Not writable, and nothing resolves to it any more, but a row
		// written while it was the default still reads back as it.
		return "Watching only: every app can sign in, while we record which ones \"known apps only\" would have turned away."
	default:
		return ""
	}
}

func registerUnavailableClientAdmissionTools(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "get_mcp_client_admission",
		Title:       "Which Apps Can Sign In",
		Description: "Show which apps can sign in to an MCP server. This is not switched on for your organization yet.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("client_admission"))
	addTool(reg, &mcp.Tool{
		Name:        "set_mcp_client_admission",
		Title:       "Choose Which Apps Can Sign In",
		Description: "Choose which apps can sign in to an MCP server. This is not switched on for your organization yet.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("client_admission"))
}

// registerClientAdmissionTools serves the CIMD half of MCP Server -> Settings
// -> Authentication. Both tools are connection-less: the registration is
// resolved by user and project, so the managed project assistant reaches the
// same state an external client does.
func registerClientAdmissionTools(reg *Registrar, registrations *RegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "get_mcp_client_admission",
		Title:       "Which Apps Can Sign In",
		Description: "Show which MCP clients are allowed to sign in to one MCP server: the setting in force now, the settings you can choose, and any custom client ID metadata URLs this MCP server allows on top. Read this before proposing a change.",
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
		Title:       "Choose Which Apps Can Sign In",
		Description: "Choose which MCP clients may sign in to one MCP server. Show the user the setting in force now, from get_mcp_client_admission, and what the new one would mean, ask them to confirm out loud, then call this with confirmed: true. Constraints: \"known apps only\" turns away every app that is not on the list, with no way through — make sure the user accepts that. This tool never accepts or returns credentials, OAuth codes, tokens, or client secrets.",
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
		return clientAdmissionErrorTool("invalid_request", "This MCP server is not fully set up with its own sign-in settings, or something about it changed since you last looked. Read it again with find_mcp or get_mcp; do not retry the same input."), true
	case errors.Is(err, ErrClientAdmissionUnavailable):
		return clientAdmissionErrorTool(unavailableCode, "This setting is temporarily unavailable, and nothing was changed. Try again later, or use the MCP server's Authentication settings page."), true
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
