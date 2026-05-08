package authz

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckExpand_orgRead(t *testing.T) {
	t.Parallel()

	checks := Check{Scope: ScopeOrgRead, ResourceID: "org_123"}.expand()

	require.Contains(t, checks, Check{Scope: ScopeRoot, ResourceID: "org_123"})
	require.Contains(t, checks, Check{Scope: ScopeOrgAdmin, ResourceID: "org_123"})
	require.Contains(t, checks, Check{Scope: ScopeOrgRead, ResourceID: "org_123"})
	// No wildcard resource variants — selector matching handles that natively.
	require.NotContains(t, checks, Check{Scope: ScopeOrgAdmin, ResourceID: WildcardResource})
}

func TestCheckExpand_mcpConnect(t *testing.T) {
	t.Parallel()

	checks := Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand()

	require.Contains(t, checks, Check{Scope: ScopeRoot, ResourceID: "tool_a"})
	require.Contains(t, checks, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"})
	require.Contains(t, checks, Check{Scope: ScopeMCPRead, ResourceID: "tool_a"})
	require.Contains(t, checks, Check{Scope: ScopeMCPWrite, ResourceID: "tool_a"})
	require.NotContains(t, checks, Check{Scope: ScopeMCPConnect, ResourceID: WildcardResource})
}

func TestGrantsHasAccess_orgAdminSatisfiesOrgRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeOrgAdmin, "org_123")}
	grant, _ := findMatchingGrant(g, Check{Scope: ScopeOrgRead, ResourceID: "org_123"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsHasAccess_orgReadDoesNotSatisfyOrgAdmin(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeOrgRead, "org_123")}
	grant, _ := findMatchingGrant(g, Check{Scope: ScopeOrgAdmin, ResourceID: "org_123"}.expand())
	require.Nil(t, grant)
}

func TestGrantsHasAccess_buildWriteSatisfiesBuildRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeProjectWrite, "proj_123")}
	grant, _ := findMatchingGrant(g, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsHasAccess_buildReadDoesNotSatisfyBuildWrite(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeProjectRead, "proj_123")}
	grant, _ := findMatchingGrant(g, Check{Scope: ScopeProjectWrite, ResourceID: "proj_123"}.expand())
	require.Nil(t, grant)
}

func TestGrantsHasAccess_orgAdminDoesNotSatisfyBuildRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeOrgAdmin, "org_123")}
	grant, _ := findMatchingGrant(g, Check{Scope: ScopeProjectRead, ResourceID: "org_123"}.expand())
	require.Nil(t, grant)
}

func TestGrantsHasAccess_mcpConnectDoesNotSatisfyMCPRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeMCPConnect, "tool_a")}
	grant, _ := findMatchingGrant(g, Check{Scope: ScopeMCPRead, ResourceID: "tool_a"}.expand())
	require.Nil(t, grant)
}

func TestGrantsHasAccess_mcpReadSatisfiesMCPConnect(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeMCPRead, "tool_a")}
	grant, _ := findMatchingGrant(g, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsHasAccess_mcpWriteSatisfiesMCPConnect(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeMCPWrite, "tool_a")}
	grant, _ := findMatchingGrant(g, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsHasAccess_mcpWriteSatisfiesMCPRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeMCPWrite, "tool_a")}
	grant, _ := findMatchingGrant(g, Check{Scope: ScopeMCPRead, ResourceID: "tool_a"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsHasAccess_rootWildcardSatisfiesAnyScope(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeRoot, WildcardResource)}

	grant, _ := findMatchingGrant(g, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"}.expand())
	require.NotNil(t, grant)

	grant, _ = findMatchingGrant(g, Check{Scope: ScopeOrgAdmin, ResourceID: "org_456"}.expand())
	require.NotNil(t, grant)

	grant, _ = findMatchingGrant(g, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsHasAccess_wrongResourceNotSatisfied(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeOrgAdmin, "org_123")}
	grant, _ := findMatchingGrant(g, Check{Scope: ScopeOrgRead, ResourceID: "org_999"}.expand())
	require.Nil(t, grant)
}

// --- Deny-wins evaluation tests ---

func TestEvaluateGrants_denyBlocksAllow(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, WildcardResource),
		NewDenyGrant(ScopeProjectRead, "proj_secret"),
	}
	checks := Check{Scope: ScopeProjectRead, ResourceID: "proj_secret"}.expand()

	allow, _, denied := evaluateGrants(grants, checks)
	require.Nil(t, allow)
	require.True(t, denied)
}

func TestEvaluateGrants_denyWinsRegardlessOfOrder(t *testing.T) {
	t.Parallel()

	// Deny grant comes first — should still block the allow.
	grants := []Grant{
		NewDenyGrant(ScopeMCPRead, "server_a"),
		NewGrant(ScopeMCPRead, WildcardResource),
	}
	checks := Check{Scope: ScopeMCPRead, ResourceID: "server_a"}.expand()

	allow, _, denied := evaluateGrants(grants, checks)
	require.Nil(t, allow)
	require.True(t, denied)
}

func TestEvaluateGrants_denyOnDifferentResourceDoesNotBlock(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, WildcardResource),
		NewDenyGrant(ScopeProjectRead, "proj_secret"),
	}
	checks := Check{Scope: ScopeProjectRead, ResourceID: "proj_normal"}.expand()

	allow, _, denied := evaluateGrants(grants, checks)
	require.NotNil(t, allow)
	require.False(t, denied)
}

