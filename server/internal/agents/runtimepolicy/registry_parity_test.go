package runtimepolicy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// TestRuntimeScopeDefinitions_MatchModel proves the authz registry declares
// the same agent eligibility as this package's overlay, scope for scope,
// tombstones included. The next change derives the overlay from the registry.
func TestRuntimeScopeDefinitions_MatchModel(t *testing.T) {
	t.Parallel()

	model := authz.AgentEligibilityByScope()
	require.Len(t, model, len(runtimeScopeDefinitions))
	for scope, definition := range runtimeScopeDefinitions {
		declared, ok := model[scope]
		require.True(t, ok, "%s: not in the authz model", scope)
		require.Equal(t, definition.lifecycle == RuntimeScopeLifecycleRetired, declared.Retired, "%s: lifecycle differs", scope)
		require.Equal(t, int(definition.safeSince), declared.SafeSince, "%s: safe-since version differs", scope)
	}
}
