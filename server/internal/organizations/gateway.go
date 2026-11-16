package organizations

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// GetGateway reports the active organization's gateway URL, or why it is not
// served. The URL is under the organization's own host, which is where the
// gateway is served to its members.
func (s *Service) GetGateway(ctx context.Context, _ *gen.GetGatewayPayload) (*gen.OrganizationGateway, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization").LogError(ctx, s.logger)
	}
	_, unavailable, err := orggateway.Availability(ctx, s.db, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve organization gateway").LogError(ctx, s.logger)
	}
	if unavailable != orggateway.UnavailabilityNone {
		return &gen.OrganizationGateway{Available: false, URL: nil, UnavailableReason: new(string(unavailable))}, nil
	}
	return &gen.OrganizationGateway{
		Available:         true,
		URL:               new(orggateway.URL(s.orgHosts.ServerURL(org.DefaultHost), org.Slug)),
		UnavailableReason: nil,
	}, nil
}

// GetProjectGateway reports the selected project's gateway URL, or why it is
// not served. It is the organization's gateway limited to the project, so it
// is served exactly when the organization's is, but reading it needs only
// access to the project.
func (s *Service) GetProjectGateway(ctx context.Context, _ *gen.GetProjectGatewayPayload) (*gen.ProjectGateway, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}
	if ac.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: ac.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	project, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: *ac.ProjectID, OrganizationID: ac.ActiveOrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read project").LogError(ctx, s.logger)
	}
	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization").LogError(ctx, s.logger)
	}
	_, unavailable, err := orggateway.Availability(ctx, s.db, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve organization gateway").LogError(ctx, s.logger)
	}
	if unavailable != orggateway.UnavailabilityNone {
		return &gen.ProjectGateway{Available: false, URL: nil, UnavailableReason: new(string(unavailable))}, nil
	}
	return &gen.ProjectGateway{
		Available:         true,
		URL:               new(orggateway.ProjectURL(s.orgHosts.ServerURL(org.DefaultHost), org.Slug, project.Slug)),
		UnavailableReason: nil,
	}, nil
}
