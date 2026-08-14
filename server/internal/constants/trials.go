package constants

// MinTrialExtensionDays is the smallest extension an operator can apply. Zero
// would report success while moving nothing but updated_at, and a negative
// value would shorten the trial through an endpoint named extend.
const MinTrialExtensionDays = 1

// MaxTrialExtensionDays caps a single extension at a year, because a trial that
// runs longer than that is a contract rather than a trial, and the bound also
// keeps the ends_at arithmetic far away from what timestamptz can hold.
const MaxTrialExtensionDays = 365

// MinTrialRearmDays and MaxTrialRearmDays bound the runway a re-arm grants. A
// re-arm counts its days from now rather than from the trial's old end date, so
// the two operations bound different arithmetic, but the reasons for both ends
// of the range are the extension's reasons unchanged. They are written as
// aliases so a future divergence is a deliberate edit rather than a silent one.
const (
	MinTrialRearmDays = MinTrialExtensionDays
	MaxTrialRearmDays = MaxTrialExtensionDays
)
