package risk

import (
	"testing"

	"github.com/stretchr/testify/require"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestValidateAction(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"flag", "block", "warn"} {
		require.NoError(t, validateAction(action), "action %q must be accepted", action)
	}
	for _, action := range []string{"", "warning", "deny", "ask", "Block"} {
		require.Error(t, validateAction(action), "action %q must be rejected", action)
	}
}

func TestValidateSourceAction_WarnIsBlockingClass(t *testing.T) {
	t.Parallel()

	flagOnly := []string{shadowmcp.SourceDestructiveTool}
	scannable := []string{ra.SourceGitleaks, ra.SourcePresidio}

	// flag is unconstrained on any source.
	require.NoError(t, validateSourceAction(flagOnly, "flag"))
	require.NoError(t, validateSourceAction(scannable, "flag"))

	// warn and block are equally constrained: rejected on flag-only sources,
	// allowed on scannable ones.
	for _, action := range []string{"warn", "block"} {
		require.Error(t, validateSourceAction(flagOnly, action),
			"%q must be rejected on a flag-only source", action)
		require.Error(t, validateSourceAction([]string{ra.SourceCLIDestructive}, action),
			"%q must be rejected on cli_destructive", action)
		require.NoError(t, validateSourceAction(scannable, action),
			"%q must be allowed on scannable sources", action)
	}
}
