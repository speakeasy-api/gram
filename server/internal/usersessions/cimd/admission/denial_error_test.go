package admission

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDenialError_DescriptionIsActionable: the description is the end
// user's only clue, since MCP clients do not fall back to dynamic
// registration after an authorize rejection. A denial that reads like a
// generic "unknown client" leaves them with nowhere to go.
func TestDenialError_DescriptionIsActionable(t *testing.T) {
	t.Parallel()

	notListed := &DenialError{Mode: ModePresets, Reason: DenialNotListed}
	require.Contains(t, notListed.Description(), "not permitted by the server's client policy")
	require.Contains(t, notListed.Description(), "operator")

	disabled := &DenialError{Mode: ModeDisabled, Reason: DenialDisabled}
	require.Contains(t, disabled.Description(), "does not accept client ID metadata documents")
}

// TestDenialError_DescriptionNeverLeaksInternals: the description reaches an
// unauthenticated client verbatim, so it must not name the stored mode, the
// internal reason label, or anything else about how the policy is stored.
func TestDenialError_DescriptionNeverLeaksInternals(t *testing.T) {
	t.Parallel()

	for _, mode := range Modes() {
		for _, reason := range []DenialReason{DenialDisabled, DenialNotListed, DenialOversized, DenialUnknownMode} {
			description := (&DenialError{Mode: mode, Reason: reason}).Description()
			require.NotEmpty(t, description)
			require.NotContainsf(t, description, string(reason), "description leaked reason label for %s/%s", mode, reason)
			require.NotContainsf(t, description, string(mode), "description leaked mode for %s/%s", mode, reason)
		}
	}
}

// TestDenialError_ErrorCarriesDiagnosticDetail: unlike Description, the Go
// error text is for operators and logs, so it does carry the mode and
// reason.
func TestDenialError_ErrorCarriesDiagnosticDetail(t *testing.T) {
	t.Parallel()

	err := &DenialError{Mode: ModePresets, Reason: DenialNotListed}
	require.Contains(t, err.Error(), string(DenialNotListed))
	require.Contains(t, err.Error(), string(ModePresets))
}
