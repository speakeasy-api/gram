package mcp

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

// An mcp:blocked_* scope removes the permission it names and every
// permission that implies it: blocking read also takes write, because write
// implies read. Connect is implied by nothing else here, so blocking it leaves
// read and write standing, and blocking read or write leaves connect.
func TestConsentPolicyExclusionHierarchy(t *testing.T) {
	t.Parallel()
	for _, blocked := range []authz.Scope{authz.ScopeMCPBlockedConnect, authz.ScopeMCPBlockedRead, authz.ScopeMCPBlockedWrite} {
		for _, scope := range []authz.Scope{authz.ScopeMCPConnect, authz.ScopeMCPRead, authz.ScopeMCPWrite} {
			t.Run(string(blocked)+"/"+string(scope), func(t *testing.T) {
				t.Parallel()
				policy := []authz.Grant{
					authz.NewGrant(authz.ScopeMCPWrite, "example-server"),
					authz.NewGrant(authz.ScopeMCPConnect, "example-server"),
					authz.NewGrant(blocked, "example-server"),
				}
				allowed, err := consentPolicyConnect(policy, authz.MCPCheck(scope, "example-server", "example-project"))
				require.NoError(t, err)
				// mcp:write and mcp:connect satisfy all three checks, so what
				// takes one away is the block on the checked scope or on any
				// scope it implies.
				want := true
				for _, implied := range authz.ScopeImplicationClosure(scope) {
					if exclusion, ok := authz.ExclusionScopeFor(implied); ok && exclusion == blocked {
						want = false
					}
				}
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
