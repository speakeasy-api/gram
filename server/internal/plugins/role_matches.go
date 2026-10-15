package plugins

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// RolePluginsForResource is a read-only, batched projection of live plugin
// contents and exact assignments, not proof of client delivery or authorization.
// nil means undisclosed; an allocated empty slice means no matches.
func RolePluginsForResource(ctx context.Context, db repo.DBTX, engine *authz.Engine, organizationID string, projectID, resourceID uuid.UUID, principals []string) ([]repo.ListRolePluginsForResourceRow, error) {
	if _, ok := authz.GrantsFromContext(ctx); !ok {
		return nil, nil
	}
	admin, err := engine.Evaluate(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: authz.ResourceKindOrg, ResourceID: organizationID, Dimensions: nil})
	if err != nil {
		return nil, fmt.Errorf("evaluate plugin assignment disclosure: %w", err)
	}
	if !admin {
		return nil, nil
	}
	reader, err := engine.Evaluate(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: authz.ResourceKindOrg, ResourceID: organizationID, Dimensions: nil})
	if err != nil {
		return nil, fmt.Errorf("evaluate plugin read: %w", err)
	}
	if !reader && engine.RequirePluginWrite(ctx, organizationID, projectID.String()) != nil {
		return nil, nil
	}
	roles := make([]string, 0, len(principals))
	for _, principal := range principals {
		if strings.HasPrefix(principal, "role:") {
			roles = append(roles, principal)
		}
	}
	rows, err := repo.New(db).ListRolePluginsForResource(ctx, repo.ListRolePluginsForResourceParams{OrganizationID: organizationID, ProjectID: projectID, ResourceID: resourceID, PrincipalUrns: roles})
	if err != nil {
		return nil, fmt.Errorf("read matching role plugins: %w", err)
	}
	if rows == nil {
		rows = []repo.ListRolePluginsForResourceRow{}
	}
	return rows, nil
}
