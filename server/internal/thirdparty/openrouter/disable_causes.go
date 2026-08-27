package openrouter

import "fmt"

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

type DisableCauseChange struct {
	CauseChanged     bool
	KeyAccessChanged bool
}

func unchangedDisableCauseChange() DisableCauseChange {
	return DisableCauseChange{CauseChanged: false, KeyAccessChanged: false}
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
