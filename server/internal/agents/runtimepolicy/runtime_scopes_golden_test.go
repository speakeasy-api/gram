package runtimepolicy

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

var updateRuntimeScopes = flag.Bool("update-runtime-scopes", false, "rewrite testdata/runtime_scopes.golden from the current runtime scope definitions")

// TestRuntimeScopeDefinitionsGolden pins each scope's agent-runtime lifecycle
// and safe-since version. Moving a scope's runtime safety into the core scope
// registry must leave this file unchanged.
func TestRuntimeScopeDefinitionsGolden(t *testing.T) {
	t.Parallel()

	scopes := make([]authz.Scope, 0, len(runtimeScopeDefinitions))
	for scope := range runtimeScopeDefinitions {
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)

	var b strings.Builder
	for _, scope := range scopes {
		definition := runtimeScopeDefinitions[scope]
		fmt.Fprintf(&b, "%s lifecycle=%s safe_since=%d\n", scope, definition.lifecycle, definition.safeSince)
	}

	path := filepath.Join("testdata", "runtime_scopes.golden")
	if *updateRuntimeScopes {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), b.String())
}
