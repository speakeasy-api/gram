//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func registerGetPlatformContextTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "get_platform_context",
		Title:       "Show the Current Organization",
		Description: "Show which organization this session is working in and explain how live RBAC affects the shared Platform MCP catalogue. Call this first in a new conversation before choosing a permitted workflow.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, PlatformContext, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, PlatformContext{}, err
		}
		available, requestable := []string{}, []string{}
		if principal.surface() == SurfacePlatformMCP {
			grants, ok := authz.GrantsFromContext(ctx)
			if !ok {
				return nil, PlatformContext{}, ErrUnavailable
			}
			available, requestable = platformWorkflowCategories(grants, principal.OrganizationID)
		}
		return nil, PlatformContext{
			OrganizationID:       principal.OrganizationID,
			ConnectionID:         principal.ConnectionID,
			ReadOnly:             false,
			Overview:             platformOverview,
			AvailableWorkflows:   available,
			RequestableWorkflows: requestable,
		}, nil
	})
}

func platformWorkflowCategories(grants []authz.Grant, organizationID string) ([]string, []string) {
	categories := []struct {
		name   string
		scopes []authz.Scope
	}{
		{name: "project discovery", scopes: discoveryProjectRead},
		{name: "MCP discovery and connection", scopes: discoveryMCPReadOrConnect},
		{name: "assigned plugin installation", scopes: discoveryOrgRead},
		{name: "standalone MCP installation", scopes: []authz.Scope{authz.ScopeMCPConnect}},
		{name: "skill reading and feedback", scopes: discoverySkillRead},
		{name: "skill authoring", scopes: discoverySkillWrite},
		{name: "organization administration", scopes: []authz.Scope{authz.ScopeOrgAdmin}},
	}
	available := []string{"personal session recall"}
	requestable := make([]string, 0, len(categories))
	for _, category := range categories {
		if grantsAuthorizeAnyScope(grants, organizationID, category.scopes) {
			available = append(available, category.name)
		} else {
			requestable = append(requestable, category.name)
		}
	}
	return available, requestable
}
