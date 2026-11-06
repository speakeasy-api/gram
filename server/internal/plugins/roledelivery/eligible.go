package roledelivery

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authz"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

// Eligible delivers only the backend whose eligibility just changed. Existing
// role audiences supply access; replay never restores removed membership.
// Only grants naming the server deliver it here: a role allowed to use every
// server would otherwise pull each newly enabled one into its plugins.
// Callers hold project admission before any plugin locks and enqueue publication.
func Eligible(ctx context.Context, tx pgx.Tx, org string, projectID, mcpServerID uuid.UUID, guard *admission.Guard) (bool, error) {
	candidates, err := servers(ctx, tx, org, projectID)
	if err != nil {
		return false, err
	}
	var selected *server
	for _, candidate := range candidates {
		if candidate.Eligible && candidate.ID == mcpServerID {
			selected = &candidate
			break
		}
	}
	if selected == nil {
		return false, nil
	}
	ids, err := pluginsrepo.New(tx).ListProjectRoleDeliveryPluginsForUpdate(ctx, pluginsrepo.ListProjectRoleDeliveryPluginsForUpdateParams{OrganizationID: org, ProjectID: projectID})
	if err != nil {
		return false, fmt.Errorf("list eligible server role plugins: %w", err)
	}
	changed := false
	for _, id := range ids {
		assignments, err := pluginsrepo.New(tx).ListPluginAssignments(ctx, pluginsrepo.ListPluginAssignmentsParams{PluginID: id, OrganizationID: org, ProjectID: projectID})
		if err != nil {
			return false, fmt.Errorf("list eligible server plugin audiences: %w", err)
		}
		roles := make([]string, 0, len(assignments))
		for _, assignment := range assignments {
			roles = append(roles, assignment.PrincipalUrn)
		}
		grants, err := roleGrants(ctx, tx, org, roles)
		if err != nil {
			return false, err
		}
		for role, g := range grants {
			grants[role] = withoutUntargetedUse(g)
		}
		yes, err := supplied(grants, *selected)
		if err != nil {
			return false, err
		}
		if !yes {
			continue
		}
		did, err := apply(ctx, tx, org, id, *selected, true, true, guard)
		if err != nil {
			return false, err
		}
		changed = changed || did
	}
	return changed, nil
}

// withoutUntargetedUse drops Use grants that don't name a specific server.
// Exclusions are kept so a block still wins over a targeted grant.
func withoutUntargetedUse(grants []authz.Grant) []authz.Grant {
	return slices.DeleteFunc(slices.Clone(grants), func(g authz.Grant) bool {
		if g.Scope != authz.ScopeMCPConnect && g.Scope != authz.ScopeRoot {
			return false
		}
		return g.Selector.ResourceID() == authz.WildcardResource
	})
}
