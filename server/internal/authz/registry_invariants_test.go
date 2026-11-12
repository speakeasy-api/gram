package authz

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// The tests in this file state properties every authorization model must
// keep, and check them for each permission Model declares by evaluating real
// grants. They do not list scopes: a permission added to the registry is
// covered by them without a new test. A failure names the permission and the
// property it breaks.

// invariantCheck builds a check of scope on a resource of t, carrying a
// concrete value for every dimension t requires. project is the project_id
// value used where t requires one.
func invariantCheck(t ResourceType, scope Scope, resourceID, project string) Check {
	if t.Kind == ResourceKindMCPAuthClient {
		return MCPAuthClientCheck(resourceID, MCPAuthClientDimensions{
			ClientType:      AuthClientTypeServiceAccount,
			ProjectID:       project,
			ConnectionOwner: "",
			UpstreamClient:  "",
		})
	}
	dimensions := make(map[string]string)
	for _, d := range t.Dimensions {
		if d.Required {
			dimensions[d.Key] = project
		}
	}
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, representedUser: ""}
}

func invariantAuthorizes(t *testing.T, grants []Grant, check Check) bool {
	t.Helper()
	allowed, err := GrantsAuthorize(grants, check)
	require.NoError(t, err, "check %s on %s", check.Scope, check.ResourceID)
	return allowed
}

func invariantName(t ResourceType, p Permission) string {
	return fmt.Sprintf("%s.%s", t.Kind, p.Action)
}

// A grant of a permission satisfies every permission it implies, directly or
// through a chain.
func TestModelInvariant_GrantSatisfiesItsImplications(t *testing.T) {
	t.Parallel()

	for _, typ := range Model.Types {
		for _, higher := range typ.Permissions {
			grants := []Grant{NewGrant(typ.ScopeFor(higher), WildcardResource)}
			for _, action := range impliedClosure(typ, higher.Action) {
				lower, _ := typ.Permission(action)
				check := invariantCheck(typ, typ.ScopeFor(lower), "resource-1", "project-1")
				require.True(t, invariantAuthorizes(t, grants, check), "%s must satisfy %s, which it implies", invariantName(typ, higher), invariantName(typ, lower))
			}
		}
	}
}

// A grant satisfies nothing it does not imply: not a permission on the same
// type outside its closure, and nothing on another type.
func TestModelInvariant_GrantSatisfiesNothingElse(t *testing.T) {
	t.Parallel()

	for _, grantType := range Model.Types {
		for _, granted := range grantType.Permissions {
			grants := []Grant{NewGrant(grantType.ScopeFor(granted), WildcardResource)}
			closure := impliedClosure(grantType, granted.Action)
			for _, checkType := range Model.Types {
				for _, checked := range checkType.Permissions {
					if checkType.Kind == grantType.Kind && (checked.Action == granted.Action || slices.Contains(closure, checked.Action)) {
						continue
					}
					check := invariantCheck(checkType, checkType.ScopeFor(checked), "resource-1", "project-1")
					require.False(t, invariantAuthorizes(t, grants, check), "%s must not satisfy %s", invariantName(grantType, granted), invariantName(checkType, checked))
				}
			}
		}
	}
}

// A grant naming one resource never satisfies a check on another.
func TestModelInvariant_ResourceGrantsStayOnTheirResource(t *testing.T) {
	t.Parallel()

	for _, typ := range Model.Types {
		for _, p := range typ.Permissions {
			grants := []Grant{NewGrant(typ.ScopeFor(p), "resource-1")}
			require.True(t, invariantAuthorizes(t, grants, invariantCheck(typ, typ.ScopeFor(p), "resource-1", "project-1")), "%s on resource-1 must satisfy a check on resource-1", invariantName(typ, p))
			require.False(t, invariantAuthorizes(t, grants, invariantCheck(typ, typ.ScopeFor(p), "resource-2", "project-1")), "%s on resource-1 must not satisfy a check on resource-2", invariantName(typ, p))
		}
	}
}

// A grant bounded on a required dimension never satisfies a check carrying
// another value for it.
func TestModelInvariant_RequiredDimensionsBoundGrants(t *testing.T) {
	t.Parallel()

	for _, typ := range Model.Types {
		for _, d := range typ.Dimensions {
			if !d.Required {
				continue
			}
			for _, p := range typ.Permissions {
				selector := NewSelector(typ.ScopeFor(p), WildcardResource)
				selector[d.Key] = "project-1"
				grants := []Grant{NewGrantWithSelector(typ.ScopeFor(p), selector)}
				require.True(t, invariantAuthorizes(t, grants, invariantCheck(typ, typ.ScopeFor(p), "resource-1", "project-1")), "%s bounded on %s must satisfy a check with the same value", invariantName(typ, p), d.Key)
				require.False(t, invariantAuthorizes(t, grants, invariantCheck(typ, typ.ScopeFor(p), "resource-1", "project-2")), "%s bounded on %s must not satisfy a check with another value", invariantName(typ, p), d.Key)
			}
		}
	}
}

