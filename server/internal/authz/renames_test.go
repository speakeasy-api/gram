package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The definitions rename nothing yet, so these use their own table. The
// targets are declared scopes, so the selector kind comes from the real
// catalogue.
func TestCanonicalGrant(t *testing.T) {
	t.Parallel()

	renames := make(map[Scope]scopeRename)
	// The same resource, under a new name on another kind.
	renames["org:old_read"] = scopeRename{To: ScopeProjectRead, WildcardResource: false}
	// A move from the organization onto a type inside a project.
	renames["org:old_ingest"] = scopeRename{To: ScopeMCPRead, WildcardResource: true}

	t.Run("a scope that was not renamed is unchanged", func(t *testing.T) {
		t.Parallel()
		grant := NewGrant(ScopeOrgRead, "organization-1")
		require.Equal(t, grant, canonicalGrant(renames, grant))
	})

	t.Run("the selector moves to the new kind and keeps its resource", func(t *testing.T) {
		t.Parallel()
		stored := Grant{
			PrincipalUrn: "role:viewer",
			Scope:        "org:old_read",
			Selector:     Selector{SelectorKeyResourceKind: "org", SelectorKeyResourceID: "project-1"},
			Inferred:     true,
		}
		got := canonicalGrant(renames, stored)
		require.Equal(t, Grant{
			PrincipalUrn: "role:viewer",
			Scope:        ScopeProjectRead,
			Selector:     Selector{SelectorKeyResourceKind: ResourceKindForScope(ScopeProjectRead), SelectorKeyResourceID: "project-1"},
			Inferred:     true,
		}, got)
		require.Equal(t, "org", stored.Selector[SelectorKeyResourceKind], "the stored selector is not modified")
	})

	t.Run("an organization resource id becomes the wildcard and other keys stay", func(t *testing.T) {
		t.Parallel()
		got := canonicalGrant(renames, Grant{
			PrincipalUrn: "agent:a1",
			Scope:        "org:old_ingest",
			Selector: Selector{
				SelectorKeyResourceKind: "org",
				SelectorKeyResourceID:   "organization-1",
				SelectorKeyProjectID:    "project-1",
			},
			Inferred: false,
		})
		require.Equal(t, ScopeMCPRead, got.Scope)
		require.Equal(t, Selector{
			SelectorKeyResourceKind: ResourceKindForScope(ScopeMCPRead),
			SelectorKeyResourceID:   WildcardResource,
			SelectorKeyProjectID:    "project-1",
		}, got.Selector)
	})

	t.Run("a nil selector stays nil", func(t *testing.T) {
		t.Parallel()
		got := canonicalGrant(renames, Grant{PrincipalUrn: "", Scope: "org:old_read", Selector: nil, Inferred: false})
		require.Equal(t, ScopeProjectRead, got.Scope)
		require.Nil(t, got.Selector)
	})
}

// Until a scope is renamed, reading stored grants changes nothing.
func TestCanonicalGrant_DefinitionsRenameNothingYet(t *testing.T) {
	t.Parallel()

	require.Empty(t, scopeRenames)
	for _, scope := range RegisteredScopes() {
		require.Equal(t, scope, CanonicalScope(scope))
	}
}
