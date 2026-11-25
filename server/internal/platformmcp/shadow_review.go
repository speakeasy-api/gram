package platformmcp

import (
	"context"
	"net/url"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// shadowMCPStandingApproved is the standing a direct-remote server has once an
// organization administrator's write has been recorded as its approval for the
// people who receive it.
const shadowMCPStandingApproved = "approved"

// ShadowMCPReviewLinker builds the dashboard page where a Shadow MCP server and
// its review live.
type ShadowMCPReviewLinker interface {
	ShadowMCPReviewURL(ctx context.Context, principal Principal, project ResolvedProject, serverSlug string) string
}

// ShadowMCPReviewLinkService builds Shadow MCP server page links. The page is
// the same one the inventory and the approval queue link to, so a server an
// administrator shared and a server someone asked about land in one place.
type ShadowMCPReviewLinkService struct {
	dashboardURL  *url.URL
	organizations OrganizationSlugResolver
}

func NewShadowMCPReviewLinkService(dashboardURL *url.URL, organizations OrganizationSlugResolver) *ShadowMCPReviewLinkService {
	service := &ShadowMCPReviewLinkService{dashboardURL: nil, organizations: organizations}
	if validDashboardURL(dashboardURL) {
		copyURL := *dashboardURL
		service.dashboardURL = &copyURL
	}
	return service
}

var _ ShadowMCPReviewLinker = (*ShadowMCPReviewLinkService)(nil)

func (s *ShadowMCPReviewLinkService) ShadowMCPReviewURL(ctx context.Context, principal Principal, project ResolvedProject, serverSlug string) string {
	if s == nil || s.dashboardURL == nil || s.organizations == nil || project.Slug == "" || serverSlug == "" {
		return ""
	}
	organizationSlug, err := s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil || organizationSlug == "" {
		return ""
	}
	return s.dashboardURL.JoinPath(organizationSlug, "projects", project.Slug, "shadow-ai", "mcps", serverSlug).String()
}

// ShadowMCPStanding is where a direct-remote server stands with the project's
// Shadow MCP policy after a write: whether the people who receive it are
// covered, and where to see the entry.
type ShadowMCPStanding struct {
	// State is "approved" when a standing approval covers the audience,
	// "not_required" when no block policy applies, or the raw admission state
	// otherwise.
	State string `json:"state"`
	// Target is the server URL as the inventory records it.
	Target string `json:"target"`
	// ReviewURL is the Shadow MCP server page. Empty without a dashboard URL.
	ReviewURL string `json:"review_url,omitempty"`
}

// shadowMCPStanding reads a direct-remote server's standing for one audience
// after the write committed. It is a read of what the runtime will enforce,
// not an echo of what the write intended.
func shadowMCPStanding(ctx context.Context, reads distributionAdmissionReader, links ShadowMCPReviewLinker, principal Principal, project ResolvedProject, remoteURL string) *ShadowMCPStanding {
	if reads == nil || project.ID == uuid.Nil || remoteURL == "" {
		return nil
	}
	canonical, ok := shadowmcp.CanonicalizeInventoryURL(remoteURL)
	if !ok {
		return nil
	}
	admission := reads.ForTarget(ctx, principal.OrganizationID, project.ID, canonical.CanonicalURL)
	state := admission.State
	if state == DistributionAdmissionCovered {
		state = shadowMCPStandingApproved
	}
	standing := &ShadowMCPStanding{State: state, Target: canonical.CanonicalURL, ReviewURL: ""}
	if links != nil {
		standing.ReviewURL = links.ShadowMCPReviewURL(ctx, principal, project, shadowmcp.ServerSlug(canonical.CanonicalURL))
	}
	return standing
}