// A block takes its permission away from a holder of an inherited wildcard
// allow, and also takes away every permission that implies it.
func TestModelInvariant_BlocksWinOverInheritedAllows(t *testing.T) {
	t.Parallel()

	for _, typ := range Model.Types {
		for _, blocked := range typ.Permissions {
			if !blocked.HasBlock() {
				continue
			}
			block := NewGrant(typ.BlockScopeFor(blocked), WildcardResource)

			grants := []Grant{NewGrant(typ.ScopeFor(blocked), WildcardResource), block}
			require.False(t, invariantAuthorizes(t, grants, invariantCheck(typ, typ.ScopeFor(blocked), "resource-1", "project-1")), "%s must deny %s", typ.BlockScopeFor(blocked), invariantName(typ, blocked))

			for _, higher := range typ.Permissions {
				if !slices.Contains(impliedClosure(typ, higher.Action), blocked.Action) || !higher.HasBlock() {
					continue
				}
				grants := []Grant{NewGrant(typ.ScopeFor(higher), WildcardResource), block}
				require.False(t, invariantAuthorizes(t, grants, invariantCheck(typ, typ.ScopeFor(higher), "resource-1", "project-1")), "%s must also deny %s, which implies %s", typ.BlockScopeFor(blocked), invariantName(typ, higher), invariantName(typ, blocked))
			}
		}
	}
}

// Root satisfies every check except auth-client use, which needs its own
// grant.
func TestModelInvariant_RootSatisfiesEverythingButAuthClients(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrant(Model.Root.Scope, WildcardResource)}
	for _, typ := range Model.Types {
		for _, p := range typ.Permissions {
			allowed := invariantAuthorizes(t, grants, invariantCheck(typ, typ.ScopeFor(p), "resource-1", "project-1"))
			if typ.Kind == ResourceKindMCPAuthClient {
				require.False(t, allowed, "root must not satisfy %s", invariantName(typ, p))
				continue
			}
			require.True(t, allowed, "root must satisfy %s", invariantName(typ, p))
		}
	}
}

// The member role is seeded with a subset of the admin role.
func TestModelInvariant_MemberIsASubsetOfAdmin(t *testing.T) {
	t.Parallel()

	admin := Model.SystemRoles[SystemRoleAdmin]
	for _, scope := range Model.SystemRoles[SystemRoleMember] {
		require.Contains(t, admin, scope, "member is seeded with %s, which admin is not", scope)
	}
}

// A grant constrained on a dimension never allows a request that could not
// determine it, and a block constrained on it always applies to one. Checked
// for every dimension a check may carry as unknown.
func TestModelInvariant_UnknownValuesFailClosed(t *testing.T) {
	t.Parallel()

	for _, typ := range Model.Types {
		for _, p := range typ.Permissions {
			scope := typ.ScopeFor(p)
			for key := range unknownableDimensions[scope] {
				value := "value-1"
				if values := Model.deriveDimensionValues(typ.Kind, key); len(values) > 0 {
					for v := range values {
						value = v
						break
					}
				}
				check := invariantCheck(typ, scope, "resource-1", "project-1")
				dimensions := map[string]string{key: SelectorValueUnknown}
				for k, v := range check.Dimensions {
					dimensions[k] = v
				}
				check.Dimensions = dimensions

				constrained := NewSelector(scope, WildcardResource)
				constrained[key] = value
				require.False(t, invariantAuthorizes(t, []Grant{NewGrantWithSelector(scope, constrained)}, check), "%s constrained on %s must not allow an unknown %s", invariantName(typ, p), key, key)

				if !p.HasBlock() {
					continue
				}
				block := NewSelector(typ.BlockScopeFor(p), WildcardResource)
				block[key] = value
				grants := []Grant{NewGrant(scope, WildcardResource), NewGrantWithSelector(typ.BlockScopeFor(p), block)}
				require.False(t, invariantAuthorizes(t, grants, check), "%s constrained on %s must apply to an unknown %s", typ.BlockScopeFor(p), key, key)
			}
		}
	}
}
