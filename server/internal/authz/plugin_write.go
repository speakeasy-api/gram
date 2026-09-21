package authz

import "context"

// RequirePluginWrite authorizes project-scoped plugin content mutations and
// publishing, without granting edits to the referenced skills or MCP servers.
// Keep the org-admin alternative for existing roles, whose grants are not
// rewritten when new default capabilities are introduced. Explicit exclusions
// still win over either allow alternative.
func (e *Engine) RequirePluginWrite(ctx context.Context, organizationID, projectID string) error {
	return e.RequireAnyUnblocked(ctx,
		Check{Scope: ScopePluginWrite, ResourceKind: ResourceKindProject, ResourceID: projectID, Dimensions: nil, selectorMatch: selectorMatchNormal},
		Check{Scope: ScopeOrgAdmin, ResourceKind: ResourceKindOrg, ResourceID: organizationID, Dimensions: nil, selectorMatch: selectorMatchNormal},
	)
}
