package hooks

import (
	"context"
	"net/url"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	organizationsRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// orgSiteURL returns the dashboard base URL of the links a deny response hands
// to an organization's users: the block page, the policy acknowledgement and
// the policy bypass request. It follows the organization's default host, and
// an organization without one (or whose row cannot be read) gets the legacy
// host. It returns nil when no dashboard links can be built.
//
// It reads the organization row, so call it only when a link is minted, not on
// every hook call.
func (s *Service) orgSiteURL(ctx context.Context, organizationID string) *url.URL {
	if s.orgHosts == nil {
		return nil
	}
	defaultHost := pgtype.Text{String: "", Valid: false}
	if s.db != nil && organizationID != "" {
		org, err := organizationsRepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to read organization default host for deny links; using legacy host",
				attr.SlogError(err),
				attr.SlogOrganizationID(organizationID),
			)
		} else {
			defaultHost = org.DefaultHost
		}
	}
	return s.orgHosts.SiteURL(defaultHost)
}
