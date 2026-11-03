package plugins

import (
	"context"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
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
	configured, err := url.Parse(s.serverURL)
	if err != nil {
		return s.serverURL
	}
	base, _ := s.orgHosts.StoredPlatformBaseURLsOr(defaultHost, configured, nil)
	return strings.TrimRight(base.String(), "/")
}

// projectServerURL is organizationServerURL for the organization that owns
// projectID. It reads the project only when a resolver is set, and falls back
// to the configured server URL when that read fails.
func (s *Service) projectServerURL(ctx context.Context, projectID uuid.UUID) string {
	if s.orgHosts == nil {
		return s.serverURL
	}
	project, err := projectsrepo.New(s.db).GetProjectWithOrganizationMetadata(ctx, projectID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to fetch organization default host, falling back to server url", attr.SlogError(err))
		return s.serverURL
	}
	return s.organizationServerURL(project.DefaultHost)
}
