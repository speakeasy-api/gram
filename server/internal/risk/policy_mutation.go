package risk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

type policyMutationAuditor struct {
	logger *audit.Logger
}

// NewPolicyMutationCore composes the shared risk policy command for non-Goa
// adapters. It preserves the same audit, approval, URL-grant, signal, and cache
// dependencies used by the dashboard service.
func NewPolicyMutationCore(db *pgxpool.Pool, auditLogger *audit.Logger, approvals policycore.ApprovalCoordinator, signaler policycore.PolicySignaler, cacheInvalidator policycore.PolicyCacheInvalidator) *policycore.Core {
	return policycore.New(db, policycore.MutationDependencies{
		Transactor:       db,
		Auditor:          policyMutationAuditor{logger: auditLogger},
		Approvals:        approvals,
		ReconcileURLs:    policycore.ReconcilePolicyURLs(policybypass.ReconcilePolicyURLs),
		Signaler:         signaler,
		CacheInvalidator: cacheInvalidator,
	})
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
		SnapshotBefore:   policyToGoa(policycore.AuditSnapshot(event.Before)),
		SnapshotAfter:    policyToGoa(policycore.AuditSnapshot(event.After)),
	}); err != nil {
		return fmt.Errorf("log policy update audit: %w", err)
	}
	return nil
}

func (s *Service) policyMutationError(ctx context.Context, err error) error {
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		return shareable
	}

	if conflict, ok := errors.AsType[*policycore.DecisionConflictError](err); ok {
		return oops.E(
			oops.CodeConflict,
			nil,
			"This change contradicts recorded access decisions for %s. Confirm superseding those decisions to proceed.",
			strings.Join(conflict.Targets, ", "),
		)
	}

	if _, ok := errors.AsType[*policycore.StalePolicyError](err); ok {
		return oops.E(oops.CodeConflict, err, "risk policy changed during update; reload and retry")
	}

	if blockingConflict, ok := errors.AsType[*policycore.BlockingPolicyConflictError](err); ok {
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

	if mutation, ok := errors.AsType[*policycore.MutationError](err); ok {
		return oops.E(oops.CodeUnexpected, mutation.Cause, "%s", mutation.Message).LogError(ctx, s.logger)
	}
	return oops.E(oops.CodeUnexpected, err, "mutate risk policy").LogError(ctx, s.logger)
}
