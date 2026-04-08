package access

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckExpand_orgRead(t *testing.T) {
	t.Parallel()

	checks := Check{Scope: ScopeOrgRead, ResourceID: "org_123"}.expand()

	require.Contains(t, checks, Check{Scope: ScopeRoot, ResourceID: WildcardResource})
	require.Contains(t, checks, Check{Scope: ScopeOrgAdmin, ResourceID: "org_123"})
	require.Contains(t, checks, Check{Scope: ScopeOrgAdmin, ResourceID: WildcardResource})
	require.Contains(t, checks, Check{Scope: ScopeOrgWrite, ResourceID: "org_123"})
	require.Contains(t, checks, Check{Scope: ScopeOrgWrite, ResourceID: WildcardResource})
	require.Contains(t, checks, Check{Scope: ScopeOrgRead, ResourceID: "org_123"})
	require.Contains(t, checks, Check{Scope: ScopeOrgRead, ResourceID: WildcardResource})
}

func TestCheckExpand_mcpConnect(t *testing.T) {
	t.Parallel()

	checks := Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand()

	require.Contains(t, checks, Check{Scope: ScopeRoot, ResourceID: WildcardResource})
	require.Contains(t, checks, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"})
	require.Contains(t, checks, Check{Scope: ScopeMCPConnect, ResourceID: WildcardResource})
	// mcp:connect is not implied by mcp:write — it is a distinct capability
	for _, c := range checks {
		require.NotEqual(t, ScopeMCPWrite, c.Scope)
		require.NotEqual(t, ScopeMCPRead, c.Scope)
	}
}

func TestGrantsHasAccess_orgAdminSatisfiesOrgRead(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeOrgAdmin, Resource: "org_123"}}}
	require.True(t, g.satisfies(Check{Scope: ScopeOrgRead, ResourceID: "org_123"}.expand()))
}

func TestGrantsHasAccess_orgAdminSatisfiesOrgWrite(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeOrgAdmin, Resource: "org_123"}}}
	require.True(t, g.satisfies(Check{Scope: ScopeOrgWrite, ResourceID: "org_123"}.expand()))
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
