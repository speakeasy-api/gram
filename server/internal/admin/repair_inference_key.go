package admin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func (s *Service) RepairInferenceKey(ctx context.Context, p *gen.RepairInferenceKeyPayload) (*gen.AdminInferenceKeyRepairResult, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if p == nil || p.Confirmation != "I know what I'm doing" || strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 2000 || len(p.RemoveCauses) == 0 || len(p.RemoveCauses) > 3 {
		return nil, oops.E(oops.CodeBadRequest, nil, "Explicit causes, exact confirmation, and a reason (at most 2000 bytes) are required")
	}
	kt := openrouter.KeyType(p.KeyType)
	if p.KeyType == "" {
		return nil, oops.C(oops.CodeBadRequest)
	}
	if err := kt.Validate(); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "Invalid key type")
	}
	seen := map[string]bool{}
	for _, cause := range p.RemoveCauses {
		if err := openrouter.DisableCause(cause).Validate(); err != nil || seen[cause] {
			return nil, oops.E(oops.CodeBadRequest, err, "Select unique known causes only; investigate unknown causes with engineering")
		}
		seen[cause] = true
	}
	id, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin inference key repair: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err = trialsrepo.New(tx).LockTrialLifecycle(ctx, id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lock trial lifecycle: %w", err)
	}
	if err = openrouter.AcquireAPIKeyBillingTransactionLock(ctx, tx, id, kt); err != nil {
		return nil, fmt.Errorf("lock key billing: %w", err)
	}
	if err = openrouter.AcquireAPIKeyProvisioningTransactionLock(ctx, tx, id, kt); err != nil {
		return nil, fmt.Errorf("lock key provisioning: %w", err)
	}
	bq := usagerepo.New(tx)
	if err = bq.LockBillingMetadataOrganization(ctx, id); err != nil {
		return nil, fmt.Errorf("lock billing metadata organization: %w", err)
	}
	q := repo.New(tx)
	if _, err = q.LockOrganizationMetadata(ctx, id); err != nil {
		return nil, fmt.Errorf("lock organization metadata: %w", err)
	}
	if _, err = bq.LockBillingMetadata(ctx, id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lock billing metadata: %w", err)
	}
	org, err := q.AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: id, AllowSlug: false})
	if err != nil {
		return nil, fmt.Errorf("get organization: %w", err)
	}
	if slices.Contains(p.RemoveCauses, string(openrouter.DisableCauseBillingInactive)) {
		if err = s.requireExistingBillingEligibility(ctx, org); err != nil {
			return nil, err
		}
	}
	if slices.Contains(p.RemoveCauses, string(openrouter.DisableCauseTrialDemotion)) {
		if err = s.requireTrialDemotionRepairEligibility(ctx, org); err != nil {
			return nil, err
		}
	}
	change, err := new(openrouter.OpenRouter).PrepareAdminKeyPolicyWithDB(ctx, tx, id, kt, openrouter.AdminKeyPolicy{RemoveCauses: p.RemoveCauses, MonthlyCredits: nil})
	if errors.Is(err, openrouter.ErrAPIKeyDisableCausesUnclassified) {
		return nil, oops.E(oops.CodeConflict, err, "Unclassified key requires engineering investigation")
	}
	if err != nil {
		return nil, fmt.Errorf("prepare inference key repair: %w", err)
	}
	if !change.Exists {
		return nil, oops.C(oops.CodeNotFound)
	}
	actor, display, _ := adminActor(ctx)
	if err = s.audit.LogOrganizationInferenceKeyRepaired(ctx, tx, audit.LogOrganizationInferenceKeyRepairedEvent{OrganizationID: id, Actor: actor, ActorDisplayName: display, Reason: strings.TrimSpace(p.Reason), RemoveCauses: p.RemoveCauses, Before: inferenceKeyAuditSnapshot(change.Before), After: inferenceKeyAuditSnapshot(change.After)}); err != nil {
		return nil, fmt.Errorf("audit inference key repair: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit inference key repair: %w", err)
	}
	key := &gen.AdminInferenceKeyRepairState{KeyType: p.KeyType, MonthlyCredits: change.After.MonthlyCredits, Disabled: change.After.Disabled, DisableCauses: change.After.DisableCauses, DisableCausesClassified: true, CauseDiagnostics: nil}
	key.CauseDiagnostics = s.inferenceKeyCauseDiagnostics(ctx, org, key.DisableCauses, true)
	return &gen.AdminInferenceKeyRepairResult{Key: key, ReconciliationPending: true}, nil
}

// Provider errors deliberately become bounded policy explanations, never read failures.
func (s *Service) inferenceKeyCauseDiagnostics(ctx context.Context, org repo.AdminGetOrganizationRow, causes []string, classified bool) []*gen.AdminInferenceKeyCause {
	result := make([]*gen.AdminInferenceKeyCause, 0, len(causes))
	for _, cause := range causes {
		d := &gen.AdminInferenceKeyCause{Cause: cause, Removable: true, Description: "", BlockedReason: nil}
		switch openrouter.DisableCause(cause) {
		case openrouter.DisableCauseAdminLock:
			d.Description = "Explicit staff lock."
		case openrouter.DisableCauseTrialDemotion:
			d.Description = "Trial demotion disabled this key."
			if err := s.requireTrialDemotionRepairEligibility(ctx, org); err != nil {
				d.Removable = false
				d.BlockedReason = conv.PtrEmpty("Verified PAYG billing or a non-trial enterprise entitlement is required. Use the account lifecycle workflow first.")
			}
		case openrouter.DisableCauseBillingInactive:
			d.Description = "Billing is inactive or unverified."
			if err := s.requireExistingBillingEligibility(ctx, org); err != nil {
				d.Removable = false
				d.BlockedReason = conv.PtrEmpty("Live linked billing eligibility could not be verified. Repair billing before removing this cause.")
			}
		default:
			d.Description = "Unrecognized disable cause."
			d.Removable = false
			d.BlockedReason = conv.PtrEmpty("Investigate with engineering; unknown causes cannot be removed here.")
		}
		if !classified {
			d.Removable = false
			d.BlockedReason = conv.PtrEmpty("Unclassified key requires engineering investigation.")
		}
		result = append(result, d)
	}
	return result
}

func inferenceKeyAuditSnapshot(state openrouter.EnterpriseTrialConversionKeyState) audit.InferenceKeyPolicySnapshot {
	return audit.InferenceKeyPolicySnapshot{KeyType: string(state.KeyType), MonthlyCredits: state.MonthlyCredits, Disabled: state.Disabled, DisableCauses: state.DisableCauses}
}
