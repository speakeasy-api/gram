package mcp

import (
	"context"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// CodeModeAvailable checks runtime configuration and the temporary rollout gate
// for a new setting or connection override. Callers separately enforce the
// discovery-settings entitlement and RBAC. Existing code-mode executions never
// consult this gate: switching off rollout must not reinterpret stored policy.
func (s *Service) CodeModeAvailable(ctx context.Context, organizationID string, projectID uuid.UUID) bool {
	if s.codeExecutor == nil || !s.codeExecutor.Enabled() {
		return false
	}
	organization, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return false
	}
	project, err := projectrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectrepo.GetProjectByIDAndOrganizationIDParams{
		ID: projectID, OrganizationID: organizationID,
	})
	if err != nil {
		return false
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.features, feature.FlagGatewayCodeMode, organizationID, feature.OrgProjectGroups(organization.Slug, project.Slug))
	return err == nil && evaluation == feature.EvaluationEnabled
}
