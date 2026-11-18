package runtimepolicy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// Credentials issued while mcp:read and mcp:write implied mcp:connect stored
// that connect in their policy's effective grants. They must keep decoding
// and keep the connect they record, now that authz no longer implies it.
func TestStoredPoliciesKeepReadWriteConnect(t *testing.T) {
	t.Parallel()

	for version := DelegatedPolicyVersion1; version <= DelegatedPolicyVersion7; version++ {
		for _, scope := range []authz.Scope{authz.ScopeMCPRead, authz.ScopeMCPWrite} {
			effective := `{"scope":"mcp:connect","selector":{"resource_id":"*","resource_kind":"mcp"}},{"scope":"mcp:read","selector":{"resource_id":"*","resource_kind":"mcp"}}`
			if scope == authz.ScopeMCPWrite {
				effective += `,{"scope":"mcp:write","selector":{"resource_id":"*","resource_kind":"mcp"}}`
			}
			raw := []byte(`{"requested":[{"scope":"` + string(scope) + `","selector":{"resource_id":"*","resource_kind":"mcp"}}],"effective":[` + effective + `]}`)
			policy, err := DecodeDelegatedPolicy(version, raw)
			require.NoError(t, err, "version %d, %s", version, scope)

			allowed, err := authz.GrantsAuthorize(policy.RuntimeGrants(), authz.MCPCheck(authz.ScopeMCPConnect, "server-1", "project-1"))
			require.NoError(t, err)
			require.True(t, allowed, "version %d, %s", version, scope)

			issued, err := NewDelegatedPolicy(version, []authz.Grant{authz.NewGrant(scope, authz.WildcardResource)})
			require.NoError(t, err)
			encoded, err := EncodeDelegatedPolicy(version, issued)
			require.NoError(t, err)
			require.JSONEq(t, string(raw), string(encoded), "version %d, %s", version, scope)
		}
	}
}

// From version 8 a delegated policy records no connect for read or write.
func TestVersion8PoliciesRecordNoImpliedConnect(t *testing.T) {
	t.Parallel()

	for _, scope := range []authz.Scope{authz.ScopeMCPRead, authz.ScopeMCPWrite} {
		policy, err := NewDelegatedPolicy(DelegatedPolicyVersion8, []authz.Grant{authz.NewGrant(scope, authz.WildcardResource)})
		require.NoError(t, err)
		for _, grant := range policy.Effective {
			require.NotEqual(t, authz.ScopeMCPConnect, grant.Scope, scope)
		}
		allowed, err := authz.GrantsAuthorize(policy.RuntimeGrants(), authz.MCPCheck(authz.ScopeMCPConnect, "server-1", "project-1"))
		require.NoError(t, err)
		require.False(t, allowed, scope)
	}
}
