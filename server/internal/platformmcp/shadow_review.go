package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// ErrShadowMCPReviewRequired means an enabled Shadow MCP block policy on the
// project refuses the user-supplied URL for the people it would reach, so the
// write did not happen. When a review request could be filed on the caller's
// behalf the error is a *ShadowMCPReviewRequiredError carrying that request.
var ErrShadowMCPReviewRequired = errors.New("platform mcp shadow mcp review required")

const (
	shadowMCPReviewNextAction  = "await_shadow_mcp_review"
	shadowMCPReviewMaxPolicies = 10
	// shadowMCPPolicyActionBlock is the risk policy action that makes a Shadow
	// MCP policy refuse unreviewed servers.
	shadowMCPPolicyActionBlock = "block"
	// shadowMCPReviewRequestedCode reports that a review was filed for the
	// refused server; shadowMCPReviewRequiredCode reports the same refusal when
	// no review could be filed, so the caller must request one.
	shadowMCPReviewRequestedCode = "shadow_mcp_review_requested"
	shadowMCPReviewRequiredCode  = "shadow_mcp_review_required"
)

// ShadowMCPReviewRequest is the bounded, caller-facing state after a review was
// filed because a Shadow MCP block policy refused a direct-remote URL. It
// carries only what the administrator needs next: where the review lives,
// which policies are in force, and a sentence they can repeat.
type ShadowMCPReviewRequest struct {
	// RequestID is the review request this call created or converged on.
	RequestID string
	// Status is the request's current status, normally "requested".
	Status string
	// Target is the stored, credential-redacted form of the refused URL.
	Target string
	// ReviewURL is the dashboard page where the request is reviewed. Empty when
	// no dashboard URL is configured.
	ReviewURL string
	// PolicyNames are the enabled block policies on the project, by name.
	PolicyNames []string
	// Explanation is the plain-language account of why nothing was written and
	// what happens next.
	Explanation string
}

// ShadowMCPReviewRequiredError is ErrShadowMCPReviewRequired with the filed
// request attached. It also satisfies errors.Is for
// ErrDistributionBlockedPendingApproval so existing conflict mappings hold.
type ShadowMCPReviewRequiredError struct {
	Review ShadowMCPReviewRequest
	Cause  error
}

func (e *ShadowMCPReviewRequiredError) Error() string { return ErrShadowMCPReviewRequired.Error() }

func (e *ShadowMCPReviewRequiredError) Is(target error) bool {
	return errors.Is(target, ErrShadowMCPReviewRequired) || errors.Is(target, ErrDistributionBlockedPendingApproval)
}

func (e *ShadowMCPReviewRequiredError) Unwrap() error { return e.Cause }

// ShadowMCPReviewFiler files a review request for a direct-remote URL that an
// enabled Shadow MCP block policy refuses. Filing never writes project
// configuration or plugin membership. activity is what the caller was doing,
// in the administrator's words ("adding X to the Support plugin"), and is the
// recorded justification unless the caller supplies one.
type ShadowMCPReviewFiler interface {
	FileShadowMCPReview(ctx context.Context, principal Principal, project ResolvedProject, canonicalURL, activity, justification string) (ShadowMCPReviewRequest, error)
}

// ShadowMCPReviewLinker builds the dashboard page for a review request.
type ShadowMCPReviewLinker interface {
	ShadowMCPReviewURL(ctx context.Context, principal Principal, project ResolvedProject, serverSlug string) string
}

// ShadowMCPPolicyNameLister names the enabled block policies on a project.
type ShadowMCPPolicyNameLister func(ctx context.Context, projectID uuid.UUID) ([]string, error)

// ShadowMCPReviewService joins the two halves the product already has: the
// project's enabled block policies and the MCP review request flow. It files
// requests through the same service request_mcp_review uses, so a request
// filed by an agent on an administrator's behalf is indistinguishable in the
// queue from one the administrator typed.
type ShadowMCPReviewService struct {
	requests      MCPReviewRequestService
	policyNames   ShadowMCPPolicyNameLister
	dashboardURL  *url.URL
	organizations OrganizationSlugResolver
}

func NewShadowMCPReviewService(db *pgxpool.Pool, requests MCPReviewRequestService, dashboardURL *url.URL, organizations OrganizationSlugResolver) *ShadowMCPReviewService {
	service := &ShadowMCPReviewService{requests: requests, policyNames: nil, dashboardURL: nil, organizations: organizations}
	if db != nil {
		service.policyNames = postgresShadowMCPPolicyNames(db)
	}
	if validDashboardURL(dashboardURL) {
		copyURL := *dashboardURL
		service.dashboardURL = &copyURL
	}
	return service
}

// WithPolicyNames replaces the policy-name source, which tests use to avoid a
// database.
func (s *ShadowMCPReviewService) WithPolicyNames(lister ShadowMCPPolicyNameLister) *ShadowMCPReviewService {
	if s != nil && lister != nil {
		s.policyNames = lister
	}
	return s
}

var _ ShadowMCPReviewFiler = (*ShadowMCPReviewService)(nil)
var _ ShadowMCPReviewLinker = (*ShadowMCPReviewService)(nil)

