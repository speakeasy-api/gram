package authz

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests prove the registry (Model) says exactly what today's hand-kept
// tables say. They exist for one change: the next one replaces each table with
// its derivation and turns these into a golden snapshot of the derived
// tables.

func sortedScopes(in []Scope) []Scope {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

func TestModelParity_ScopeSet(t *testing.T) {
	t.Parallel()

	derived := Model.deriveScopeVisibility()
	var got []Scope
	for scope := range derived {
		got = append(got, scope)
	}
	slices.Sort(got)
	require.Equal(t, RegisteredScopes(), got)
}

func TestModelParity_Visibility(t *testing.T) {
	t.Parallel()
	require.Equal(t, scopeVisibilityByScope, Model.deriveScopeVisibility())
}

func TestModelParity_Expansions(t *testing.T) {
	t.Parallel()

	derived := Model.deriveScopeExpansions()
	require.Len(t, derived, len(scopeExpansions))
	for scope, want := range scopeExpansions {
		got, ok := derived[scope]
		require.True(t, ok, "%s: missing from the derived expansions", scope)
		require.Equal(t, sortedScopes(want), sortedScopes(got), "%s: expansions differ", scope)
	}
}

func TestModelParity_Exclusions(t *testing.T) {
	t.Parallel()
	require.Equal(t, scopeExclusions, Model.deriveScopeExclusions())
}

func TestModelParity_ManagedBy(t *testing.T) {
	t.Parallel()
	require.Equal(t, scopeGrantSource, Model.deriveGrantSource())
}

func TestModelParity_ResourceKinds(t *testing.T) {
	t.Parallel()

	derived := Model.deriveResourceKinds()
	for _, scope := range RegisteredScopes() {
		require.Equal(t, ResourceKindForScope(scope), derived[scope], "%s: resource kind differs", scope)
	}
}

func TestModelParity_SelectorKeys(t *testing.T) {
	t.Parallel()

	require.Equal(t, allowedSelectorKeys, Model.deriveAllowedSelectorKeys())

	perScope := Model.deriveScopeSelectorKeys()
	for _, scope := range RegisteredScopes() {
		for _, key := range []string{SelectorKeyTool, SelectorKeyDisposition, SelectorKeyProjectID, SelectorKeyServerURL, SelectorKeyServerIdentity, SelectorKeyClientType, SelectorKeyConnectionOwner, SelectorKeyUpstreamClient} {
			require.Equal(t, scopeAllowsSelectorKey(scope, key), perScope[scope][key], "%s/%s: per-scope selector key differs", scope, key)
		}
	}
}

func TestModelParity_RequiredDimensions(t *testing.T) {
	t.Parallel()
	require.Equal(t, requiredCheckDimensions, Model.deriveRequiredCheckDimensions())
}

func TestModelParity_DimensionValues(t *testing.T) {
	t.Parallel()
	require.Equal(t, validDispositions, Model.deriveDimensionValues("mcp", SelectorKeyDisposition))
	require.Equal(t, validAuthClientTypes, Model.deriveDimensionValues("mcp_auth_client", SelectorKeyClientType))
}

func TestModelParity_SystemRoles(t *testing.T) {
	t.Parallel()
	require.Equal(t, adminScopes, Model.deriveSystemRoleScopes(SystemRoleAdmin))
	require.Equal(t, memberScopes, Model.deriveSystemRoleScopes(SystemRoleMember))
	require.Len(t, Model.SystemRoles, len(SystemRoleGrants))
}

func TestModelParity_Catalog(t *testing.T) {
	t.Parallel()
	require.Equal(t, scopeCatalog, Model.deriveCatalog())
}
