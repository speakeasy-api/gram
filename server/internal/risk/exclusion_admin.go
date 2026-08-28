package risk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/exclusioncore"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

type exclusionMutationAuditor struct {
	logger *audit.Logger
}

func (a exclusionMutationAuditor) LogExclusionCreate(ctx context.Context, db repo.DBTX, event exclusioncore.CreateAuditEvent) error {
	if err := a.logger.LogRiskExclusionCreate(ctx, db, audit.LogRiskExclusionCreateEvent{
		OrganizationID:   event.OrganizationID,
		ProjectID:        event.ProjectID,
		Actor:            event.Actor.Principal,
		ActorDisplayName: event.Actor.DisplayName,
		ActorSlug:        event.Actor.Slug,
		RiskExclusionID:  event.Exclusion.ID,
		DisplayName:      event.DisplayName,
	}); err != nil {
		return fmt.Errorf("log exclusion create audit: %w", err)
	}
	return nil
}

func (a exclusionMutationAuditor) LogExclusionUpdate(ctx context.Context, db repo.DBTX, event exclusioncore.UpdateAuditEvent) error {
	if err := a.logger.LogRiskExclusionUpdate(ctx, db, audit.LogRiskExclusionUpdateEvent{
		OrganizationID:   event.OrganizationID,
		ProjectID:        event.ProjectID,
		Actor:            event.Actor.Principal,
		ActorDisplayName: event.Actor.DisplayName,
		ActorSlug:        event.Actor.Slug,
		RiskExclusionID:  event.After.ID,
		DisplayName:      event.DisplayName,
		SnapshotBefore:   exclusionToType(event.Before),
		SnapshotAfter:    exclusionToType(event.After),
	}); err != nil {
		return fmt.Errorf("log exclusion update audit: %w", err)
	}
	return nil
}

func (a exclusionMutationAuditor) LogExclusionDelete(ctx context.Context, db repo.DBTX, event exclusioncore.DeleteAuditEvent) error {
	if err := a.logger.LogRiskExclusionDelete(ctx, db, audit.LogRiskExclusionDeleteEvent{
		OrganizationID:   event.OrganizationID,
		ProjectID:        event.ProjectID,
		Actor:            event.Actor.Principal,
		ActorDisplayName: event.Actor.DisplayName,
		ActorSlug:        event.Actor.Slug,
		RiskExclusionID:  event.Exclusion.ID,
		DisplayName:      event.DisplayName,
	}); err != nil {
		return fmt.Errorf("log exclusion delete audit: %w", err)
	}
	return nil
}

func exclusionToType(exclusion exclusioncore.Exclusion) *types.RiskExclusion {
	var policyID *string
	if exclusion.RiskPolicyID.Valid {
		value := exclusion.RiskPolicyID.UUID.String()
		policyID = &value
	}
	return &types.RiskExclusion{
		ID:           exclusion.ID.String(),
		ProjectID:    exclusion.ProjectID.String(),
		RiskPolicyID: policyID,
		MatchType:    exclusion.MatchType,
		MatchValue:   exclusion.MatchValue,
		RuleIDFilter: exclusion.RuleIDFilter,
		SourceFilter: exclusion.SourceFilter,
		Enabled:      exclusion.Enabled,
		CreatedAt:    exclusion.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    exclusion.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *Service) exclusionError(ctx context.Context, err error) error {
	var validation *exclusioncore.ValidationError
	if errors.As(err, &validation) {
		return oops.E(oops.CodeInvalid, validation.Cause, "%s", validation.Message)
	}
	var regexLimit *exclusioncore.RegexLimitError
	if errors.As(err, &regexLimit) {
		return oops.E(oops.CodeInvalid, nil, "%s", regexLimit)
	}
	if errors.Is(err, exclusioncore.ErrPolicyNotFound) {
		return oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
	}
	if errors.Is(err, exclusioncore.ErrExclusionNotFound) {
		return oops.E(oops.CodeNotFound, err, "risk exclusion not found").LogError(ctx, s.logger)
	}
	var mutation *exclusioncore.MutationError
	if errors.As(err, &mutation) {
		return oops.E(oops.CodeUnexpected, mutation.Cause, "%s", mutation.Message).LogError(ctx, s.logger)
	}
	return oops.E(oops.CodeUnexpected, err, "administer risk exclusion").LogError(ctx, s.logger)
}

func newExclusionAfterCommit(logger *slog.Logger, reconciler RiskExclusionReconciler) exclusioncore.AfterCommit {
	if reconciler == nil {
		return nil
	}
	return func(ctx context.Context, projectID, exclusionID uuid.UUID) {
		reconcileBaseCtx := context.WithoutCancel(ctx)
		go func() {
			reconcileCtx, cancel := context.WithTimeout(reconcileBaseCtx, 10*time.Second)
			defer cancel()
			if err := reconciler.Reconcile(reconcileCtx, projectID, exclusionID); err != nil {
				logger.ErrorContext(reconcileCtx, "trigger risk exclusion reconcile",
					attr.SlogError(err),
					attr.SlogProjectID(projectID.String()),
				)
			}
		}()
	}
}
