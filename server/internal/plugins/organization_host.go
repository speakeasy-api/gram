package plugins

import (
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/orghost"
)

// WithOrganizationHosts makes generated plugins use the platform host stored as
// the organization's default host, the host the dashboard serves it on.
func (s *Service) WithOrganizationHosts(resolver *orghost.Resolver) *Service {
	if s != nil {
		s.orgHosts = resolver
	}
	return s
}

// organizationServerURL returns the base URL of the server routes and MCP
// addresses in an organization's generated plugins: the platform host stored
// as its default host, so plugins match the host its dashboard is served on,
// or the configured server URL when none is stored. Custom-domain addresses
// are resolved by the caller and never use this base.
func (s *Service) organizationServerURL(defaultHost pgtype.Text) string {
	if s.orgHosts != nil {
		if serverURL, _, ok := s.orgHosts.StoredPlatformBaseURLs(defaultHost); ok {
			return strings.TrimRight(serverURL.String(), "/")
		}
	}
	return s.serverURL
}
