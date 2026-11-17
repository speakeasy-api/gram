package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestGrantsAuthorizeAnyResourceTreatsWildcardToolAsNoLimit(t *testing.T) {
	t.Parallel()

	for _, key := range []string{authz.SelectorKeyTool, authz.SelectorKeyDisposition} {
		selector := authz.NewSelector(authz.ScopeMCPConnect, "server-1")
		selector[authz.SelectorKeyProjectID] = "project-1"
		selector[key] = authz.WildcardResource
		grants := []authz.Grant{authz.NewGrantWithSelector(authz.ScopeMCPConnect, selector)}

		require.True(t, grantsAuthorizeAnyResource(grants, "org-1", authz.ScopeMCPConnect), "a connect grant with a wildcard %s makes connect tools discoverable", key)
	}
}

func TestGrantsAuthorizeAnyResourceProbesEveryDispositionOfANamedTool(t *testing.T) {
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
	require.True(t, grantsAuthorizeAnyResource([]authz.Grant{grant, block(authz.DispositionDestructive)}, "org-1", authz.ScopeMCPConnect))

	// A block on every tool takes the grant away whatever the tool is.
	require.False(t, grantsAuthorizeAnyResource([]authz.Grant{grant, block("")}, "org-1", authz.ScopeMCPConnect))

	// A grant limited to a disposition is probed with that disposition only.
	limited := authz.NewSelector(authz.ScopeMCPConnect, "server-1")
	limited[authz.SelectorKeyProjectID] = "project-1"
	limited[authz.SelectorKeyDisposition] = authz.DispositionDestructive
	require.False(t, grantsAuthorizeAnyResource([]authz.Grant{authz.NewGrantWithSelector(authz.ScopeMCPConnect, limited), block(authz.DispositionDestructive)}, "org-1", authz.ScopeMCPConnect))
}
