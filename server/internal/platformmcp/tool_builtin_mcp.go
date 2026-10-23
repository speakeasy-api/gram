//nolint:exhaustruct // MCP manifests rely on documented optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func registerBuiltinMCPTools(reg *Registrar, core BuiltinMCPManagement) {
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}
	addTool(reg, &mcp.Tool{
		Name: "list_builtin_mcps", Title: "List Built-in MCP Servers",
		Description: "List the immutable built-in catalog for one explicit project, including visually hidden entries for administrator settings. Built-ins such as Slack are separate from find_mcp/get_mcp. Returns stable endpoint, immutable tool schemas, credential variable names (never values), and settings versions. All human identities, including admins, are forbidden from connecting to built-in runtime endpoints. This tool does not grant machine access.",
		Annotations: readOnlyAnnotations(),
	}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input ListBuiltinMCPsInput) (*mcp.CallToolResult, ListBuiltinMCPsOutput, error) {
		return principalToolCall(ctx, builtinMCPToolResult, func(principal Principal) (ListBuiltinMCPsOutput, error) {
			if core == nil {
				return ListBuiltinMCPsOutput{}, ErrUnavailable
			}
			project, err := builtinMCPProject(principal, input.ProjectID)
			if err != nil {
				return ListBuiltinMCPsOutput{}, err
			}
			servers, err := core.List(ctx, principal.OrganizationID, project)
			if err != nil {
				return ListBuiltinMCPsOutput{}, fmt.Errorf("list built-in MCPs: %w", err)
			}
			return ListBuiltinMCPsOutput{Servers: servers}, nil
		})
	})
	addTool(reg, &mcp.Tool{
		Name: "get_builtin_mcp", Title: "Get Built-in MCP Server",
		Description: "Read an exact immutable built-in key in an explicit project, including its settings version, stable endpoint and immutable tools. Administrator-only; includes visually hidden entries but never credential values. Read this before configure_builtin_mcp. Human identities cannot connect to the runtime endpoint; settings do not grant access.",
		Annotations: readOnlyAnnotations(),
	}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input GetBuiltinMCPInput) (*mcp.CallToolResult, builtinmcp.Server, error) {
		return principalToolCall(ctx, builtinMCPToolResult, func(principal Principal) (builtinmcp.Server, error) {
			if core == nil {
				return builtinmcp.Server{}, ErrUnavailable
			}
			project, err := builtinMCPProject(principal, input.ProjectID)
			if err != nil {
				return builtinmcp.Server{}, err
			}
			if input.Key == "" {
				return builtinmcp.Server{}, errBuiltinMCPInvalid
			}
			result, err := core.Get(ctx, principal.OrganizationID, project, input.Key)
			if err != nil {
				return builtinmcp.Server{}, fmt.Errorf("get built-in MCP: %w", err)
			}
			return result, nil
		})
	})
	addTool(reg, &mcp.Tool{
		Name: "configure_builtin_mcp", Title: "Configure Built-in MCP Server",
		Description: "Set visual hidden state and the default environment override for one exact built-in in an explicit project. First read get_builtin_mcp, show the exact proposed settings, obtain explicit user confirmation, then supply confirmed:true and expected_version. Omitted environment_id clears the override. The shared core checks live human admin membership, project ownership and version, atomically audits the change and returns committed settings. Retry a stale version only after reading and confirming again. Hidden is visual only, not runtime authorization. Cannot edit tools, endpoint, credentials or machine grants, and does not enable human runtime access.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), OpenWorldHint: new(false)},
	}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input ConfigureBuiltinMCPInput) (*mcp.CallToolResult, builtinmcp.Server, error) {
		return principalToolCall(ctx, builtinMCPToolResult, func(principal Principal) (builtinmcp.Server, error) {
			return configureBuiltinMCP(ctx, core, principal, input)
		})
	})
}
func builtinMCPToolResult(err error) (*mcp.CallToolResult, bool) {
	result := featureUnavailableResult{Feature: "builtin_mcps"}
	switch {
	case errors.Is(err, ErrUnavailable):
		result.Code, result.Message = unavailableCode, "Built-in MCP management is unavailable on this server."
	case errors.Is(err, errBuiltinMCPInvalid):
		result.Code, result.Message = "invalid_request", "Provide an explicit project UUID and exact built-in key. For changes, read current settings and supply their version with explicit confirmation; environment_id must be a project environment UUID or omitted."
	default:
		shared, ok := errors.AsType[*oops.ShareableError](err)
		if !ok {
			return nil, false
		}
		switch shared.Code {
		case oops.CodeForbidden:
			result.Code, result.Message = "permission_denied", "Built-in MCP management requires a live human organization administrator."
		case oops.CodeNotFound:
			result.Code, result.Message = "not_found", "That built-in is not available in the selected project."
		case oops.CodeConflict:
			result.Code, result.Message = "conflict", "The settings changed. Read get_builtin_mcp again and confirm the new settings version before retrying."
		case oops.CodeInvalid:
			result.Code, result.Message = "invalid_request", "The settings are invalid. Use a current version and an environment belonging to the selected project."
		default:
			return nil, false
		}
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}
