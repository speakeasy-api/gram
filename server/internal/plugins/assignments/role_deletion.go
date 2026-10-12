package assignments

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/plugins/installmode"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RoleDeletion describes the existing role lifecycle's scope and audit actor.
// OrganizationID is empty only for a global role, whose deletion affects all orgs.
type RoleDeletion struct {
	OrganizationID   string
	PrincipalURN     string
	Actor            urn.Principal
	ActorDisplayName *string
}

// RemoveDeletedRole removes only the deleted role's audience rows and audits
// each changed plugin. Call inside the role-deletion transaction, after marking
// the role deleted: that lock serializes with role distribution setup, and the
// separate cleanup statement sees any assignment that setup committed first.
// No plugin content or publication state is changed.
func RemoveDeletedRole(ctx context.Context, tx pluginsrepo.DBTX, logger *audit.Logger, input RoleDeletion) error {
	principal, err := urn.ParsePrincipal(input.PrincipalURN)
	if err != nil || principal.Type != urn.PrincipalTypeRole || input.Actor.IsZero() || logger == nil {
		return ErrInvalid
	}
	kind, id, ok := strings.Cut(principal.ID, ":")
	roleID, err := uuid.Parse(id)
	if !ok || err != nil || roleID == uuid.Nil || (kind != "organization" && kind != "global") || (kind == "global") != (input.OrganizationID == "") {
		return ErrInvalid
	}
	queries := pluginsrepo.New(tx)
	var targets []pluginsrepo.Plugin
	if kind == "global" {
		targets, err = queries.ListPluginsForGlobalRoleDeletion(ctx, input.PrincipalURN)
	} else {
		targets, err = queries.ListPluginsForRoleDeletion(ctx, pluginsrepo.ListPluginsForRoleDeletionParams{
			OrganizationID: input.OrganizationID, PrincipalUrn: input.PrincipalURN,
		})
	}
	if err != nil {
		return fmt.Errorf("find deleted role plugin assignments: %w", err)
	}
	for _, plugin := range targets {
		removed, err := queries.RemoveDeletedRolePluginAssignment(ctx, pluginsrepo.RemoveDeletedRolePluginAssignmentParams{
			OrganizationID: plugin.OrganizationID, ProjectID: plugin.ProjectID, PluginID: plugin.ID,
			PrincipalUrn: input.PrincipalURN,
		})
		if err != nil {
			return fmt.Errorf("remove deleted role plugin assignment: %w", err)
		}
		if removed == 0 {
			continue
		}
		audience, err := queries.ListPluginAudienceForRoleDeletionAudit(ctx, pluginsrepo.ListPluginAudienceForRoleDeletionAuditParams{
			OrganizationID: plugin.OrganizationID, ProjectID: plugin.ProjectID, PluginID: plugin.ID,
		})
		if err != nil {
			return fmt.Errorf("read remaining plugin audience: %w", err)
		}
		remaining := make([]string, 0, len(audience))
		modes := make(map[string]string, len(audience))
		for _, assignment := range audience {
			remaining = append(remaining, assignment.PrincipalUrn)
			modes[assignment.PrincipalUrn] = string(installmode.FromStored(assignment.InstallMode))
		}
		if err := logger.LogPluginAssignmentsSet(ctx, tx, audit.LogPluginAssignmentsSetEvent{
			OrganizationID: plugin.OrganizationID, ProjectID: plugin.ProjectID,
			Actor: input.Actor, ActorDisplayName: input.ActorDisplayName, ActorSlug: nil,
			PluginID: plugin.ID, PluginName: plugin.Name, PluginSlug: plugin.Slug, PrincipalURNs: remaining,
			InstallModes: modes,
		}); err != nil {
			return fmt.Errorf("audit deleted role plugin assignments: %w", err)
		}
	}
	return nil
}
