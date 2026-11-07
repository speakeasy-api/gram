package catalogpromotion

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
)

type fakeReviewer struct {
	found    *mcpapproval.StaffReview
	off      bool
	findErr  error
	startErr error
	started  []string
	actors   []string
}

func (f *fakeReviewer) Available(context.Context) bool { return !f.off }

func (f *fakeReviewer) Find(context.Context, string) (*mcpapproval.StaffReview, error) {
	return f.found, f.findErr
}

func (f *fakeReviewer) Start(_ context.Context, serverURL, actor string, _ *string) (*mcpapproval.StaffReview, error) {
	f.started = append(f.started, serverURL)
	f.actors = append(f.actors, actor)
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &mcpapproval.StaffReview{RequestID: "synthetic-review", Evidence: nil, EvidenceCollectedAt: "", Research: nil}, nil
}

func TestStartReviewUsesTheSharedReviewer(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	reviewer := &fakeReviewer{}
	f.service.SetReviewer(reviewer)

	review, err := f.service.StartReview(t.Context(), f.target, "synthetic-staff", nil)

	require.NoError(t, err)
	require.Equal(t, "synthetic-review", review.RequestID)
	require.Equal(t, []string{"https://catalog.example.com/mcp"}, reviewer.started)
	require.Equal(t, []string{"synthetic-staff"}, reviewer.actors)
}

func TestStartReviewReportsDisabledRollout(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	f.service.SetReviewer(&fakeReviewer{startErr: mcpapproval.ErrStaffReviewDisabled})

	_, err := f.service.StartReview(t.Context(), f.target, "synthetic-staff", nil)

	require.ErrorIs(t, err, ErrReviewDisabled)
}

func TestReviewRefusedWithoutReviewer(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)

	_, err := f.service.StartReview(t.Context(), f.target, "synthetic-staff", nil)

	require.ErrorIs(t, err, ErrUnavailable)
}

func TestReviewableEndpointRefusesTenantShapedURLs(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]bool{
		"https://mcp.example.com/mcp":             true,
		"https://mcp.example.com/sse":             true,
		"https://mcp.example.com/some-tenant/mcp": false,
		"https://mcp.example.com/mcp?token=abc":   false,
		"https://mcp.example.com/t/0190b3c8/tool": false,
		"http://mcp.example.com/mcp":              false,
	} {
		_, ok := reviewableEndpoint(repo.GetSourceRow{Url: raw})
		require.Equal(t, want, ok, raw)
	}
}

func TestInspectCandidateAddsAccountUsageAndReview(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	f.service.SetReviewer(&fakeReviewer{found: &mcpapproval.StaffReview{RequestID: "existing-review", Evidence: nil, EvidenceCollectedAt: "", Research: nil}})

	candidate, err := f.service.InspectCandidate(t.Context(), f.target)

	require.NoError(t, err)
	require.Equal(t, ReviewAvailable, candidate.ReviewAvailability)
	require.Equal(t, "existing-review", candidate.Review.RequestID)
	require.NotNil(t, candidate.AccountUsage)
	require.Equal(t, int64(3), candidate.AccountUsage.DistinctUsers)
	require.Equal(t, []string{"synthetic-client"}, candidate.AccountUsage.Clients)
	require.Equal(t, "search", candidate.AccountUsage.Tools[0].Name)
}

func TestInspectCandidateSurvivesReviewLookupFailure(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	f.service.SetReviewer(&fakeReviewer{findErr: errors.New("synthetic outage")})

	candidate, err := f.service.InspectCandidate(t.Context(), f.target)

	require.NoError(t, err)
	require.Nil(t, candidate.Review)
	require.NotNil(t, candidate.AccountUsage)
}

func TestInspectCandidateHonoursLoggingConsent(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	f.service.features = fakePromotionFeatures{enabled: false}

	candidate, err := f.service.InspectCandidate(t.Context(), f.target)

	require.NoError(t, err)
	require.Nil(t, candidate.AccountUsage)
}

// Staff activity belongs to Speakeasy's own organization, never the source account.
func TestPromotionAuditLandsInHomeOrganization(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)

	_, err := f.promote(t)
	require.NoError(t, err)

	event, err := audittest.LatestAuditLogByAction(t.Context(), f.db, audit.ActionMcpServerPromoteCatalogue)
	require.NoError(t, err)
	require.Equal(t, syntheticHome.OrganizationID, event.OrganizationID)
	require.Equal(t, syntheticHome.ProjectID, event.ProjectID.UUID)
	require.NotEqual(t, f.target.OrganizationID, event.OrganizationID)
}

func TestInspectCandidateReportsDisabledReviews(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)

	candidate, err := f.service.InspectCandidate(t.Context(), f.target)
	require.NoError(t, err)
	require.Equal(t, ReviewDisabled, candidate.ReviewAvailability)

	f.service.SetReviewer(&fakeReviewer{off: true})
	candidate, err = f.service.InspectCandidate(t.Context(), f.target)
	require.NoError(t, err)
	require.Equal(t, ReviewDisabled, candidate.ReviewAvailability)
}
