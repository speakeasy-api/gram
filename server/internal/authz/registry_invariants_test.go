package authz_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authz/internal/registry"
)

// The tests in this file state properties every authorization model must
// keep, and check them for each permission registry.Definitions declares by
// evaluating real grants. They are where the model meets the evaluator, so
// they live with the evaluator: the registry itself imports nothing from
// authz. They do not list scopes: a permission added to the registry is
// covered by them without a new test. A failure names the permission and the
// property it breaks.

// invariantCheck builds a check of scope on a resource of t, carrying a
// concrete value for every dimension t requires. project is the project_id
// value used where t requires one.
func invariantCheck(t registry.ResourceType, scope registry.Scope, resourceID, project string) authz.Check {
	if t.Kind == authz.ResourceKindMCPAuthClient {
		return authz.MCPAuthClientCheck(resourceID, authz.MCPAuthClientDimensions{
			ClientType:      authz.AuthClientTypeServiceAccount,
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
	return authz.Check{Scope: authz.Scope(scope), ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions}
}

// invariantGrant is a grant of a scope the model declares.
func invariantGrant(scope registry.Scope, resourceID string) authz.Grant {
	return authz.NewGrant(authz.Scope(scope), resourceID)
}

func invariantAuthorizes(t *testing.T, grants []authz.Grant, check authz.Check) bool {
	t.Helper()
	allowed, err := authz.GrantsAuthorize(grants, check)
	require.NoError(t, err, "check %s on %s", check.Scope, check.ResourceID)
	return allowed
}

func invariantName(t registry.ResourceType, p registry.Permission) string {
	return fmt.Sprintf("%s.%s", t.Kind, p.Action)
}

// A grant of a permission satisfies every permission it implies, directly or
// through a chain.
func TestModelInvariant_GrantSatisfiesItsImplications(t *testing.T) {
	t.Parallel()

	for _, typ := range registry.Definitions.Types {
		for _, higher := range typ.Permissions {
			grants := []authz.Grant{invariantGrant(typ.ScopeFor(higher), authz.WildcardResource)}
			for _, action := range typ.Implied(higher.Action) {
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

	for _, grantType := range registry.Definitions.Types {
		for _, granted := range grantType.Permissions {
			grants := []authz.Grant{invariantGrant(grantType.ScopeFor(granted), authz.WildcardResource)}
			closure := grantType.Implied(granted.Action)
			for _, checkType := range registry.Definitions.Types {
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

	for _, typ := range registry.Definitions.Types {
		for _, p := range typ.Permissions {
			grants := []authz.Grant{invariantGrant(typ.ScopeFor(p), "resource-1")}
			require.True(t, invariantAuthorizes(t, grants, invariantCheck(typ, typ.ScopeFor(p), "resource-1", "project-1")), "%s on resource-1 must satisfy a check on resource-1", invariantName(typ, p))
			require.False(t, invariantAuthorizes(t, grants, invariantCheck(typ, typ.ScopeFor(p), "resource-2", "project-1")), "%s on resource-1 must not satisfy a check on resource-2", invariantName(typ, p))
		}
	}
}

// A grant bounded on a required dimension never satisfies a check carrying
// another value for it.
func TestModelInvariant_RequiredDimensionsBoundGrants(t *testing.T) {
	t.Parallel()

	for _, typ := range registry.Definitions.Types {
		for _, d := range typ.Dimensions {
			if !d.Required {
				continue
			}
			for _, p := range typ.Permissions {
				selector := authz.NewSelector(authz.Scope(typ.ScopeFor(p)), authz.WildcardResource)
				selector[d.Key] = "project-1"
				grants := []authz.Grant{authz.NewGrantWithSelector(authz.Scope(typ.ScopeFor(p)), selector)}
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

	for _, typ := range registry.Definitions.Types {
		for _, blocked := range typ.Permissions {
			if !blocked.HasBlock() {
				continue
			}
			block := invariantGrant(typ.BlockScopeFor(blocked), authz.WildcardResource)

			grants := []authz.Grant{invariantGrant(typ.ScopeFor(blocked), authz.WildcardResource), block}
			require.False(t, invariantAuthorizes(t, grants, invariantCheck(typ, typ.ScopeFor(blocked), "resource-1", "project-1")), "%s must deny %s", typ.BlockScopeFor(blocked), invariantName(typ, blocked))

			for _, higher := range typ.Permissions {
				if !slices.Contains(typ.Implied(higher.Action), blocked.Action) || !higher.HasBlock() {
					continue
				}
				grants := []authz.Grant{invariantGrant(typ.ScopeFor(higher), authz.WildcardResource), block}
				require.False(t, invariantAuthorizes(t, grants, invariantCheck(typ, typ.ScopeFor(higher), "resource-1", "project-1")), "%s must also deny %s, which implies %s", typ.BlockScopeFor(blocked), invariantName(typ, higher), invariantName(typ, blocked))
			}
		}
	}
}

// Root satisfies every check except auth-client use, which needs its own
// grant.
func TestModelInvariant_RootSatisfiesEverythingButAuthClients(t *testing.T) {
	t.Parallel()

	grants := []authz.Grant{invariantGrant(registry.Definitions.Root.Scope, authz.WildcardResource)}
	for _, typ := range registry.Definitions.Types {
		for _, p := range typ.Permissions {
			allowed := invariantAuthorizes(t, grants, invariantCheck(typ, typ.ScopeFor(p), "resource-1", "project-1"))
			if typ.Kind == authz.ResourceKindMCPAuthClient {
				require.False(t, allowed, "root must not satisfy %s", invariantName(typ, p))
				continue
			}
			require.True(t, allowed, "root must satisfy %s", invariantName(typ, p))
		}
	}
}
