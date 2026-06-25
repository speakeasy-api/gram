package risk

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// GetRiskBlock returns a durable tool call block by its ID. Blocks are recorded
// at hook-time deny into tool_call_blocks, carrying the exact reason shown to
// the agent. Access is scoped to the viewer's active organization and gated on
// the same org-admin scope as the rest of the risk surface, so the plain-UUID
// URL is not usable by outsiders.
func (s *Service) GetRiskBlock(ctx context.Context, payload *gen.GetRiskBlockPayload) (*gen.RiskBlock, error) {
	authCtx, err := s.authorizeBlockAccess(ctx)
	if err != nil {
		return nil, err
	}

	blockID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid block id")
	}

	row, err := s.repo.GetToolCallBlock(ctx, repo.GetToolCallBlockParams{
		ID:             blockID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load block").LogError(ctx, s.logger)
	}

	return &gen.RiskBlock{
		ID:         row.ID.String(),
		ProjectID:  row.ProjectID.String(),
		Reason:     blockReason(row.Reason),
		PolicyName: row.PolicyName.String,
		ToolName:   conv.FromPGText[string](row.ToolName),
		CreatedAt:  row.CreatedAt.Time.Format(time.RFC3339),
		Feedback:   blockFeedbackSentiment(row.Feedback),
	}, nil
}

// SubmitRiskBlockFeedback records 👍/👎 feedback on a tool call block and returns
// the refreshed block so the page reflects the vote.
func (s *Service) SubmitRiskBlockFeedback(ctx context.Context, payload *gen.SubmitRiskBlockFeedbackPayload) (*gen.RiskBlock, error) {
	authCtx, err := s.authorizeBlockAccess(ctx)
	if err != nil {
		return nil, err
	}

	switch payload.Sentiment {
	case "up", "down":
	default:
		return nil, oops.E(oops.CodeInvalid, nil, "invalid feedback sentiment")
	}

	blockID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid block id")
	}

	row, err := s.repo.UpdateToolCallBlockFeedback(ctx, repo.UpdateToolCallBlockFeedbackParams{
		Feedback:       conv.ToPGTextEmpty(payload.Sentiment),
		FeedbackUserID: conv.ToPGTextEmpty(authCtx.UserID),
		ID:             blockID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "record block feedback").LogError(ctx, s.logger)
	}

	return &gen.RiskBlock{
		ID:         row.ID.String(),
		ProjectID:  row.ProjectID.String(),
		Reason:     blockReason(row.Reason),
		PolicyName: row.PolicyName,
		ToolName:   conv.FromPGText[string](row.ToolName),
		CreatedAt:  row.CreatedAt.Time.Format(time.RFC3339),
		Feedback:   blockFeedbackSentiment(row.Feedback),
	}, nil
}

// authorizeBlockAccess requires a session whose active organization can read
// the risk surface, matching the org-admin gate on Risk Events.
func (s *Service) authorizeBlockAccess(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	return authCtx, nil
}

func blockReason(reason string) string {
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		return trimmed
	}
	return "This tool call was blocked by a Speakeasy risk policy."
}

// blockFeedbackSentiment normalizes the stored feedback to the API sentiment
// ("up"/"down"), or nil when none has been recorded.
func blockFeedbackSentiment(feedback pgtype.Text) *string {
	switch feedback.String {
	case "up", "down":
		return conv.PtrEmpty(feedback.String)
	}
	return nil
}
