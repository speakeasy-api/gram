package runtimepolicy_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

var updateRuntimeScopes = flag.Bool("update-runtime-scopes", false, "rewrite testdata/runtime_scopes.golden from the current runtime scope registry")

// retiredRuntimeScopeNames are tombstoned names that are no longer registered
// authorization scopes but must stay known to the runtime registry.
var retiredRuntimeScopeNames = []authz.Scope{"mcp_approval:read", "mcp_approval:decide"}

// TestRuntimeScopeDefinitionsGolden pins, for every registered scope and every
// retired name, its agent-runtime lifecycle and the first registry version in
// which it is safe for agent policy. It reads only the exported API, so moving
// the definitions to another package must leave this file and its golden
// unchanged.
func TestRuntimeScopeDefinitionsGolden(t *testing.T) {
	t.Parallel()

	scopes := slices.Concat(authz.RegisteredScopes(), retiredRuntimeScopeNames)
	slices.Sort(scopes)
	scopes = slices.Compact(scopes)

	var b strings.Builder
	for _, scope := range scopes {
		lifecycle, ok := runtimepolicy.RuntimeScopeLifecycleFor(scope)
		if !ok {
			lifecycle = "unregistered"
		}
		fmt.Fprintf(&b, "%s lifecycle=%s safe_since=%d\n", scope, lifecycle, firstSafeVersion(scope))
	}

	path := filepath.Join("testdata", "runtime_scopes.golden")
	if *updateRuntimeScopes {
		require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), b.String())
}

// firstSafeVersion returns the lowest registry version in which the scope is
// safe for agent policy, or 0 when it is safe in none.
func firstSafeVersion(scope authz.Scope) runtimepolicy.RuntimeScopeRegistryVersion {
	for version := runtimepolicy.RuntimeScopeRegistryVersion1; version <= runtimepolicy.CurrentRuntimeScopeRegistryVersion; version++ {
		if runtimepolicy.IsRuntimeScopeSafe(version, scope) {
			return version
		}
	}
	return 0
}
