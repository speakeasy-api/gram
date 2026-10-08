package platformmcp

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
)

type fixedOrganizationSlugs struct{ slug string }

func (f fixedOrganizationSlugs) OrganizationSlug(context.Context, string) (string, error) {
	return f.slug, nil
}

type recordingReviewRequests struct {
	calls         int
	organization  string
	project       uuid.UUID
	user          string
	targetKind    string
	target        string
	justification string
	serverSlug    *string
	err           error
}

func (r *recordingReviewRequests) CreatePlatformRequest(_ context.Context, organizationID string, projectID uuid.UUID, userID, targetKind, target, note string) (*gen.ApprovalRequestSummary, error) {
	r.calls++
	r.organization, r.project, r.user, r.targetKind, r.target, r.justification = organizationID, projectID, userID, targetKind, target, note
	if r.err != nil {
		return nil, r.err
	}
	return &gen.ApprovalRequestSummary{ID: "request-1", TargetKind: targetKind, TargetRaw: target, ServerSlug: r.serverSlug, Status: "requested"}, nil
}

func (r *recordingReviewRequests) ReadPlatformRequesterReview(context.Context, mcpapproval.PlatformRequesterReviewInput) (mcpapproval.PlatformRequesterReview, error) {
	panic("not used")
}

func newTestShadowMCPReviewService(requests MCPReviewRequestService, names []string) *ShadowMCPReviewService {
	dashboard, _ := url.Parse("https://app.example.test")
	return NewShadowMCPReviewService(nil, requests, dashboard, fixedOrganizationSlugs{slug: "acme"}).
		WithBudget(allowBudget()).
		WithPolicyNames(func(context.Context, uuid.UUID) ([]string, error) { return names, nil })
}

// Filing gathers evidence over the network, so it is charged to the same
// budget as request_mcp_review rather than running free on every retry.
func TestShadowMCPReviewServiceChargesTheReviewRequestBudget(t *testing.T) {
	t.Parallel()

	requests := &recordingReviewRequests{}
	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	principal := Principal{OrganizationID: "org", UserID: "admin", ConnectionID: "connection"}
	denied := OperationBudget{Connection: &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}}, Organization: allowOperationLimiter{}}

	_, err := newTestShadowMCPReviewService(requests, nil).WithBudget(denied).FileShadowMCPReview(t.Context(), principal, project, "https://mcp.example.test/server", "adding it", "")
	require.ErrorIs(t, err, ErrOperationRateLimited)
	require.Zero(t, requests.calls, "a denied budget files nothing")

	_, err = newTestShadowMCPReviewService(requests, nil).WithBudget(OperationBudget{}).FileShadowMCPReview(t.Context(), principal, project, "https://mcp.example.test/server", "adding it", "")
	require.ErrorIs(t, err, ErrUnavailable, "no budget fails closed")
	require.Zero(t, requests.calls)
}

func TestShadowMCPReviewServiceFilesRequestWithLinkAndPolicies(t *testing.T) {
	t.Parallel()

	slug := "mcp-example-test-server-deadbeef"
	requests := &recordingReviewRequests{serverSlug: &slug}
	service := newTestShadowMCPReviewService(requests, []string{"Block unreviewed MCPs", "Finance lockdown"})
	principal := Principal{OrganizationID: "org", UserID: "admin"}
	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}

	review, err := service.FileShadowMCPReview(t.Context(), principal, project, "https://mcp.example.test/server", "adding https://mcp.example.test/server to project project", "")
	require.NoError(t, err)

	require.Equal(t, 1, requests.calls)
	require.Equal(t, "org", requests.organization)
	require.Equal(t, project.ID, requests.project)
	require.Equal(t, "admin", requests.user)
	require.Equal(t, "server_url", requests.targetKind)
	require.Equal(t, "https://mcp.example.test/server", requests.target)
	require.Equal(t, "Requested through the Platform MCP while adding https://mcp.example.test/server to project project.", requests.justification)

	require.Equal(t, "request-1", review.RequestID)
	require.Equal(t, "requested", review.Status)
	require.Equal(t, "https://mcp.example.test/server", review.Target)
	require.Equal(t, "https://app.example.test/acme/projects/project/shadow-ai/mcps/"+slug, review.ReviewURL)
	require.Equal(t, []string{"Block unreviewed MCPs", "Finance lockdown"}, review.PolicyNames)
	require.Contains(t, review.Explanation, "Block unreviewed MCPs, Finance lockdown")
	require.Contains(t, review.Explanation, "Nothing was changed")
	require.Contains(t, review.Explanation, "decide_shadow_mcp_access")
}

func TestShadowMCPReviewServicePassesThroughAJustificationAndOmitsTheLinkWithoutASlug(t *testing.T) {
	t.Parallel()

	requests := &recordingReviewRequests{serverSlug: nil}
	service := newTestShadowMCPReviewService(requests, nil)
	project := ResolvedProject{ID: uuid.New(), Slug: "project"}

	review, err := service.FileShadowMCPReview(t.Context(), Principal{OrganizationID: "org", UserID: "admin"}, project, "https://mcp.example.test/server", "adding it", "  needed for the support rota  ")
	require.NoError(t, err)
	require.Equal(t, "needed for the support rota", requests.justification)
	require.Empty(t, review.ReviewURL)
	require.Empty(t, review.PolicyNames)
	require.Contains(t, review.Explanation, "for the project project.")
}

func TestShadowMCPReviewServiceFailsClosedWhenNothingIsWired(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	principal := Principal{OrganizationID: "org", UserID: "admin"}

	_, err := (*ShadowMCPReviewService)(nil).FileShadowMCPReview(t.Context(), principal, project, "https://mcp.example.test/server", "adding it", "")
	require.ErrorIs(t, err, ErrUnavailable)

	_, err = newTestShadowMCPReviewService(nil, nil).FileShadowMCPReview(t.Context(), principal, project, "https://mcp.example.test/server", "adding it", "")
	require.ErrorIs(t, err, ErrUnavailable)

	_, err = newTestShadowMCPReviewService(&recordingReviewRequests{}, nil).FileShadowMCPReview(t.Context(), principal, ResolvedProject{}, "https://mcp.example.test/server", "adding it", "")
	require.ErrorIs(t, err, ErrUnavailable)

	requests := &recordingReviewRequests{err: errors.New("queue down")}
	_, err = newTestShadowMCPReviewService(requests, nil).FileShadowMCPReview(t.Context(), principal, project, "https://mcp.example.test/server", "adding it", "")
	require.ErrorContains(t, err, "queue down")
}

func TestShadowMCPReviewRequiredErrorSatisfiesBothSentinels(t *testing.T) {
	t.Parallel()

	err := error(&ShadowMCPReviewRequiredError{Review: ShadowMCPReviewRequest{RequestID: "request-1", Explanation: "why"}, Cause: ErrShadowMCPReviewRequired})
	require.ErrorIs(t, err, ErrShadowMCPReviewRequired)
	require.ErrorIs(t, err, ErrDistributionBlockedPendingApproval)

	output, ok := shadowMCPReviewToolOutput(err)
	require.True(t, ok)
	require.Equal(t, "request-1", output.RequestID)
	require.Equal(t, "why", output.Explanation)

	_, ok = shadowMCPReviewToolOutput(ErrShadowMCPReviewRequired)
	require.False(t, ok, "a bare refusal with no filed request is not a review result")
	_, ok = shadowMCPReviewToolOutput(&ShadowMCPReviewRequiredError{})
	require.False(t, ok, "a review without a request id is not a filed review")
}
