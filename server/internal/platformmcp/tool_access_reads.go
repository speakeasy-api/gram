//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type accessReadRefusalResult struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func registerAccessReadTools(reg *Registrar, accessReads *AccessReadService) {
	addTool(reg, &mcp.Tool{
		Name:        "list_access_roles",
		Title:       "List MCP Access Roles",
		Description: "List the organization's roles and summarize the MCP access each role carries. Member counts are withheld for small groups, and each role is represented by a short-lived opaque reference rather than a role ID or principal.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListAccessRolesOutput, error) {
		return principalToolCall(ctx, accessReadToolResult, func(principal Principal) (ListAccessRolesOutput, error) {
			return accessReads.ListRoles(ctx, principal)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_access_members",
		Title:       "Find Organization Members for MCP Access",
		Description: "Find organization members by an explicit identity query of at least three characters or a role reference. Returns masked identities, role names, and short-lived opaque member references only when at least five people match; smaller result sets are withheld rather than enumerated.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListAccessMembersInput) (*mcp.CallToolResult, ListAccessMembersOutput, error) {
		return principalToolCall(ctx, accessReadToolResult, func(principal Principal) (ListAccessMembersOutput, error) {
			return accessReads.ListMembers(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_mcp_access",
		Title:       "Inspect Access to One MCP Server",
		Description: "Inspect which roles can enter one exact configured MCP server through its configured endpoint and which known tools or behavior classes they can use. Uses the same authorization resource and selector semantics as that endpoint; dynamic servers may not have an enumerable tool catalog.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPAccessInput) (*mcp.CallToolResult, GetMCPAccessOutput, error) {
		return principalToolCall(ctx, accessReadToolResult, func(principal Principal) (GetMCPAccessOutput, error) {
			return accessReads.GetMCPAccess(ctx, principal, input)
		})
	})
}

func registerUnavailableAccessReadTools(reg *Registrar) {
	for _, tool := range []struct {
		name         string
		title        string
		description  string
		projectScope ProjectScope
	}{
		{"list_access_roles", "List MCP Access Roles", "List MCP access roles. This is not switched on for your organization yet.", ProjectScopeNone},
		{"list_access_members", "Find Organization Members for MCP Access", "Find organization members for MCP access. This is not switched on for your organization yet.", ProjectScopeNone},
		{"get_mcp_access", "Inspect Access to One MCP Server", "Inspect access to one MCP server. This is not switched on for your organization yet.", ProjectScopeExplicit},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, ToolMeta{Audiences: externalOnly, ProjectScope: tool.projectScope}, unavailableTool("mcp_access_reads"))
	}
}

func accessReadToolResult(err error) (*mcp.CallToolResult, bool) {
	var result accessReadRefusalResult
	switch {
	case errors.Is(err, ErrAccessQueryRequired):
		result = accessReadRefusalResult{Code: "invalid_request", Message: "Supply an identity query of at least three characters or choose one of the role references returned by list_access_roles; the full employee directory is not exposed."}
	case errors.Is(err, ErrAccessReferenceNotFound):
		result = accessReadRefusalResult{Code: "not_found", Message: "That access reference is not available here. List the roles or members again and use a reference from the new result."}
	case errors.Is(err, ErrAccessMCPNotFound):
		result = accessReadRefusalResult{Code: "not_found", Message: "That MCP server is not in the selected project. Choose one returned by find_mcp."}
	default:
		if budgetResult, ok := operationBudgetToolResult(err); ok {
			return budgetResult, true
		}
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
