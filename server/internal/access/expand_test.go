package access

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckExpand_orgRead(t *testing.T) {
	t.Parallel()

	checks := Check{Scope: ScopeOrgRead, ResourceID: "org_123"}.expand()

	require.Contains(t, checks, Check{Scope: ScopeRoot, ResourceID: WildcardResource})
	require.Contains(t, checks, Check{Scope: ScopeOrgAdmin, ResourceID: "org_123"})
	require.Contains(t, checks, Check{Scope: ScopeOrgAdmin, ResourceID: WildcardResource})
	require.Contains(t, checks, Check{Scope: ScopeOrgRead, ResourceID: "org_123"})
	require.Contains(t, checks, Check{Scope: ScopeOrgRead, ResourceID: WildcardResource})
}

func TestCheckExpand_mcpConnect(t *testing.T) {
	t.Parallel()

	checks := Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand()

	require.Contains(t, checks, Check{Scope: ScopeRoot, ResourceID: WildcardResource})
	require.Contains(t, checks, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"})
	require.Contains(t, checks, Check{Scope: ScopeMCPConnect, ResourceID: WildcardResource})
	require.Contains(t, checks, Check{Scope: ScopeMCPRead, ResourceID: "tool_a"})
	require.Contains(t, checks, Check{Scope: ScopeMCPRead, ResourceID: WildcardResource})
	require.Contains(t, checks, Check{Scope: ScopeMCPWrite, ResourceID: "tool_a"})
	require.Contains(t, checks, Check{Scope: ScopeMCPWrite, ResourceID: WildcardResource})
}

func TestGrantsHasAccess_orgAdminSatisfiesOrgRead(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeOrgAdmin, Resource: "org_123"}}}
	require.True(t, g.satisfies(Check{Scope: ScopeOrgRead, ResourceID: "org_123"}.expand()))
}

func TestGrantsHasAccess_orgReadDoesNotSatisfyOrgAdmin(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeOrgRead, Resource: "org_123"}}}
	require.False(t, g.satisfies(Check{Scope: ScopeOrgAdmin, ResourceID: "org_123"}.expand()))
}

func TestGrantsHasAccess_buildWriteSatisfiesBuildRead(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeBuildWrite, Resource: "proj_123"}}}
	require.True(t, g.satisfies(Check{Scope: ScopeBuildRead, ResourceID: "proj_123"}.expand()))
}

func TestGrantsHasAccess_buildReadDoesNotSatisfyBuildWrite(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeBuildRead, Resource: "proj_123"}}}
	require.False(t, g.satisfies(Check{Scope: ScopeBuildWrite, ResourceID: "proj_123"}.expand()))
}

func TestGrantsHasAccess_orgAdminDoesNotSatisfyBuildRead(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeOrgAdmin, Resource: "org_123"}}}
	require.False(t, g.satisfies(Check{Scope: ScopeBuildRead, ResourceID: "org_123"}.expand()))
}

func TestGrantsHasAccess_mcpConnectDoesNotSatisfyMCPRead(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeMCPConnect, Resource: "tool_a"}}}
	require.False(t, g.satisfies(Check{Scope: ScopeMCPRead, ResourceID: "tool_a"}.expand()))
}

func TestGrantsHasAccess_mcpReadSatisfiesMCPConnect(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeMCPRead, Resource: "tool_a"}}}
	require.True(t, g.satisfies(Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand()))
}

func TestGrantsHasAccess_mcpWriteSatisfiesMCPConnect(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeMCPWrite, Resource: "tool_a"}}}
	require.True(t, g.satisfies(Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand()))
}

func TestGrantsHasAccess_mcpWriteSatisfiesMCPRead(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeMCPWrite, Resource: "tool_a"}}}
	require.True(t, g.satisfies(Check{Scope: ScopeMCPRead, ResourceID: "tool_a"}.expand()))
}

func TestGrantsHasAccess_rootWildcardSatisfiesAnyScope(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeRoot, Resource: WildcardResource}}}
	require.True(t, g.satisfies(Check{Scope: ScopeBuildRead, ResourceID: "proj_123"}.expand()))
	require.True(t, g.satisfies(Check{Scope: ScopeOrgAdmin, ResourceID: "org_456"}.expand()))
	require.True(t, g.satisfies(Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand()))
}

func TestGrantsHasAccess_wrongResourceNotSatisfied(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeOrgAdmin, Resource: "org_123"}}}
	require.False(t, g.satisfies(Check{Scope: ScopeOrgRead, ResourceID: "org_999"}.expand()))
}

func TestScopeExpansions_isDAG(t *testing.T) {
	t.Parallel()

	// DFS from every scope; assert no scope is reachable from itself.
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
