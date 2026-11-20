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
