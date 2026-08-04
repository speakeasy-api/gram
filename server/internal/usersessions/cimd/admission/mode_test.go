package admission

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveMode_NullResolvesToPresets pins the default that the whole
// rollout rests on: an issuer whose mode was never configured stores NULL
// and must behave exactly like one explicitly set to presets.
func TestResolveMode_NullResolvesToPresets(t *testing.T) {
	t.Parallel()

	mode, recognized := ResolveMode("", false)
	require.Equal(t, ModePresets, mode)
	require.True(t, recognized, "an absent mode is a valid state, not a data error")
}

// TestResolveMode_EmptyStringResolvesToPresets covers the degenerate stored
// value. A non-NULL empty string is indistinguishable from unset in intent,
// so it must not fall through to the fail-closed branch.
func TestResolveMode_EmptyStringResolvesToPresets(t *testing.T) {
	t.Parallel()

	mode, recognized := ResolveMode("", true)
	require.Equal(t, ModePresets, mode)
	require.True(t, recognized)
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
