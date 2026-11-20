package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// "warn" is the deprecated name of "confirm", so a retry that switches between
// the two names must hash the same and not look like a conflicting request.
func TestNormalizeRiskPolicyPatchForReceipt_CanonicalizesLegacyWarn(t *testing.T) {
	t.Parallel()

	warn, err := normalizeRiskPolicyPatchForReceipt(map[string]json.RawMessage{"action": json.RawMessage(`"warn"`)})
	require.NoError(t, err)
	confirm, err := normalizeRiskPolicyPatchForReceipt(map[string]json.RawMessage{"action": json.RawMessage(`" confirm "`)})
	require.NoError(t, err)

	require.Equal(t, map[string]any{"action": "confirm"}, warn)
	require.Equal(t, confirm, warn)
}
