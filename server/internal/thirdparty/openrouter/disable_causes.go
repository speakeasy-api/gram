package openrouter

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

type DisableCause string

const (
	DisableCauseAdminLock       DisableCause = "admin_lock"
	DisableCauseTrialDemotion   DisableCause = "trial_demotion"
	DisableCauseBillingInactive DisableCause = "billing_inactive"
)

func (c DisableCause) Validate() error {
	switch c {
	case DisableCauseAdminLock, DisableCauseTrialDemotion, DisableCauseBillingInactive:
		return nil
	default:
		return fmt.Errorf("unknown OpenRouter disable cause %q", c)
	}
}

// DisableStateReconciler is the narrow idempotent boundary used by durable
// retry orchestration. Reconciliation always derives desired state from the
// committed local row; callers never pass key material or an upstream target.
type DisableStateReconciler interface {
	ReconcileAPIKeyDisabled(context.Context, string, KeyType) error
}

type DisableCauseChange struct {
	CauseChanged     bool
	KeyAccessChanged bool
}

func unchangedDisableCauseChange() DisableCauseChange {
	return DisableCauseChange{CauseChanged: false, KeyAccessChanged: false}
}

// AcquireAPIKeyBillingTransactionLock serializes a caller-owned business
// transaction with key replacement and upstream reconciliation. Callers that
// fan out must acquire these locks in AllKeyTypes order before reading key rows.
func AcquireAPIKeyBillingTransactionLock(ctx context.Context, db DBTX, orgID string, keyType KeyType) error {
	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return fmt.Errorf("acquire OpenRouter API key billing transaction lock: %w", err)
	}

	if err := repo.New(db).AcquireOpenRouterBillingLock(ctx, repo.AcquireOpenRouterBillingLockParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	}); err != nil {
		return fmt.Errorf("acquire OpenRouter %s key billing transaction lock: %w", keyType, err)
	}
	return nil
}

// EffectiveDisabled preserves legacy access for rows that have not been
// classified yet. Once disable_causes is non-NULL, the cause set is
// authoritative even if the rollout mirror has drifted.
func EffectiveDisabled(legacyDisabled bool, disableCauses []string) bool {
	if disableCauses == nil {
		return legacyDisabled
	}

	return len(disableCauses) > 0
}
