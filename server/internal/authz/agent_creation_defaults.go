package authz

import (
	"context"
	"fmt"
	"slices"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// adminAgentCreationInitializer identifies the durable audit receipt shared by
// new-organization provisioning and the explicit one-time operator backfill.
const adminAgentCreationInitializer = "admin-agent-creation-initialization"

// InitializeAdminAgentCreationTx initializes the built-in Admin creation grant
// once per organization. Callers must supply a transaction and commit the grant
// and completion audit together. A dry run must roll that transaction back.
// The completion receipt is independent of grant rows, so later removal sticks.
func InitializeAdminAgentCreationTx(ctx context.Context, dbtx accessrepo.DBTX, organizationID string) (bool, error) {
	if _, err := orgrepo.New(dbtx).LockOrganizationForAdminConfiguration(ctx, organizationID); err != nil {
		return false, fmt.Errorf("lock organization for Admin creation initialization: %w", err)
	}
	done, err := auditrepo.New(dbtx).HasAdminAgentCreationInitialization(ctx, auditrepo.HasAdminAgentCreationInitializationParams{
		OrganizationID: organizationID, ActorID: adminAgentCreationInitializer,
	})
	if err != nil {
		return false, fmt.Errorf("read Admin creation initialization receipt: %w", err)
	}
	if done {
		return false, nil
	}
	role, err := accessrepo.New(dbtx).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
	if err != nil {
		return false, fmt.Errorf("read global Admin role: %w", err)
	}
	if role.Deleted || role.WorkosDeleted {
		return false, fmt.Errorf("global Admin role is inactive")
	}
	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+role.ID.String())
	rows, err := accessrepo.New(dbtx).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{
		OrganizationID: organizationID, PrincipalUrns: []string{principal.String()},
	})
	if err != nil {
		return false, fmt.Errorf("read stored Admin creation grants: %w", err)
	}
	hasCreate := slices.ContainsFunc(rows, func(row accessrepo.GetPrincipalGrantsRow) bool { return row.Scope == string(ScopeAgentCreate) })
	if !hasCreate {
		if _, err := PatchRoleGrantsTx(ctx, dbtx, organizationID, SystemRoleAdmin, principal.String(), []*RoleGrant{{Scope: string(ScopeAgentCreate), Selectors: nil}}, nil); err != nil {
			return false, fmt.Errorf("initialize stored Admin creation grant: %w", err)
		}
	}
	if err := audit.NewLogger(audit.WithoutOutbox()).LogAccessRoleUpdate(ctx, dbtx, audit.LogAccessRoleUpdateEvent{
		OrganizationID: organizationID,
		Actor:          urn.NewSystemPrincipal(adminAgentCreationInitializer), ActorDisplayName: nil, ActorSlug: nil,
		RoleID: role.ID.String(), RoleName: role.WorkosName, RoleSlug: role.WorkosSlug,
		RoleSnapshotBefore: nil, RoleSnapshotAfter: nil,
	}); err != nil {
		return false, fmt.Errorf("record Admin creation initialization: %w", err)
	}
	return !hasCreate, nil
}
