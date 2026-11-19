package hooks

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	organizationsRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// projectGatewayName is what the dashboard calls the derived gateway that
// serves every server in a project.
const projectGatewayName = "Project Gateway"

// projectGatewayRoute is the project's derived gateway as a route to an
// official server. That gateway is not stored: it serves every server in the
// project the signed-in person can reach, so a new official server joins it
// with no write. It is a route only while the organization is served a
// gateway. The organization-wide gateway address reaches the same server, so
// a session connected there counts as this route too.
func (s *Service) projectGatewayRoute(ctx context.Context, organizationID string, projectID string) (shadowmcp.OfficialRoute, bool) {
	var none shadowmcp.OfficialRoute
	if s.db == nil || s.orgHosts == nil || organizationID == "" {
		return none, false
	}
	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		return none, false
	}
	org, err := organizationsRepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		s.logger.WarnContext(ctx, "read organization for project gateway route", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return none, false
	}
	_, unavailable, err := orggateway.Availability(ctx, s.db, org)
	if err != nil {
		s.logger.WarnContext(ctx, "resolve project gateway availability", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return none, false
	}
	if unavailable != orggateway.UnavailabilityNone {
		return none, false
	}
	project, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: parsedProjectID, OrganizationID: organizationID})
	if err != nil {
		s.logger.WarnContext(ctx, "read project for project gateway route", attr.SlogError(err), attr.SlogProjectID(projectID))
		return none, false
	}
	base := s.orgHosts.ServerURL(org.DefaultHost)
	return shadowmcp.NewOfficialRoute(projectGatewayName, orggateway.ProjectURL(base, org.Slug, project.Slug), orggateway.URL(base, org.Slug)), true
}
