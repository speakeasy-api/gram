package admission

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveMode_NullResolvesToReporting pins the rollout lever. An issuer
// whose mode was never configured measures rather than enforces, so
// admission control ships without changing anyone's behaviour. Flipping
// this to ModePresets is the deliberate, evidence-gated final step.
func TestResolveMode_NullResolvesToReporting(t *testing.T) {
	t.Parallel()

	mode, recognized := ResolveMode("", false)
	require.Equal(t, ModeReporting, mode)
	require.True(t, recognized, "an absent mode is a valid state, not a data error")
	require.False(t, mode.Enforces(), "the default must not enforce yet")
}

// TestResolveMode_EmptyStringFailsClosed: a non-NULL empty string is a data
// error, not an absent choice. Nothing writes one, so it can only come from
// a direct database write, and treating it as "unset" would silently hand a
// corrupt row the permissive default.
func TestResolveMode_EmptyStringFailsClosed(t *testing.T) {
	t.Parallel()

	empty, recognized := ResolveMode("", true)
	require.Equal(t, ModeDisabled, empty)
	require.False(t, recognized, "caller must be able to log the bad value")

	null, nullRecognized := ResolveMode("", false)
	require.NotEqual(t, null, empty, "NULL and empty string must not be conflated")
	require.True(t, nullRecognized)
}

func TestResolveMode_ExplicitValues(t *testing.T) {
	t.Parallel()

	for _, want := range Modes() {
		mode, recognized := ResolveMode(string(want), true)
		require.Equal(t, want, mode)
		require.True(t, recognized)
	}
}

// TestResolveMode_UnknownFailsClosed: a value outside the enum is a data
// error and must never be an implicit allow.
func TestResolveMode_UnknownFailsClosed(t *testing.T) {
	t.Parallel()

	mode, recognized := ResolveMode("allow-everything", true)
	require.Equal(t, ModeDisabled, mode)
	require.False(t, recognized, "caller must be able to log the bad value")
}

func TestIsValidMode(t *testing.T) {
	t.Parallel()

	for _, mode := range Modes() {
		require.True(t, IsValidMode(string(mode)))
	}
	require.False(t, IsValidMode(""), "the API requires an explicit choice; unset is not writable")
	require.False(t, IsValidMode("Presets"), "mode values are case-sensitive")
	require.False(t, IsValidMode("allow-everything"))
}

// TestReportingIsNotWritable: reporting is as permissive as open for as
// long as it is on, so it must never be selectable through the management
// API. It is a deployment-time default, not a setting an operator can leave
// switched on.
func TestReportingIsNotWritable(t *testing.T) {
	t.Parallel()

	require.False(t, IsValidMode(string(ModeReporting)))
	require.NotContains(t, Modes(), ModeReporting)

	// ResolveMode must still recognize it, or the rollout lever could not
	// resolve its own value.
	mode, recognized := ResolveMode(string(ModeReporting), true)
	require.Equal(t, ModeReporting, mode)
	require.True(t, recognized)
}

// TestEnforces covers the one property that separates reporting from every
// other mode.
func TestEnforces(t *testing.T) {
	t.Parallel()

	require.False(t, ModeReporting.Enforces())
	for _, mode := range Modes() {
		require.Truef(t, mode.Enforces(), "%s must enforce its decisions", mode)
	}
}
