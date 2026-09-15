package constants

// MinTrialExtensionDays is the smallest extension an operator can apply. Zero
// would report success while moving nothing but updated_at, and a negative
// value would shorten the trial through an endpoint named extend.
const MinTrialExtensionDays = 1

// MaxTrialExtensionDays caps a single extension at a year, because a trial that
// runs longer than that is a contract rather than a trial, and the bound also
// keeps the ends_at arithmetic far away from what timestamptz can hold.
const MaxTrialExtensionDays = 365

// MinTrialRearmDays and MaxTrialRearmDays bound the runway a re-arm grants.
// Aliases, so a divergence from the extension bounds has to be a deliberate edit.
const (
	MinTrialRearmDays = MinTrialExtensionDays
	MaxTrialRearmDays = MaxTrialExtensionDays
)

// MinTrialStartDays and MaxTrialStartDays bound a newly granted trial. Aliases
// of the extension pair for the same reason the re-arm bounds are: a start that
// runs longer than a year is a contract, and the three writes should keep the
// same floor and ceiling until someone splits them on purpose.
const (
	MinTrialStartDays = MinTrialExtensionDays
	MaxTrialStartDays = MaxTrialExtensionDays
)
