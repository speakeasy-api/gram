package mcp

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

// The mcp:blocked_* scopes are independent of one another: each removes the
// grant it names and nothing else. Connecting to a server and administering
// it are different jobs, so blocking one must leave the other standing (see
// scopeExpansions in authz/scopes.go).
func TestConsentPolicyExclusionHierarchy(t *testing.T) {
	t.Parallel()
	for _, blocked := range []authz.Scope{authz.ScopeMCPBlockedConnect, authz.ScopeMCPBlockedRead, authz.ScopeMCPBlockedWrite} {
		for _, scope := range []authz.Scope{authz.ScopeMCPConnect, authz.ScopeMCPRead, authz.ScopeMCPWrite} {
			t.Run(string(blocked)+"/"+string(scope), func(t *testing.T) {
				t.Parallel()
				policy := []authz.Grant{
					authz.NewGrant(authz.ScopeMCPWrite, "example-server"),
					authz.NewGrant(blocked, "example-server"),
				}
				allowed, err := consentPolicyConnect(policy, authz.MCPCheck(scope, "example-server", "example-project"))
				require.NoError(t, err)
				// mcp:write satisfies all three checks, so the only thing
				// that can take one away is that scope's own exclusion.
				exclusion, ok := authz.ExclusionScopeFor(scope)
				require.True(t, ok)
				want := blocked != exclusion
				require.Equal(t, want, allowed)
			})
		}
	}
}

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
