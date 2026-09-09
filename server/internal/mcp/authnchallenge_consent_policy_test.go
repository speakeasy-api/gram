package mcp

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestConsentPolicyConnectExclusionsAndErrors(t *testing.T) {
	t.Parallel()
	for _, principal := range []string{"user:example-owner", "agent:example-agent"} {
		t.Run(principal, func(t *testing.T) {
			t.Parallel()
			scope := authz.ScopeMCPConnect
			policy := []authz.Grant{
				{PrincipalUrn: principal, Scope: scope, Selector: authz.NewSelector(scope, "example-server")},
				{PrincipalUrn: principal, Scope: authz.ScopeMCPBlockedConnect, Selector: authz.NewSelector(authz.ScopeMCPBlockedConnect, "example-server")},
			}
			allowed, err := consentPolicyConnect(policy, authz.MCPCheck(scope, "example-server", "example-project"))
			require.NoError(t, err)
			require.False(t, allowed)
			policy[1].Selector = authz.NewSelector(authz.ScopeMCPBlockedConnect, "other-server")
			allowed, err = consentPolicyConnect(policy, authz.MCPCheck(scope, "example-server", "example-project"))
			require.NoError(t, err)
			require.True(t, allowed)
			for _, resourceID := range []string{"", "*"} {
				allowed, err = consentPolicyConnect(policy, authz.MCPCheck(scope, resourceID, "example-project"))
				require.False(t, allowed)
				require.ErrorIs(t, err, authz.ErrInvalidCheck, "evaluation errors must propagate, not become ordinary ineligibility")
			}
		})
	}
}
