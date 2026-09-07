package openrouterdisablecauses

import "fmt"

const (
	CauseAdminLock       = "admin_lock"
	CauseTrialDemotion   = "trial_demotion"
	CauseBillingInactive = "billing_inactive"

	AmbiguousNoProvenance      = "no_provenance"
	AmbiguousTrialProjection   = "trial_projection"
	AmbiguousBillingProjection = "billing_projection"
	AmbiguousAdminAudit        = "admin_audit"
)

type TrialState uint8

const (
	TrialNone TrialState = iota
	TrialDemoted
	TrialContradictory
)

type BillingState uint8

const (
	BillingIrrelevant BillingState = iota
	BillingActive
	BillingInactive
	BillingInconsistent
)

type AdminState uint8

const (
	AdminNone AdminState = iota
	AdminEnabled
	AdminDisabled
	AdminMalformed
)

type Projection struct {
	LegacyDisabled bool
	Trial          TrialState
	Billing        BillingState
	Admin          AdminState
}

type Classification struct {
	Classified      bool
	Causes          []string
	AmbiguousReason string
}

func Classify(p Projection) Classification {
	if !p.LegacyDisabled {
		return Classification{Classified: true, Causes: []string{}, AmbiguousReason: ""}
	}

	switch {
	case p.Trial == TrialContradictory:
		return Classification{Classified: false, Causes: nil, AmbiguousReason: AmbiguousTrialProjection}
	case p.Billing == BillingInconsistent:
		return Classification{Classified: false, Causes: nil, AmbiguousReason: AmbiguousBillingProjection}
	case p.Admin == AdminMalformed:
		return Classification{Classified: false, Causes: nil, AmbiguousReason: AmbiguousAdminAudit}
	}

	causes := make([]string, 0, 3)
	if p.Admin == AdminDisabled {
		causes = append(causes, CauseAdminLock)
	}
	if p.Trial == TrialDemoted {
		causes = append(causes, CauseTrialDemotion)
	}
	if p.Billing == BillingInactive {
		causes = append(causes, CauseBillingInactive)
	}
	if len(causes) == 0 {
		return Classification{Classified: false, Causes: nil, AmbiguousReason: AmbiguousNoProvenance}
	}

	return Classification{Classified: true, Causes: causes, AmbiguousReason: ""}
}

func CanonicalizeCauses(causes []string) ([]string, error) {
	present := make(map[string]bool, len(causes))
	for _, cause := range causes {
		switch cause {
		case CauseAdminLock, CauseTrialDemotion, CauseBillingInactive:
			present[cause] = true
		default:
			return nil, fmt.Errorf("unknown OpenRouter disable cause %q", cause)
		}
	}

	ordered := make([]string, 0, len(present))
	for _, cause := range []string{CauseAdminLock, CauseTrialDemotion, CauseBillingInactive} {
		if present[cause] {
			ordered = append(ordered, cause)
		}
	}
	return ordered, nil
}
