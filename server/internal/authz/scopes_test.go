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
	require.True(t, grantsSatisfy(g, Check{Scope: ScopeOrgRead, ResourceID: "org_123"}.expand()))
}

func TestGrantsHasAccess_orgReadDoesNotSatisfyOrgAdmin(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeOrgRead, "org_123")}
	require.False(t, grantsSatisfy(g, Check{Scope: ScopeOrgAdmin, ResourceID: "org_123"}.expand()))
}

func TestGrantsHasAccess_buildWriteSatisfiesBuildRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeProjectWrite, "proj_123")}
	require.True(t, grantsSatisfy(g, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"}.expand()))
}

func TestGrantsHasAccess_buildReadDoesNotSatisfyBuildWrite(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeProjectRead, "proj_123")}
	require.False(t, grantsSatisfy(g, Check{Scope: ScopeProjectWrite, ResourceID: "proj_123"}.expand()))
}

func TestGrantsHasAccess_orgAdminDoesNotSatisfyBuildRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeOrgAdmin, "org_123")}
	require.False(t, grantsSatisfy(g, Check{Scope: ScopeProjectRead, ResourceID: "org_123"}.expand()))
}

func TestGrantsHasAccess_mcpConnectDoesNotSatisfyMCPRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeMCPConnect, "tool_a")}
	require.False(t, grantsSatisfy(g, Check{Scope: ScopeMCPRead, ResourceID: "tool_a"}.expand()))
}

func TestGrantsHasAccess_mcpReadSatisfiesMCPConnect(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeMCPRead, "tool_a")}
	require.True(t, grantsSatisfy(g, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand()))
}

func TestGrantsHasAccess_mcpWriteSatisfiesMCPConnect(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeMCPWrite, "tool_a")}
	require.True(t, grantsSatisfy(g, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand()))
}

func TestGrantsHasAccess_mcpWriteSatisfiesMCPRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeMCPWrite, "tool_a")}
	require.True(t, grantsSatisfy(g, Check{Scope: ScopeMCPRead, ResourceID: "tool_a"}.expand()))
}

func TestGrantsHasAccess_rootWildcardSatisfiesAnyScope(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeRoot, WildcardResource)}
	require.True(t, grantsSatisfy(g, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"}.expand()))
	require.True(t, grantsSatisfy(g, Check{Scope: ScopeOrgAdmin, ResourceID: "org_456"}.expand()))
	require.True(t, grantsSatisfy(g, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand()))
}

func TestGrantsHasAccess_wrongResourceNotSatisfied(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeOrgAdmin, "org_123")}
	require.False(t, grantsSatisfy(g, Check{Scope: ScopeOrgRead, ResourceID: "org_999"}.expand()))
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
