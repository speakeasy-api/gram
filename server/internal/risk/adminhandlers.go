package risk

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin_risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The adminRiskAnalysis handlers trigger and monitor ad-hoc risk analysis
// runs. This is a Speakeasy-operator surface, not a customer lever: no
// project/org RBAC grant exists for it, so each handler gates inline on the
// platform-admin flag. Every trigger is recorded in the target org's audit
// log so customers can see that their transcripts were re-scanned.

// requirePlatformAdmin extracts the auth context and enforces the
// platform-admin flag. The returned logger is pre-tagged with the actor.
func (s *Service) requirePlatformAdmin(ctx context.Context) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogUserID(authCtx.UserID))

	if !authCtx.IsAdmin {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "platform admin required").LogError(ctx, logger)
	}

	return authCtx, logger, nil
}

func (s *Service) Trigger(ctx context.Context, payload *gen.TriggerPayload) (*gen.AdhocRiskAnalysisStatus, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").LogError(ctx, logger)
	}
	policyID, err := uuid.Parse(payload.RiskPolicyID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid risk policy id").LogError(ctx, logger)
	}

	from, err := time.Parse(time.RFC3339, payload.From)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid from timestamp").LogError(ctx, logger)
	}
	to := time.Now()
	if payload.To != nil {
		to, err = time.Parse(time.RFC3339, *payload.To)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid to timestamp").LogError(ctx, logger)
		}
	}
	if !from.Before(to) {
		return nil, oops.E(oops.CodeBadRequest, nil, "from must be before to").LogError(ctx, logger)
	}

	policy, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        policyID,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, logger)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading risk policy").LogError(ctx, logger)
	}
	if !policy.Enabled {
		return nil, oops.E(oops.CodeBadRequest, nil, "risk policy is disabled").LogError(ctx, logger)
	}

	status, err := s.adhocClient.Trigger(ctx, AdhocAnalysisTriggerArgs{
		ProjectID:    projectID,
		RiskPolicyID: policyID,
		From:         from,
		To:           to,
	})
	if errors.Is(err, ErrAdhocAnalysisAlreadyRunning) {
		return nil, oops.E(oops.CodeConflict, err, "an ad-hoc risk analysis run is already in flight for this project").LogError(ctx, logger)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting ad-hoc risk analysis").LogError(ctx, logger)
	}

	if err := s.audit.LogRiskPolicyTrigger(ctx, s.db, audit.LogRiskPolicyTriggerEvent{
		OrganizationID:   policy.OrganizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskPolicyID:     policyID,
		RiskPolicyName:   policy.Name,
	}); err != nil {
		logger.ErrorContext(ctx, "audit adhoc risk analysis trigger", attr.SlogError(err))
	}

	return adhocStatusToGen(status), nil
}

func (s *Service) Status(ctx context.Context, payload *gen.StatusPayload) (*gen.AdhocRiskAnalysisStatus, error) {
	_, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").LogError(ctx, logger)
	}

	status, err := s.adhocClient.Status(ctx, projectID)
	// "No run yet" is a routine state the dashboard polls for, not an error.
	if errors.Is(err, ErrAdhocAnalysisNotFound) {
		return &gen.AdhocRiskAnalysisStatus{
			WorkflowID: "",
			Status:     "none",
			StartedAt:  nil,
			ClosedAt:   nil,
			Progress:   nil,
		}, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error fetching ad-hoc risk analysis status").LogError(ctx, logger)
	}

	return adhocStatusToGen(status), nil
}

func adhocStatusToGen(status *AdhocAnalysisStatus) *gen.AdhocRiskAnalysisStatus {
	out := &gen.AdhocRiskAnalysisStatus{
		WorkflowID: status.WorkflowID,
		Status:     status.Status,
		StartedAt:  nil,
		ClosedAt:   nil,
		Progress:   nil,
	}
	if status.StartedAt != nil {
		v := status.StartedAt.Format(time.RFC3339)
		out.StartedAt = &v
	}
	if status.ClosedAt != nil {
		v := status.ClosedAt.Format(time.RFC3339)
		out.ClosedAt = &v
	}
	if status.Progress != nil {
		out.Progress = &gen.AdhocRiskAnalysisProgress{
			TotalMessages:      status.Progress.TotalMessages,
			DispatchedMessages: status.Progress.DispatchedMessages,
			ProcessedMessages:  status.Progress.ProcessedMessages,
			Findings:           status.Progress.Findings,
			BatchesCompleted:   status.Progress.BatchesCompleted,
			BatchesFailed:      status.Progress.BatchesFailed,
			Policies:           status.Progress.Policies,
		}
	}
	return out
}
