package openrouter

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

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

type EnterpriseTrialConversionKeyState struct {
	KeyType        KeyType
	MonthlyCredits int64
	Disabled       bool
	DisableCauses  []string
}

type EnterpriseTrialConversionKeyChange struct {
	Exists  bool
	Changed bool
	Before  EnterpriseTrialConversionKeyState
	After   EnterpriseTrialConversionKeyState
}

func emptyEnterpriseTrialConversionKeyState() EnterpriseTrialConversionKeyState {
	return EnterpriseTrialConversionKeyState{KeyType: "", MonthlyCredits: 0, Disabled: false, DisableCauses: nil}
}

// PrepareEnterpriseTrialConversionKeyWithDB atomically prepares one classified
// local key for enterprise conversion. The caller owns the transaction and must
// already hold lifecycle and per-key advisory locks; this method acquires none
// and never contacts OpenRouter. Missing and deleted keys are safe no-ops.
func (o *OpenRouter) PrepareEnterpriseTrialConversionKeyWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, enterpriseFloor int64) (EnterpriseTrialConversionKeyChange, error) {
	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return EnterpriseTrialConversionKeyChange{}, fmt.Errorf("prepare OpenRouter API key for enterprise trial conversion: %w", err)
	}
	if enterpriseFloor < 0 {
		return EnterpriseTrialConversionKeyChange{}, errors.New("prepare OpenRouter API key for enterprise trial conversion: enterprise floor cannot be negative")
	}

	row, err := repo.New(db).PrepareEnterpriseTrialConversionKey(ctx, repo.PrepareEnterpriseTrialConversionKeyParams{
		OrganizationID:  orgID,
		KeyType:         string(keyType),
		EnterpriseFloor: enterpriseFloor,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return EnterpriseTrialConversionKeyChange{Exists: false, Changed: false, Before: emptyEnterpriseTrialConversionKeyState(), After: emptyEnterpriseTrialConversionKeyState()}, nil
	case err != nil:
		return EnterpriseTrialConversionKeyChange{}, fmt.Errorf("prepare OpenRouter API key for enterprise trial conversion: %w", err)
	case !row.Classified:
		return EnterpriseTrialConversionKeyChange{}, errors.New("prepare OpenRouter API key for enterprise trial conversion: disable causes are unclassified")
	}

	before := EnterpriseTrialConversionKeyState{
		KeyType: keyType, MonthlyCredits: row.BeforeMonthlyCredits, Disabled: row.BeforeDisabled, DisableCauses: row.BeforeDisableCauses,
	}
	after := EnterpriseTrialConversionKeyState{
		KeyType: keyType, MonthlyCredits: row.AfterMonthlyCredits.Int64, Disabled: row.AfterDisabled.Bool, DisableCauses: row.AfterDisableCauses,
	}
	return EnterpriseTrialConversionKeyChange{
		Exists:  true,
		Changed: before.MonthlyCredits != after.MonthlyCredits || before.Disabled != after.Disabled || !slices.Equal(before.DisableCauses, after.DisableCauses),
		Before:  before,
		After:   after,
	}, nil
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
