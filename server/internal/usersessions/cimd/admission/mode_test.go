package admission

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveMode_NullResolvesToOpen pins the resting policy. An issuer
// whose mode was never configured admits every spec-valid client: a presets
// denial is unrecoverable for the end user, so enforcement is something an
// operator opts into rather than something a default does to them.
//
// New rows are written 'open' explicitly, so this branch covers rows created
// before that and any row a direct database write leaves unset.
func TestResolveMode_NullResolvesToOpen(t *testing.T) {
	t.Parallel()

	mode, recognized := ResolveMode("", false)
	require.Equal(t, ModeOpen, mode)
	require.True(t, recognized, "an absent mode is a valid state, not a data error")
	require.Equal(t, OutcomeAdmit, Evaluate(mode, "https://unknown.example.com/client.json").Outcome,
		"the default must refuse nobody")
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

// TestReportingIsNotWritable: reporting admits exactly what open admits, so
// offering it through the management API would put a second, vaguer name on
// a mode that already has an honest one.
func TestReportingIsNotWritable(t *testing.T) {
	t.Parallel()

	require.False(t, IsValidMode(string(ModeReporting)))
	require.NotContains(t, Modes(), ModeReporting)

	// ResolveMode must still recognize it, or a row written while it was
	// the default would fail closed.
	mode, recognized := ResolveMode(string(ModeReporting), true)
	require.Equal(t, ModeReporting, mode)
	require.True(t, recognized)
}

// TestEnforces covers the one property that separates reporting from every
// other mode.
//
// ModeOpen enforces, which is not a contradiction to relax: Evaluate never
// denies under it, so a caller can only hold an OutcomeDeny it built itself
// as a real refusal. The shadow measurement never produces one.
func TestEnforces(t *testing.T) {
	t.Parallel()

	require.False(t, ModeReporting.Enforces())
	for _, mode := range Modes() {
		require.Truef(t, mode.Enforces(), "%s must enforce its decisions", mode)
	}
}
