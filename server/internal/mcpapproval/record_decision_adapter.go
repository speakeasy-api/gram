package mcpapproval

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// RecordDecision adapts the dashboard transport to the shared transaction-aware
// decision command while preserving the existing empty-approval-means-Everyone
// behavior. Platform MCP applies its stricter explicit-audience contract before
// reaching the same command.
func (s *Service) RecordDecision(ctx context.Context, payload *gen.RecordDecisionPayload) (*gen.ApprovalDecision, error) {
	projectID, organizationID, err := s.project(ctx)
	if err != nil {
		return nil, err
	}
	if payload.Decision != decisionApproved && payload.Decision != decisionDenied {
		return nil, oops.E(oops.CodeBadRequest, nil, "decision must be approved or denied").LogError(ctx, s.logger)
	}
	rationale := strings.TrimSpace(payload.Rationale)
	if rationale == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "a rationale is required").LogError(ctx, s.logger)
	}
	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid approval request id").LogError(ctx, s.logger)
	}
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	var reportID uuid.NullUUID
	if payload.ResearchReportID != nil {
		parsed, parseErr := uuid.Parse(*payload.ResearchReportID)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid research report id").LogError(ctx, s.logger)
		}
		reportID = uuid.NullUUID{UUID: parsed, Valid: true}
	}
	granted := payload.GrantedPrincipalUrns
	if payload.Decision == decisionDenied {
		granted = []string{}
	} else if len(granted) == 0 {
		granted = []string{authz.AllUsersPrincipal().String()}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording decision").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	decision, err := s.DecideInTransaction(ctx, tx, DecisionCommandInput{
		OrganizationID: organizationID, ProjectID: projectID, RequestID: requestID,
		Decision: payload.Decision, Rationale: rationale, GrantedPrincipalURNs: granted,
		ResearchReportID: reportID, ActorUserID: authCtx.UserID, ActorEmail: authCtx.Email, ValidateLocked: nil,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxClosed) {
			return nil, oops.E(oops.CodeUnexpected, err, "decision transaction closed").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error recording decision").LogError(ctx, s.logger)
	}
	return decision, nil
}
