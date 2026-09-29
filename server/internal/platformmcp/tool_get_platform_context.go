//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func registerGetPlatformContextTool(reg *Registrar, backend roleProvisioningBackend) {
	addTool(reg, &mcp.Tool{
		Name:        "get_platform_context",
		Title:       "Show the Current Organization",
		Description: "Show which organization this session is working in, explain how live RBAC affects the shared Platform MCP catalogue, and identify unsupported issuer and identity-chaining remediation workflows. For external organization administrators, includes live role-plugin provisioning settings, destination projects and status (up to 100 roles/projects); members and managed assistants never receive organization-wide provisioning detail. If provisioning status cannot be read, returns the remaining context with an explicit unavailability notice, not a disabled setting. Call this first in a new conversation before choosing a permitted workflow.",
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
		var provisioning *RoleProvisioningContext
		var provisioningUnavailable string
		if canReadProvisioning(ctx, principal) {
			provisioningUnavailable = "Role plugin provisioning status is temporarily unavailable. Retry before making configuration changes; do not infer that provisioning is off."
			if backend != nil {
				if status, err := backend.Status(ctx, principal.OrganizationID); err == nil {
					provisioning = provisioningContext(status)
					provisioningUnavailable = ""
				}
			}
		}
		return nil, PlatformContext{
			RoleProvisioning:            provisioning,
			RoleProvisioningUnavailable: provisioningUnavailable,
			OrganizationID:              principal.OrganizationID,
			ConnectionID:                principal.ConnectionID,
			ReadOnly:                    false,
			Overview:                    platformOverview,
			AvailableWorkflows:          available,
			RequestableWorkflows:        requestable,
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
