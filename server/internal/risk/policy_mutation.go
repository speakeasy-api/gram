package risk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

type policyMutationAuditor struct {
	logger *audit.Logger
}

func (a policyMutationAuditor) LogPolicyCreate(ctx context.Context, db repo.DBTX, event policycore.CreateAuditEvent) error {
	if err := a.logger.LogRiskPolicyCreate(ctx, db, audit.LogRiskPolicyCreateEvent{
		OrganizationID:   event.OrganizationID,
		ProjectID:        event.ProjectID,
		Actor:            event.Actor.Principal,
		ActorDisplayName: event.Actor.DisplayName,
		ActorSlug:        event.Actor.Slug,
		RiskPolicyID:     event.Policy.ID,
		RiskPolicyName:   event.Policy.Name,
	}); err != nil {
		return fmt.Errorf("log policy create audit: %w", err)
	}
	return nil
}

func (a policyMutationAuditor) LogPolicyUpdate(ctx context.Context, db repo.DBTX, event policycore.UpdateAuditEvent) error {
	if err := a.logger.LogRiskPolicyUpdate(ctx, db, audit.LogRiskPolicyUpdateEvent{
		OrganizationID:   event.OrganizationID,
		ProjectID:        event.ProjectID,
		Actor:            event.Actor.Principal,
		ActorDisplayName: event.Actor.DisplayName,
		ActorSlug:        event.Actor.Slug,
		RiskPolicyID:     event.After.ID,
		RiskPolicyName:   event.After.Name,
		SnapshotBefore:   policyToGoa(event.Before),
		SnapshotAfter:    policyToGoa(event.After),
	}); err != nil {
		return fmt.Errorf("log policy update audit: %w", err)
	}
	return nil
}

func (s *Service) policyMutationError(ctx context.Context, err error) error {
	var shareable *oops.ShareableError
	if errors.As(err, &shareable) {
		return shareable
	}

	var conflict *policycore.DecisionConflictError
	if errors.As(err, &conflict) {
		return oops.E(
			oops.CodeConflict,
			nil,
			"This change contradicts recorded access decisions for %s. Confirm superseding those decisions to proceed.",
			strings.Join(conflict.Targets, ", "),
		)
	}

	var stale *policycore.StalePolicyError
	if errors.As(err, &stale) {
		return oops.E(oops.CodeConflict, err, "risk policy changed during update; reload and retry")
	}

	var blockingConflict *policycore.BlockingPolicyConflictError
	if errors.As(err, &blockingConflict) {
		return oops.E(
			oops.CodeConflict,
			nil,
			"project already has an enabled shadow mcp blocking policy %q; disable or delete it first",
			blockingConflict.PolicyName,
		)
	}
	if errors.Is(err, policycore.ErrLoadPolicy) {
		return oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
	}

	var mutation *policycore.MutationError
	if errors.As(err, &mutation) {
		return oops.E(oops.CodeUnexpected, mutation.Cause, "%s", mutation.Message).LogError(ctx, s.logger)
	}
	return oops.E(oops.CodeUnexpected, err, "mutate risk policy").LogError(ctx, s.logger)
}
