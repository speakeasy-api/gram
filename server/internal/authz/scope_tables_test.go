package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A scope moves from undeclaredScopes into declarations.go; listing it in
// both would let the hand entry silently override the declaration.
func TestBuildScopeTables_RejectsScopeDeclaredTwice(t *testing.T) {
	t.Parallel()

	reg, err := DefaultRegistry()
	require.NoError(t, err)
	//exhaustive:ignore
	_, err = buildScopeTables(reg, map[Scope]undeclaredScope{
		ScopeMCPRead: {visibility: scopeVisibilityUserVisible, satisfiedBy: nil, exclusion: "", roles: nil},
	})
	require.ErrorContains(t, err, "scope mcp:read is both declared and listed as undeclared")
}

// Every declared scope's tables come from the registry, so the hand-written
// remainder only covers scopes no type declares.
func TestScopeTables_DeclaredScopesComeFromRegistry(t *testing.T) {
	t.Parallel()

	reg, err := DefaultRegistry()
	require.NoError(t, err)
	for _, def := range reg.Scopes() {
		require.NotContains(t, undeclaredScopes, Scope(def.Name))
		require.Contains(t, scopeVisibilityByScope, Scope(def.Name))
	}
	require.Len(t, scopeVisibilityByScope, len(reg.Scopes())+len(undeclaredScopes))
}
