package admission

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/feature"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// WithGatewayPluginMembershipFeatures configures the rollout gate for automatic
// gateway attachments. Configure the shared guard before serving requests.
func (g *Guard) WithGatewayPluginMembershipFeatures(provider feature.Provider) *Guard {
	if g != nil {
		g.gatewayPluginMembershipFeatures = provider
	}
	return g
}

// AutomaticGatewayPluginMembershipEnabled fails closed when the scoped project
// or rollout cannot be resolved. Only automatic additions use this gate;
// removals and admission checks on existing memberships remain available.
func (g *Guard) AutomaticGatewayPluginMembershipEnabled(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID) bool {
	if g == nil || g.gatewayPluginMembershipFeatures == nil {
		return false
	}
	project, err := projectsrepo.New(tx).GetProjectWithOrganizationMetadata(ctx, projectID)
	if err != nil || project.ID != organizationID || project.DisabledAt.Valid {
		return false
	}
	state, err := feature.EvaluateFlag(ctx, g.gatewayPluginMembershipFeatures,
		feature.FlagGatewayPluginMembership, organizationID, feature.OrgProjectGroups(project.Slug, project.ProjectSlug))
	return err == nil && state == feature.EvaluationEnabled
}
