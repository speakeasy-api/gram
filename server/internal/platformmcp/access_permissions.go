package platformmcp

import (
	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

// AccessPermission is one scope as the catalogue describes it, in the words
// the dashboard uses. Nothing here is per-organization: the catalogue is
// product configuration, so the tool reveals no grants, roles or resources.
type AccessPermission struct {
	Slug         string `json:"slug"`
	Label        string `json:"label"`
	Category     string `json:"category" jsonschema:"how the dashboard groups the permission; differs from resource_kind where one resource kind carries several permission families"`
	ResourceKind string `json:"resource_kind" jsonschema:"the kind of resource the permission is checked against, such as project or mcp"`
	Description  string `json:"description"`
	// Sensitive is true when granting the permission exposes other members'
	// private activity or allows destructive administration.
	Sensitive   bool   `json:"sensitive"`
	Sensitivity string `json:"sensitivity,omitempty" jsonschema:"why the permission is sensitive: conversations, tool_logs or destructive_admin; absent when it is not"`
	// SubScopes are the permissions a grant of this one also satisfies.
	SubScopes []string `json:"sub_scopes"`
}

// AccessWorkflow is a task that needs several permissions end to end.
type AccessWorkflow struct {
	Slug   string   `json:"slug"`
	Label  string   `json:"label"`
	Scopes []string `json:"scopes"`
}

type DescribeAccessPermissionsOutput struct {
	Scopes    []AccessPermission `json:"scopes"`
	Workflows []AccessWorkflow   `json:"workflows"`
}

// describeAccessPermissions projects the scope catalogue the way
// access.listScopes does for the dashboard, limited to the permissions a
// person chooses in a role. Exception scopes and internal scopes are left
// out: an agent explaining access has no use for them.
func describeAccessPermissions() DescribeAccessPermissionsOutput {
	catalog := authz.ScopeCatalog()
	scopes := make([]AccessPermission, 0, len(catalog))
	for _, entry := range catalog {
		visibility, ok := authz.ScopeVisibilityFor(entry.Scope)
		if !ok || visibility != authz.ScopeVisibilityUserVisible {
			continue
		}
		permission := AccessPermission{
			Slug:         string(entry.Scope),
			Label:        entry.Label,
			Category:     string(entry.Category),
			ResourceKind: entry.ResourceType,
			Description:  entry.Description,
			Sensitive:    entry.Sensitive(),
			Sensitivity:  "",
			SubScopes:    entry.SubScopes(),
		}
		if permission.Sensitive {
			permission.Sensitivity = string(entry.Sensitivity)
		}
		if permission.SubScopes == nil {
			permission.SubScopes = []string{}
		}
		scopes = append(scopes, permission)
	}

	catalogWorkflows := authz.Workflows()
	workflows := make([]AccessWorkflow, 0, len(catalogWorkflows))
	for _, workflow := range catalogWorkflows {
		names := make([]string, 0, len(workflow.Scopes))
		for _, scope := range workflow.Scopes {
			names = append(names, string(scope))
		}
		workflows = append(workflows, AccessWorkflow{Slug: workflow.Slug, Label: workflow.Label, Scopes: names})
	}

	return DescribeAccessPermissionsOutput{Scopes: scopes, Workflows: workflows}
}

// roleGrantScopes lists the scopes a role's grants name, for comparing what
// it gave before and after a write.
func roleGrantScopes(grants []*accessgen.RoleGrant) []authz.Scope {
	scopes := make([]authz.Scope, 0, len(grants))
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		scopes = append(scopes, authz.Scope(grant.Scope))
	}
	return scopes
}
