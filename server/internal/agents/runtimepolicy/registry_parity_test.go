package runtimepolicy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// The agent runtime scope registry is derived from the authz registry. Its
// version-by-version contents are pinned by the generated agent eligibility
// table in authz; this test pins the tombstones this package names.
func TestRuntimeScopeDefinitions_TombstonesComeFromModel(t *testing.T) {
	t.Parallel()

	var retiredScopes []authz.Scope
	for scope, eligibility := range authz.AgentEligibilityByScope() {
		if eligibility.Retired {
			retiredScopes = append(retiredScopes, scope)
		}
	}
	require.Contains(t, retiredScopes, scopeMCPApprovalReadTombstone)
	require.Contains(t, retiredScopes, scopeMCPApprovalDecideTombstone)
	for _, retired := range retiredScopes {
		lifecycle, ok := RuntimeScopeLifecycleFor(retired)
		require.True(t, ok, "%s: not registered", retired)
		require.Equal(t, RuntimeScopeLifecycleRetired, lifecycle)
	}
}
