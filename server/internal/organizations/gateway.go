package organizations

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
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
	_, unavailable, err := orggateway.Resolve(ctx, usersessionsrepo.New(s.db), org.ID)
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
