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
// the agent. The block page is opened from the link an agent embeds in its deny
// message, so access is intentionally NOT gated on org-admin — the person whose
// agent was blocked is usually a regular member. Access is scoped to org
// MEMBERSHIP (see the query below), so a block is readable by any signed-in
// member of the organization that owns it, regardless of their active org.
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
		ID:           blockID,
		ViewerUserID: authCtx.UserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load block").LogError(ctx, s.logger)
	}

	if err := s.authorizeBlockView(ctx, authCtx, row.ProjectID, row.UserID); err != nil {
		return nil, err
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

	// Authorize against the same owner-or-project-admin rule as the read path
	// before mutating: load the block (which also enforces the org-membership
	// floor) and run the access check on it.
	block, err := s.repo.GetToolCallBlock(ctx, repo.GetToolCallBlockParams{
		ID:           blockID,
		ViewerUserID: authCtx.UserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load block").LogError(ctx, s.logger)
	}
	if err := s.authorizeBlockView(ctx, authCtx, block.ProjectID, block.UserID); err != nil {
		return nil, err
	}

	row, err := s.repo.UpdateToolCallBlockFeedback(ctx, repo.UpdateToolCallBlockFeedbackParams{
		Feedback:       conv.ToPGTextEmpty(payload.Sentiment),
		FeedbackUserID: conv.ToPGTextEmpty(authCtx.UserID),
		ID:             blockID,
		ViewerUserID:   authCtx.UserID,
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

// authorizeBlockAccess requires an authenticated session but, unlike the rest
// of the risk surface, does NOT require org-admin: the durable block page is
// meant to be opened by the end user whose agent was blocked, who is typically
// a regular org member. The block queries scope access to membership of the
// block's owning organization, so a block is only readable by a signed-in
// member of that org (regardless of which org is currently active).
func (s *Service) authorizeBlockAccess(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	return authCtx, nil
}

// authorizeBlockView tightens block access beyond bare org membership using the
// owning user recorded on the block at deny time. The owner may always view
// their own block; any other viewer must hold project-admin (project:write) on
// the block's project. When no owner was recorded (empty user_id — e.g. the user
// could not be resolved at deny time), access falls back to the org membership
// already enforced by the block queries.
//
// When RBAC is disabled for the org the project:write check is a no-op (allow),
// which is consistent with how the rest of the platform degrades to
// membership-level access without RBAC.
func (s *Service) authorizeBlockView(ctx context.Context, authCtx *contextvalues.AuthContext, projectID uuid.UUID, blockUserID string) error {
	if strings.TrimSpace(blockUserID) == "" {
		return nil
	}
	if blockUserID == authCtx.UserID {
		return nil
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeProjectWrite,
		ResourceKind: "",
		ResourceID:   projectID.String(),
		Dimensions:   nil,
	}); err != nil {
		return err
	}
	return nil
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