func (s *ShadowMCPReviewService) FileShadowMCPReview(ctx context.Context, principal Principal, project ResolvedProject, canonicalURL, activity, justification string) (ShadowMCPReviewRequest, error) {
	if s == nil || s.requests == nil || s.policyNames == nil || principal.OrganizationID == "" || principal.UserID == "" || project.ID == uuid.Nil || canonicalURL == "" {
		return ShadowMCPReviewRequest{}, ErrUnavailable
	}
	names, err := s.policyNames(ctx, project.ID)
	if err != nil {
		return ShadowMCPReviewRequest{}, fmt.Errorf("list Shadow MCP block policies for review: %w", err)
	}
	// The request service validates the note's length and presence itself.
	note := strings.TrimSpace(justification)
	if note == "" {
		note = "Requested through the Platform MCP while " + activity + "."
	}
	created, err := s.requests.CreatePlatformRequest(ctx, principal.OrganizationID, project.ID, principal.UserID, mcpapproval.TargetKindServerURL, canonicalURL, note)
	if err != nil {
		return ShadowMCPReviewRequest{}, fmt.Errorf("file Shadow MCP review request: %w", err)
	}
	reviewURL := ""
	if created.ServerSlug != nil {
		reviewURL = s.ShadowMCPReviewURL(ctx, principal, project, *created.ServerSlug)
	}
	return ShadowMCPReviewRequest{
		RequestID:   created.ID,
		Status:      created.Status,
		Target:      created.TargetRaw,
		ReviewURL:   reviewURL,
		PolicyNames: names,
		Explanation: shadowMCPReviewExplanation(project.Slug, names, created.Status),
	}, nil
}

// ShadowMCPReviewURL is the Shadow MCP server page, which is where a pending
// request is shown and decided. It is the same page the inventory links to,
// so a request filed here and one filed in the dashboard land in one place.
func (s *ShadowMCPReviewService) ShadowMCPReviewURL(ctx context.Context, principal Principal, project ResolvedProject, serverSlug string) string {
	if s == nil || s.dashboardURL == nil || s.organizations == nil || project.Slug == "" || serverSlug == "" {
		return ""
	}
	organizationSlug, err := s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil || organizationSlug == "" {
		return ""
	}
	return s.dashboardURL.JoinPath(organizationSlug, "projects", project.Slug, "shadow-ai", "mcps", serverSlug).String()
}

func shadowMCPReviewExplanation(projectSlug string, policyNames []string, status string) string {
	var b strings.Builder
	b.WriteString("Your organisation has a policy that blocks MCP servers that have not been reviewed")
	switch len(policyNames) {
	case 0:
		b.WriteString(" for the " + projectSlug + " project.")
	case 1:
		b.WriteString(" for the " + projectSlug + " project (" + policyNames[0] + ").")
	default:
		b.WriteString(" for the " + projectSlug + " project (" + strings.Join(policyNames, ", ") + ").")
	}
	b.WriteString(" Nothing was changed. A review of this server has been requested on your behalf")
	if status != "" && status != mcpapproval.StatusRequested {
		b.WriteString(" and is currently " + status)
	}
	b.WriteString(". Once it is approved for the people who should receive it, run this step again. As an organization administrator you can decide the review yourself, in the dashboard or with decide_shadow_mcp_access.")
	return b.String()
}

// postgresShadowMCPPolicyNames lists the enabled block-action Shadow MCP
// policies on a project, by name, sorted and bounded. Names are the only
// thing an administrator needs to recognise which rule is in force.
func postgresShadowMCPPolicyNames(db *pgxpool.Pool) ShadowMCPPolicyNameLister {
	return func(ctx context.Context, projectID uuid.UUID) ([]string, error) {
		policies, err := riskrepo.New(db).ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("list enabled Shadow MCP policies: %w", err)
		}
		names := make([]string, 0, len(policies))
		for _, policy := range policies {
			if policy.Action == shadowMCPPolicyActionBlock {
				names = append(names, strings.TrimSpace(policy.Name))
			}
		}
		names = conv.DedupeNonEmpty(names)
		sort.Strings(names)
		if len(names) > shadowMCPReviewMaxPolicies {
			names = names[:shadowMCPReviewMaxPolicies]
		}
		return names, nil
	}
}

// ShadowMCPReviewToolOutput is the tool-facing projection of a filed review.
type ShadowMCPReviewToolOutput struct {
	RequestID   string   `json:"request_id"`
	Status      string   `json:"status"`
	Target      string   `json:"target"`
	ReviewURL   string   `json:"review_url,omitempty"`
	PolicyNames []string `json:"policy_names,omitempty"`
	Explanation string   `json:"explanation"`
}

func (r ShadowMCPReviewRequest) toolOutput() ShadowMCPReviewToolOutput {
	return ShadowMCPReviewToolOutput{
		RequestID:   r.RequestID,
		Status:      r.Status,
		Target:      r.Target,
		ReviewURL:   r.ReviewURL,
		PolicyNames: append([]string(nil), r.PolicyNames...),
		Explanation: r.Explanation,
	}
}

// shadowMCPReviewToolOutput extracts a filed review from a service error. The
// second result is false when the error is not a filed review, including the
// bare ErrShadowMCPReviewRequired a service returns when no filer is wired.
func shadowMCPReviewToolOutput(err error) (ShadowMCPReviewToolOutput, bool) {
	var review *ShadowMCPReviewRequiredError
	if !errors.As(err, &review) || review.Review.RequestID == "" {
		return ShadowMCPReviewToolOutput{}, false
	}
	return review.Review.toolOutput(), true
}
