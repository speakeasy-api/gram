package runtimepolicy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// The agent runtime scope registry is derived from the authz model. Its
// version-by-version contents are pinned by the authz derived-tables golden;
// this test pins the tombstones this package names.
func TestRuntimeScopeDefinitions_TombstonesComeFromModel(t *testing.T) {
	t.Parallel()

	require.Contains(t, authz.Model.Retired, scopeMCPApprovalReadTombstone)
	require.Contains(t, authz.Model.Retired, scopeMCPApprovalDecideTombstone)
	for _, retired := range authz.Model.Retired {
		lifecycle, ok := RuntimeScopeLifecycleFor(retired)
		require.True(t, ok, "%s: not registered", retired)
		require.Equal(t, RuntimeScopeLifecycleRetired, lifecycle)
	}
}
