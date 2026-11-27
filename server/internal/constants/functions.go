package constants

const (
	DefaultFunctionMemoryMiB = 1024
	MaxFunctionMemoryMiB     = 4096

	DefaultFunctionScale = 2
	MaxFunctionScale     = 5

	MaxFunctionToolTags = 40
)

// Bounds, in seconds, on how long one Speakeasy Function invocation may run.
// Staff set an organization's limit in the admin dashboard; an organization
// without one gets the default. Shared by the Goa design, which rejects values
// outside the range at the HTTP boundary, the admin service, which repeats the
// check, and the gateway, which applies the limit.
const (
	DefaultFunctionTimeoutSeconds = 60
	MinFunctionTimeoutSeconds     = 30
	MaxFunctionTimeoutSeconds     = 600
)

// IsFunctionTimeoutSeconds reports whether seconds is a limit staff may set.
func IsFunctionTimeoutSeconds(seconds int) bool {
	return seconds >= MinFunctionTimeoutSeconds && seconds <= MaxFunctionTimeoutSeconds
}

// EffectiveFunctionTimeoutSeconds returns the limit that applies to an
// organization given its stored setting, and whether that is the default. A
// stored value outside the bounds can only come from a direct database write;
// it is ignored rather than trusted.
func EffectiveFunctionTimeoutSeconds(stored *int32) (seconds int, isDefault bool) {
	if stored == nil || !IsFunctionTimeoutSeconds(int(*stored)) {
		return DefaultFunctionTimeoutSeconds, true
	}
	return int(*stored), false
}
