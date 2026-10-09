//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	listProjectToolsToolName    = "list_project_tools"
	addToolsToMCPToolName       = "add_tools_to_mcp"
	removeToolsFromMCPToolName  = "remove_tools_from_mcp"
	toolExposureBlastRadiusNote = "Changing this list requests publication for affected plugins. If the edited toolset contains any platform tool, proven automatic role-plugin memberships are removed; explicit manual memberships and MCP access are preserved. Removing the last platform tool restores automatic-distribution eligibility while preserving prior removal history. The result names current distributions and plugins removed by this edit. Supported clients receive updates on their normal refresh cycle; locally installed ZIPs require replacement."
)

// registerToolExposureTools keeps the live and unavailable manifests identical
// so a tool never appears on and disappears from the catalogue as a deployment
// composes or fails to compose the service behind it.
func registerToolExposureTools(reg *Registrar, service *MCPToolExposureService, reader Reader) {
	listTools := unavailableToolExposureHandler[ListProjectToolsInput, ListProjectToolsOutput]("Listing a project's tools is not available on this server.")
	addTools := unavailableToolExposureHandler[ChangeMCPToolsInput, MCPToolExposureMutationOutput](unavailableToolExposureChangeMessage)
	removeTools := unavailableToolExposureHandler[ChangeMCPToolsInput, MCPToolExposureMutationOutput](unavailableToolExposureChangeMessage)
	projects, _ := reader.(ProjectReadResolver)
	if service.valid() && projects != nil {
		listTools = func(ctx context.Context, _ *mcp.CallToolRequest, input ListProjectToolsInput) (*mcp.CallToolResult, ListProjectToolsOutput, error) {
			return principalToolCall(ctx, toolExposureToolResult, func(principal Principal) (ListProjectToolsOutput, error) {
				project, err := projects.ResolveProjectRead(ctx, principal, FindMCPInput{ProjectID: input.ProjectID})
				if err != nil {
					return ListProjectToolsOutput{}, fmt.Errorf("resolve project for its tool list: %w", err)
				}
				return service.ListProjectTools(ctx, principal, project, input)
			})
		}
	}
	if service.valid() {
		addTools = func(ctx context.Context, _ *mcp.CallToolRequest, input ChangeMCPToolsInput) (*mcp.CallToolResult, MCPToolExposureMutationOutput, error) {
			return principalToolCall(ctx, toolExposureToolResult, func(principal Principal) (MCPToolExposureMutationOutput, error) {
				return service.AddTools(ctx, principal, input)
			})
		}
		removeTools = func(ctx context.Context, _ *mcp.CallToolRequest, input ChangeMCPToolsInput) (*mcp.CallToolResult, MCPToolExposureMutationOutput, error) {
			return principalToolCall(ctx, toolExposureToolResult, func(principal Principal) (MCPToolExposureMutationOutput, error) {
				return service.RemoveTools(ctx, principal, input)
			})
		}
	}

	addTool(reg, &mcp.Tool{
		Name:  listProjectToolsToolName,
		Title: "List a Project's Tools",
		Description: "List the tools a project produces, with the function or API document each one came from. Use this to find a tool that was just pushed before putting it on an MCP server. " +
			"A tool appears here once the deployment that generated it has finished; it is not on any MCP server until it is added to one.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{
		Authorization: ExternalAuthorizationMember, Audiences: bothAudiences,
		ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead,
	}, listTools)

	// Mutations stay off the managed assistant for the same reason the other
	// distribution-affecting writes do: the change reaches every person
	// holding an affected plugin the moment it commits, so it belongs on the
	// surface where an administrator drives it directly.
	meta := ToolMeta{
		Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly,
		ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryMCPWrite,
	}
	addTool(reg, &mcp.Tool{
		Name:  addToolsToMCPToolName,
		Title: "Put Tools on an MCP Server",
		Description: "Add named tools to one exact MCP server in an explicit project, without disturbing the tools it already exposes. " +
			"Supply exact tool URNs from list_project_tools, the exposure_version from the latest get_mcp read of that server, an idempotency key, and confirmed: true only after the user confirms the exact server and tool list. " +
			toolExposureBlastRadiusNote + " " +
			"A tool that is neither produced by this project nor already on this server is refused by name and nothing is changed. A tool the server already exposes is reported as unchanged rather than added. Only a server whose tools come from this project can be changed here, and when another server offers the same tool list the change moves both, so it is allowed only with permission to change every one of them.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, meta, addTools)
	addTool(reg, &mcp.Tool{
		Name:  removeToolsFromMCPToolName,
		Title: "Take Tools off an MCP Server",
		Description: "Remove named tools from one exact MCP server in an explicit project, leaving the rest of its tools in place. " +
			"Supply exact tool URNs, the exposure_version from the latest get_mcp read of that server, an idempotency key, and confirmed: true only after the user confirms the exact server and tool list. " +
			toolExposureBlastRadiusNote + " " +
			"People using the server lose those tools as soon as the change reaches them. A tool that is neither produced by this project nor already on this server is refused by name and nothing is changed; a tool this project produces but the server does not expose is reported as unchanged rather than removed.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true)},
	}, meta, removeTools)
}

// ProjectReadResolver is the project-read boundary the tool listing sits
// behind. It is declared here rather than taken as a concrete reader so the
// listing composes from whatever reader a deployment supplies.
type ProjectReadResolver interface {
	ResolveProjectRead(ctx context.Context, principal Principal, input FindMCPInput) (ResolvedProject, error)
}

const unavailableToolExposureChangeMessage = "Changing which tools an MCP server exposes is not available on this server."

// unavailableToolExposureHandler takes its own message so a read's refusal
// says the read is unavailable. Reusing the mutation's wording would tell a
// caller that asked only to list a project's tools that it cannot change a
// server, which is a different, more alarming claim.
func unavailableToolExposureHandler[In, Out any](message string) mcp.ToolHandlerFor[In, Out] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: toolExposureFeature, Message: message})
		if err != nil {
			return nil, zero, fmt.Errorf("encode unavailable MCP tool exposure result: %w", err)
		}
		return nil, zero, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
}

type toolExposureRefusal struct {
	Code    string `json:"code"`
	Feature string `json:"feature"`
	Message string `json:"message"`
	// UnknownTools names the tools that caused the refusal, so the caller can
	// say which ones to correct instead of retrying the whole batch blind.
	UnknownTools []string `json:"unknown_tools,omitempty"`
	// Preview carries the slugs an unconfirmed server creation would create.
	Preview *MCPFromFunctionsPreview `json:"preview,omitempty"`
}

func toolExposureToolResult(err error) (*mcp.CallToolResult, bool) {
	if refusal, ok := externalAuthorizationToolResult(err); ok {
		return refusal, true
	}
	result := toolExposureRefusal{Feature: toolExposureFeature}
	var exposure *MCPToolExposureError
	switch {
	case errors.As(err, &exposure):
		result.Code, result.Message, result.UnknownTools, result.Preview = exposure.Code, exposure.Message, exposure.UnknownTools, exposure.Preview
	case errors.Is(err, ErrForbidden):
		result.Code = "forbidden"
		result.Message = "That project or MCP server is not one this caller can read."
	case errors.Is(err, ErrUnavailable):
		result.Code = unavailableCode
		result.Message = "Reading or changing a server's tools is temporarily unavailable."
	default:
		return nil, false
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}
