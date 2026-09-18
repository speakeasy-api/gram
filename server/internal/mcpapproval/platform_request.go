package mcpapproval

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// PlatformRequesterReviewInput identifies a review owned by the calling user.
// All four dimensions are required so an untrusted request ID cannot cross an
// organization, project, or requester boundary.
type PlatformRequesterReviewInput struct {
	OrganizationID string
	ProjectID      uuid.UUID
	UserID         string
	RequestID      uuid.UUID
}

// PlatformRequesterReview is the privacy-minimized state a requester may read.
// It omits requester lists, notes, evidence, research, decision rationale, and
// every admin-only queue detail.
type PlatformRequesterReview struct {
	RequestID        string `json:"request_id"`
	TargetKind       string `json:"target_kind"`
	Target           string `json:"target"`
	Status           string `json:"status"`
	StandingDecision string `json:"standing_decision,omitempty"`
	RequestedAt      string `json:"requested_at"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	NextAction       string `json:"next_action"`
}

// ReadPlatformRequesterReview returns one request only when the caller owns its
// requester row. Missing projects, requests, and ownership all return the same
// not-found response so the lookup reveals no other user's queue activity.
func (s *Service) ReadPlatformRequesterReview(ctx context.Context, input PlatformRequesterReviewInput) (PlatformRequesterReview, error) {
	if s == nil || s.db == nil {
		return PlatformRequesterReview{}, oops.E(oops.CodeUnavailable, nil, "MCP review requests are temporarily unavailable")
	}
	if strings.TrimSpace(input.OrganizationID) == "" || input.ProjectID == uuid.Nil || strings.TrimSpace(input.UserID) == "" || input.RequestID == uuid.Nil {
		return PlatformRequesterReview{}, oops.E(oops.CodeBadRequest, nil, "organization, project, user, and request are required")
	}
	if err := s.requireFeature(ctx, input.OrganizationID); err != nil {
		return PlatformRequesterReview{}, err
	}
	if _, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: input.ProjectID, OrganizationID: input.OrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PlatformRequesterReview{}, oops.E(oops.CodeNotFound, err, "approval request not found")
		}
		return PlatformRequesterReview{}, oops.E(oops.CodeUnexpected, err, "error reading approval project").LogError(ctx, s.logger)
	}

	row, err := repo.New(s.db).GetPlatformRequesterApprovalRequest(ctx, repo.GetPlatformRequesterApprovalRequestParams{
		ID: input.RequestID, OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, UserID: input.UserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PlatformRequesterReview{}, oops.E(oops.CodeNotFound, err, "approval request not found")
		}
		return PlatformRequesterReview{}, oops.E(oops.CodeUnexpected, err, "error reading approval request").LogError(ctx, s.logger)
	}

	status := newPlatformReviewSummary(row.Status).Status
	return PlatformRequesterReview{
		RequestID: row.ID.String(), TargetKind: row.TargetKind, Target: row.TargetRaw,
		Status: status, StandingDecision: row.StandingDecision,
		RequestedAt: conv.FromPGTimestamptz(row.RequestedAt), CreatedAt: conv.FromPGTimestamptz(row.CreatedAt), UpdatedAt: conv.FromPGTimestamptz(row.UpdatedAt),
		NextAction: platformRequesterNextAction(status, row.StandingDecision),
	}, nil
}

func platformRequesterNextAction(status, decision string) string {
	switch {
	case status == statusRequested, status == statusSuperseded:
		return "wait_for_review"
	case decision == decisionApproved:
		return "ask_administrator_to_grant_access"
	case decision == decisionDenied:
		return "contact_administrator"
	default:
		return "contact_administrator"
	}
}
