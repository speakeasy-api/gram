package platformmcp

import (
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestInternalSessionIssuerNextStepEligibility(t *testing.T) {
	t.Parallel()
	for _, state := range []internalsessionissuers.State{internalsessionissuers.StateSetupRequired, internalsessionissuers.StateDisabled} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			require.Contains(t, internalSessionIssuerNextStep(state, true), "An organization administrator can")
			require.NotContains(t, internalSessionIssuerNextStep(state, false), "An organization administrator can")
			require.Contains(t, internalSessionIssuerNextStep(state, false), "signing configuration")
		})
	}
	require.Equal(t, internalSessionIssuerNextSteps[internalsessionissuers.StateEnabled], internalSessionIssuerNextStep(internalsessionissuers.StateEnabled, false))
}
