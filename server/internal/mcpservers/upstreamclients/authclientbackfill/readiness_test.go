package authclientbackfill

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrantReadinessIgnoresOnlySelectionCoverageFailures(t *testing.T) {
	t.Parallel()

	report := newReport(true)
	report.PhaseErrors[PhaseSelections] = "selections failed"
	require.True(t, sharedReady(report), "selection coverage records nothing grants depend on")
	require.True(t, keysReady(report))

	for _, phase := range []string{PhaseOAuth, PhaseHeaders, PhaseEMA, PhaseEMAWarnings, PhaseReconcile} {
		failed := newReport(true)
		failed.PhaseErrors[PhaseSelections] = "selections failed"
		failed.PhaseErrors[phase] = "failed"
		require.False(t, sharedReady(failed), phase)
		require.False(t, keysReady(failed), phase)
	}
}
