package authz_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestGrantsReachSomeResourceNeedsAGrant(t *testing.T) {
	t.Parallel()

	require.False(t, authz.GrantsReachSomeResource(nil, authz.ScopeMCPRead, "org-1"))
	require.False(t, authz.GrantsReachSomeResource([]authz.Grant{authz.NewGrant(authz.ScopeOrgRead, "org-1")}, authz.ScopeMCPRead, "org-1"))
}

func TestGrantsReachSomeResourceFollowsImpliedScopes(t *testing.T) {
	t.Parallel()

	require.True(t, authz.GrantsReachSomeResource([]authz.Grant{authz.NewGrant(authz.ScopeMCPWrite, "server-1")}, authz.ScopeMCPRead, "org-1"))
}

func TestGrantsReachSomeResourceAsksOrgScopesAboutTheCallersOrganization(t *testing.T) {
	t.Parallel()

	require.True(t, authz.GrantsReachSomeResource([]authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, "org-1")}, authz.ScopeOrgAdmin, "org-1"))
	require.False(t, authz.GrantsReachSomeResource([]authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, "org-2")}, authz.ScopeOrgAdmin, "org-1"))
}

func TestGrantsReachSomeResourceIgnoresABlockOnOtherServers(t *testing.T) {
	t.Parallel()

	grants := []authz.Grant{
		authz.NewGrant(authz.ScopeMCPRead, authz.WildcardResource),
		authz.NewGrant(authz.ScopeMCPBlockedRead, "server-1"),
	}
	require.True(t, authz.GrantsReachSomeResource(grants, authz.ScopeMCPRead, "org-1"), "servers other than server-1 stay readable")

	single := []authz.Grant{
		authz.NewGrant(authz.ScopeMCPRead, "server-1"),
		authz.NewGrant(authz.ScopeMCPBlockedRead, "server-1"),
	}
	require.False(t, authz.GrantsReachSomeResource(single, authz.ScopeMCPRead, "org-1"), "the only readable server is blocked")
}

func TestGrantsReachSomeResourceHonoursABlockOnEveryServer(t *testing.T) {
	t.Parallel()

	grants := []authz.Grant{
		authz.NewGrant(authz.ScopeMCPRead, authz.WildcardResource),
		authz.NewGrant(authz.ScopeMCPBlockedRead, authz.WildcardResource),
	}
	require.False(t, authz.GrantsReachSomeResource(grants, authz.ScopeMCPRead, "org-1"))
}

func TestGrantsReachSomeResourceTreatsWildcardToolAsNoLimit(t *testing.T) {
	t.Parallel()

	for _, key := range []string{authz.SelectorKeyTool, authz.SelectorKeyDisposition} {
		selector := authz.NewSelector(authz.ScopeMCPConnect, "server-1")
		selector[authz.SelectorKeyProjectID] = "project-1"
		selector[key] = authz.WildcardResource
		grants := []authz.Grant{authz.NewGrantWithSelector(authz.ScopeMCPConnect, selector)}

		require.True(t, authz.GrantsReachSomeResource(grants, authz.ScopeMCPConnect, "org-1"), "a connect grant with a wildcard %s makes connect tools discoverable", key)
	}
}

func TestGrantsReachSomeResourceProbesEveryDispositionOfANamedTool(t *testing.T) {
	t.Parallel()

	connect := authz.NewSelector(authz.ScopeMCPConnect, "server-1")
	connect[authz.SelectorKeyProjectID] = "project-1"
	connect[authz.SelectorKeyTool] = "search"
	block := func(disposition string) authz.Grant {
		selector := authz.NewSelector(authz.ScopeMCPBlockedConnect, authz.WildcardResource)
		if disposition != "" {
			selector[authz.SelectorKeyDisposition] = disposition
		}
		return authz.NewGrantWithSelector(authz.ScopeMCPBlockedConnect, selector)
	}
	grant := authz.NewGrantWithSelector(authz.ScopeMCPConnect, connect)

	// The tool may be read-only, which the block does not take away, so
	// connect tools stay discoverable.
	require.True(t, authz.GrantsReachSomeResource([]authz.Grant{grant, block(authz.DispositionDestructive)}, authz.ScopeMCPConnect, "org-1"))

	// A block on every tool takes the grant away whatever the tool is.
	require.False(t, authz.GrantsReachSomeResource([]authz.Grant{grant, block("")}, authz.ScopeMCPConnect, "org-1"))

	// A grant limited to a disposition is probed with that disposition only.
	limited := authz.NewSelector(authz.ScopeMCPConnect, "server-1")
	limited[authz.SelectorKeyProjectID] = "project-1"
	limited[authz.SelectorKeyDisposition] = authz.DispositionDestructive
	require.False(t, authz.GrantsReachSomeResource([]authz.Grant{authz.NewGrantWithSelector(authz.ScopeMCPConnect, limited), block(authz.DispositionDestructive)}, authz.ScopeMCPConnect, "org-1"))
}

func TestGrantsReachSomeResourceProbesADispositionGrantWithAnUnnamedTool(t *testing.T) {
	t.Parallel()

	connect := authz.NewSelector(authz.ScopeMCPConnect, "server-1")
	connect[authz.SelectorKeyProjectID] = "project-1"
	connect[authz.SelectorKeyDisposition] = authz.DispositionReadOnly
	blockSelector := authz.NewSelector(authz.ScopeMCPBlockedConnect, authz.WildcardResource)
	blockSelector[authz.SelectorKeyTool] = "delete_everything"
	grants := []authz.Grant{
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, connect),
		authz.NewGrantWithSelector(authz.ScopeMCPBlockedConnect, blockSelector),
	}

	// The grant reaches every read-only tool but the blocked one, so connect
	// tools stay discoverable.
	require.True(t, authz.GrantsReachSomeResource(grants, authz.ScopeMCPConnect, "org-1"))
}
