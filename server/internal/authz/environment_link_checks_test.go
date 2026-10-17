package authz

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	linkProject = "project-1"
	linkEnvX    = "env-x"
	linkEnvY    = "env-y"
)

func projectEnvironmentGrant(scope Scope, projectID string) Grant {
	return NewGrantWithSelector(scope, Selector{
		SelectorKeyResourceKind: "environment",
		SelectorKeyResourceID:   WildcardResource,
		SelectorKeyProjectID:    projectID,
	})
}

func environmentGrant(scope Scope, environmentID string) Grant {
	return NewGrantWithSelector(scope, Selector{
		SelectorKeyResourceKind: "environment",
		SelectorKeyResourceID:   environmentID,
		SelectorKeyProjectID:    linkProject,
	})
}

// linkAuthorized evaluates every check EnvironmentLinkChecks builds, all of
// which must pass, as Engine.Require and Engine.Evaluate do.
func linkAuthorized(t *testing.T, grants []Grant, environmentIDs ...string) bool {
	t.Helper()

	for _, check := range EnvironmentLinkChecks(linkProject, environmentIDs...) {
		allowed, err := GrantsAuthorize(grants, check)
		require.NoError(t, err)
		if !allowed {
			return false
		}
	}
	return true
}

func TestEnvironmentLinkChecksHonourEnvironmentExclusions(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		projectEnvironmentGrant(ScopeEnvironmentRead, linkProject),
		environmentGrant(ScopeEnvironmentBlockedRead, linkEnvX),
	}

	// The project-wide check alone never sees the exclusion.
	allowed, err := GrantsAuthorize(grants, EnvironmentLinkCheck(linkProject))
	require.NoError(t, err)
	require.True(t, allowed)

	require.False(t, linkAuthorized(t, grants, linkEnvX))
	require.True(t, linkAuthorized(t, grants, linkEnvY))
	// Any affected environment being excluded refuses the change.
	require.False(t, linkAuthorized(t, grants, linkEnvY, linkEnvX))
}

func TestEnvironmentLinkChecksRequireTheProjectWideGrant(t *testing.T) {
	t.Parallel()

	// Read on the one environment is not project-wide authority.
	require.False(t, linkAuthorized(t, []Grant{environmentGrant(ScopeEnvironmentRead, linkEnvX)}, linkEnvX))
	// Another project's grant does not reach this one.
	require.False(t, linkAuthorized(t, []Grant{projectEnvironmentGrant(ScopeEnvironmentRead, "project-2")}, linkEnvX))
	// environment:write expands to read.
	require.True(t, linkAuthorized(t, []Grant{projectEnvironmentGrant(ScopeEnvironmentWrite, linkProject)}, linkEnvX))
	// With nothing affected only the project-wide check remains.
	require.Len(t, EnvironmentLinkChecks(linkProject), 1)
	require.Len(t, EnvironmentLinkChecks(linkProject, linkEnvX, "", linkEnvX), 2)
}

func TestEnvironmentLinkChecksFollowPrincipalPrecedence(t *testing.T) {
	t.Parallel()

	inheritedBlock := []Grant{
		heldBy(precedenceRole, projectEnvironmentGrant(ScopeEnvironmentRead, linkProject)),
		heldBy(precedenceRole, environmentGrant(ScopeEnvironmentBlockedRead, linkEnvX)),
	}
	require.False(t, linkAuthorized(t, inheritedBlock, linkEnvX))

	// A direct grant naming the environment outranks a block inherited from a
	// role, as everywhere else.
	directGrant := append(slices.Clone(inheritedBlock), heldBy(precedenceUser, environmentGrant(ScopeEnvironmentRead, linkEnvX)))
	require.True(t, linkAuthorized(t, directGrant, linkEnvX))

	// The caller's own block always applies.
	ownBlock := []Grant{
		heldBy(precedenceRole, projectEnvironmentGrant(ScopeEnvironmentRead, linkProject)),
		heldBy(precedenceUser, environmentGrant(ScopeEnvironmentRead, linkEnvX)),
		heldBy(precedenceUser, environmentGrant(ScopeEnvironmentBlockedRead, linkEnvX)),
	}
	require.False(t, linkAuthorized(t, ownBlock, linkEnvX))
}