func TestEvaluateGrants_denyOnDifferentScopeDoesNotBlock(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, "proj_123"),
		NewDenyGrant(ScopeProjectWrite, "proj_123"),
	}
	checks := Check{Scope: ScopeProjectRead, ResourceID: "proj_123"}.expand()

	allow, _, denied := evaluateGrants(grants, checks)
	require.NotNil(t, allow)
	require.False(t, denied)
}

func TestEvaluateGrants_noGrantsReturnsNilNotDenied(t *testing.T) {
	t.Parallel()

	checks := Check{Scope: ScopeProjectRead, ResourceID: "proj_123"}.expand()
	allow, _, denied := evaluateGrants(nil, checks)
	require.Nil(t, allow)
	require.False(t, denied)
}

func TestEvaluateGrants_onlyDenyGrantReturnsDenied(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewDenyGrant(ScopeProjectRead, "proj_123")}
	checks := Check{Scope: ScopeProjectRead, ResourceID: "proj_123"}.expand()

	allow, _, denied := evaluateGrants(grants, checks)
	require.Nil(t, allow)
	require.True(t, denied)
}

func TestFindMatchingGrant_denyBlocksMatch(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeOrgAdmin, WildcardResource),
		NewDenyGrant(ScopeOrgRead, "org_123"),
	}
	// org:read check expands to include org:admin and org:read.
	// The deny on org:read should block even though org:admin matches.
	grant, _ := findMatchingGrant(grants, Check{Scope: ScopeOrgRead, ResourceID: "org_123"}.expand())
	require.Nil(t, grant)
}

func TestFindMatchingGrant_wildcardDenyBlocksAll(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeMCPConnect, WildcardResource),
		NewDenyGrant(ScopeMCPConnect, WildcardResource),
	}
	grant, _ := findMatchingGrant(grants, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand())
	require.Nil(t, grant)
}

func TestFindMatchingGrant_denyDoesNotCrossScopes(t *testing.T) {
	t.Parallel()

	// Deny mcp:write on server_a should NOT block mcp:read on server_a.
	grants := []Grant{
		NewGrant(ScopeMCPRead, WildcardResource),
		NewDenyGrant(ScopeMCPWrite, "server_a"),
	}
	grant, _ := findMatchingGrant(grants, Check{Scope: ScopeMCPRead, ResourceID: "server_a"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsToScopedGrants_separatesAllowAndDeny(t *testing.T) {
	t.Parallel()

	rows := []Grant{
		NewGrant(ScopeProjectRead, "proj_a"),
		NewDenyGrant(ScopeProjectRead, "proj_b"),
		NewGrant(ScopeProjectRead, "proj_c"),
	}

	scoped := GrantsToScopedGrants(rows)

	// Should produce two entries for project:read — one allow, one deny.
	var allowGrant, denyGrant *ScopedGrant
	for _, sg := range scoped {
		if sg.Scope != string(ScopeProjectRead) {
			continue
		}
		if sg.Effect == PolicyEffectAllow {
			allowGrant = sg
		}
		if sg.Effect == PolicyEffectDeny {
			denyGrant = sg
		}
	}
	require.NotNil(t, allowGrant, "expected allow grant for project:read")
	require.NotNil(t, denyGrant, "expected deny grant for project:read")
	require.Len(t, allowGrant.Selectors, 2)
	require.Len(t, denyGrant.Selectors, 1)
}

func TestScopeExpansions_isDAG(t *testing.T) {
	t.Parallel()

	for start := range scopeExpansions {
		inStack := map[Scope]bool{}
		visited := map[Scope]bool{}
		var hasCycle func(s Scope) bool
		hasCycle = func(s Scope) bool {
			if inStack[s] {
				return true
			}
			if visited[s] {
				return false
			}
			visited[s] = true
			inStack[s] = true
			if slices.ContainsFunc(scopeExpansions[s], hasCycle) {
				return true
			}
			inStack[s] = false
			return false
		}
		require.False(t, hasCycle(start), "cycle detected in scopeExpansions from scope %q", start)
	}
}

func TestCalculateSubScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scope string
		want  []string
	}{
		{scope: string(ScopeOrgAdmin), want: []string{string(ScopeOrgRead)}},
		{scope: string(ScopeProjectWrite), want: []string{string(ScopeProjectRead)}},
		{scope: string(ScopeMCPWrite), want: []string{string(ScopeMCPConnect), string(ScopeMCPRead)}},
		{scope: string(ScopeMCPRead), want: []string{string(ScopeMCPConnect)}},
		{scope: string(ScopeOrgRead), want: []string{}},
		{scope: string(ScopeProjectRead), want: []string{}},
		{scope: string(ScopeRoot), want: []string{}},
		{scope: string(ScopeMCPConnect), want: []string{}},
		{scope: string(ScopeEnvironmentRead), want: []string{}},
		{scope: string(ScopeEnvironmentWrite), want: []string{string(ScopeEnvironmentRead)}},
	}
	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			t.Parallel()
			got := CalculateSubScopes(Scope(tt.scope))
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCalculateSubScopes_inverseOfScopeExpansions(t *testing.T) {
	t.Parallel()

	for lower, highers := range scopeExpansions {
		for _, h := range highers {
			require.Contains(t, CalculateSubScopes(h), string(lower),
				"higher scope %q should imply lower scope %q", h, lower)
		}
	}
}
