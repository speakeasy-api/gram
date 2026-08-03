package risk

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEnforcementScanBudgetMatchesGatingDeadline(t *testing.T) {
	t.Parallel()
	// Sync gating monitor documents an ~1s evaluation deadline; keep the
	// ScanForEnforcement cap aligned with that (not the PI judge's 10s window).
	require.Equal(t, time.Second, enforcementScanBudget)
}
