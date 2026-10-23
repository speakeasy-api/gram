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
		Description: "List the organization's roles and summarize the MCP access each role carries. Each role also lists the directory groups and attribute values mapped to it, which under directory sync give the role to everyone they match; an empty list means nothing in the directory gives the role out, and mappings are changed on the dashboard's identity provider page. Member counts are withheld for small groups, and each role is represented by a short-lived opaque reference rather than a role ID or principal.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListAccessRolesOutput, error) {
		return principalToolCall(ctx, accessReadToolResult, func(principal Principal) (ListAccessRolesOutput, error) {
			return accessReads.ListRoles(ctx, principal)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_access_members",
		Title:       "Find Organization Members for MCP Access",
		Description: "Find organization members by an explicit identity query of at least three characters (part of a name, email, or role name). Role searches and returned role names include direct assignments and current directory group or attribute mappings. External clients may instead filter by a role reference from list_access_roles; that option is not available to the project assistant, which has no list_access_roles and must use the identity query. Identities are masked (for example a***@e***) and are never returned in full, so confirm a match from the query and the returned role names rather than expecting a display name. Results are enumerated only when at least five people match; smaller cohorts are withheld and reported only as a suppressed count, except that a query equal to one member's complete email address returns just that member with exact_email_match set, which confirms the masked identity is the person named. Each member carries a short-lived opaque reference and version bound to this surface and session: they identify nobody on their own and are consumed only by get_member_mcp_access or assign_mcp_access_role, which are available to external clients, before they expire. Do not use this directory search for the caller's own access; external clients use list_my_mcp_access instead.",
		Annotations: readOnlyAnnotations(),
		InputSchema: listAccessMembersInputSchema(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListAccessMembersInput) (*mcp.CallToolResult, ListAccessMembersOutput, error) {
		return principalToolCall(ctx, accessReadToolResult, func(principal Principal) (ListAccessMembersOutput, error) {
			return accessReads.ListMembers(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_my_mcp_access",
		Title:       "Inspect My Access Across MCP Servers",
		Description: "Inspect the authenticated caller's effective access across every configured MCP server without searching or enumerating the organization directory. Returns the same table-ready project and server rows as get_member_mcp_access. Use this tool for questions about the caller's own access; do not infer their roles or server access from their ability to call Platform MCP or from organization administration.",
		Annotations: readOnlyAnnotations(),
		InputSchema: listMyMCPAccessInputSchema(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, GetMemberMCPAccessOutput, error) {
		return principalToolCall(ctx, accessReadToolResult, func(principal Principal) (GetMemberMCPAccessOutput, error) {
			return accessReads.ListMyMCPAccess(ctx, principal)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_member_mcp_access",
		Title:       "Inspect One Member's Access Across MCP Servers",
		Description: "Inspect one exact organization member's effective access across every configured MCP server. Use list_my_mcp_access instead when the caller asks about their own access. For someone else, first select the member with list_access_members and pass its short-lived member reference. Returns table-ready rows with project and server names, visibility, effective connection and tool coverage, view and manage access, and grant/block rule sources including blocked servers and overridden exclusions. Role-scoped block and unblock mutations require the dashboard role editor: the MCP role-write contract supports connect grants only and does not offer a confirmed, versioned exclusion-edit workflow. Rule sources name roles or direct/everyone grants; member identifiers and directory attributes are withheld. Public and disabled endpoint behavior matches the dashboard access check; private endpoints use the member's live direct, role, everyone, and directory-mapped grants.",
		Annotations: readOnlyAnnotations(),
		InputSchema: getMemberMCPAccessInputSchema(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMemberMCPAccessInput) (*mcp.CallToolResult, GetMemberMCPAccessOutput, error) {
		return principalToolCall(ctx, accessReadToolResult, func(principal Principal) (GetMemberMCPAccessOutput, error) {
			return accessReads.GetMemberMCPAccess(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_mcp_access",
		Title:       "Inspect Access to One MCP Server",
		Description: "Inspect role access to one exact configured MCP or built-in ID in its explicit project. Built-in coverage requires an exact connect rule, applies only to already-assigned machines subject to credential and owner ceilings, never humans, and does not offer assignments or plugin distribution. Inspect configured servers through their configured endpoint and which known tools or behavior classes they can use. Uses the same authorization resource and selector semantics as that endpoint; dynamic servers may not have an enumerable tool catalog. Matching plugins summarize live contents assigned to each exact role, not access grants or installation state.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPAccessInput) (*mcp.CallToolResult, GetMCPAccessOutput, error) {
		return principalToolCall(ctx, accessReadToolResult, func(principal Principal) (GetMCPAccessOutput, error) {
			return accessReads.GetMCPAccess(ctx, principal, input)
		})
	})
}

func registerUnavailableAccessReadTools(reg *Registrar) {
	// Each stub declares the same audiences as its live registration, so an
	// audience sees one stable catalogue whether or not the service is wired.
	// list_access_members also carries its live input schema: the assistant
	// advertises that schema to a model, which must see one contract either way.
	for _, tool := range []struct {
		name         string
		title        string
		description  string
		audiences    []Audience
		projectScope ProjectScope
		inputSchema  *jsonschema.Schema
	}{
		{"list_access_roles", "List MCP Access Roles", "List MCP access roles. This is not switched on for your organization yet.", externalOnly, ProjectScopeNone, nil},
		{"list_access_members", "Find Organization Members for MCP Access", "Find organization members for MCP access. This is not switched on for your organization yet.", bothAudiences, ProjectScopeNone, listAccessMembersInputSchema()},
		{"list_my_mcp_access", "Inspect My Access Across MCP Servers", "Inspect your own access across MCP servers. This is not switched on for your organization yet.", externalOnly, ProjectScopeNone, listMyMCPAccessInputSchema()},
		{"get_member_mcp_access", "Inspect One Member's Access Across MCP Servers", "Inspect one member's access across MCP servers. This is not switched on for your organization yet.", externalOnly, ProjectScopeNone, getMemberMCPAccessInputSchema()},
		{"get_mcp_access", "Inspect Access to One MCP Server", "Inspect access to one MCP server. This is not switched on for your organization yet.", externalOnly, ProjectScopeExplicit, nil},
	} {
		stub := &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}
		// The SDK's InputSchema field is untyped, so a nil *jsonschema.Schema
		// stored in it would read as a present-but-empty schema and panic; only
		// a declared schema is assigned, the rest stay inferred.
		if tool.inputSchema != nil {
			stub.InputSchema = tool.inputSchema
		}
		addTool(reg, stub, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: tool.audiences, ProjectScope: tool.projectScope}, unavailableTool("mcp_access_reads"))
	}
}

func listMyMCPAccessInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[struct{}](nil)
	if err != nil {
		panic(fmt.Sprintf("platformmcp: infer list_my_mcp_access input schema: %v", err))
	}
	return schema
}

// getMemberMCPAccessInputSchema keeps the live and unavailable tool's input
// contract identical.
func getMemberMCPAccessInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[GetMemberMCPAccessInput](nil)
	if err != nil {
		panic(fmt.Sprintf("platformmcp: infer get_member_mcp_access input schema: %v", err))
	}
	return schema
}

// listAccessMembersInputSchema keeps the live and unavailable tool's input
// contract identical.
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
