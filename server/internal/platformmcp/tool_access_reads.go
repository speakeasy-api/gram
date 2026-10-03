//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
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
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListAccessRolesOutput, error) {
		return principalToolCall(ctx, accessReadToolResult, func(principal Principal) (ListAccessRolesOutput, error) {
			return accessReads.ListRoles(ctx, principal)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_access_members",
		Title:       "Find Organization Members for MCP Access",
		Description: "Find organization members by an explicit identity query of at least three characters (part of a name, email, or role name). External clients may instead filter by a role reference from list_access_roles; that option is not available to the project assistant, which has no list_access_roles and must use the identity query. Identities are masked (for example a***@e***) and are never returned in full, so confirm a match from the query and the returned role names rather than expecting a display name. Results are enumerated only when at least five people match; smaller cohorts are withheld and reported only as a suppressed count. Each member carries a short-lived opaque reference and version bound to this surface and session: they identify nobody on their own and are consumed only by assign_mcp_access_role, which is available to external clients, before they expire.",
		Annotations: readOnlyAnnotations(),
		InputSchema: listAccessMembersInputSchema(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListAccessMembersInput) (*mcp.CallToolResult, ListAccessMembersOutput, error) {
		return principalToolCall(ctx, accessReadToolResult, func(principal Principal) (ListAccessMembersOutput, error) {
			return accessReads.ListMembers(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_mcp_access",
		Title:       "Inspect Access to One MCP Server",
		Description: "Inspect which roles can enter one exact configured MCP server through its configured endpoint and which known tools or behavior classes they can use. Uses the same authorization resource and selector semantics as that endpoint; dynamic servers may not have an enumerable tool catalog.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPAccessInput) (*mcp.CallToolResult, GetMCPAccessOutput, error) {
		return principalToolCall(ctx, accessReadToolResult, func(principal Principal) (GetMCPAccessOutput, error) {
			return accessReads.GetMCPAccess(ctx, principal, input)
		})
	})
}

// listAccessMembersInputSchema is the input contract list_access_members
// advertises. It is inferred from the typed input.
func listAccessMembersInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[ListAccessMembersInput](nil)
	if err != nil {
		panic(fmt.Sprintf("platformmcp: infer list_access_members input schema: %v", err))
	}
	return schema
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
