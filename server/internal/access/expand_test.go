package access

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckExpand_orgRead(t *testing.T) {
	t.Parallel()

	checks := Check{Scope: ScopeOrgRead, ResourceID: "org_123"}.Expand()

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

	checks := Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.Expand()

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
	require.True(t, g.hasAccess(ScopeOrgRead, "org_123"))
}

func TestGrantsHasAccess_orgAdminSatisfiesOrgWrite(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeOrgAdmin, Resource: "org_123"}}}
	require.True(t, g.hasAccess(ScopeOrgWrite, "org_123"))
}

func TestGrantsHasAccess_orgReadDoesNotSatisfyOrgAdmin(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeOrgRead, Resource: "org_123"}}}
	require.False(t, g.hasAccess(ScopeOrgAdmin, "org_123"))
}

func TestGrantsHasAccess_buildWriteSatisfiesBuildRead(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeBuildWrite, Resource: "proj_123"}}}
	require.True(t, g.hasAccess(ScopeBuildRead, "proj_123"))
}

func TestGrantsHasAccess_buildReadDoesNotSatisfyBuildWrite(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeBuildRead, Resource: "proj_123"}}}
	require.False(t, g.hasAccess(ScopeBuildWrite, "proj_123"))
}

func TestGrantsHasAccess_orgAdminDoesNotSatisfyBuildRead(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeOrgAdmin, Resource: "org_123"}}}
	require.False(t, g.hasAccess(ScopeBuildRead, "org_123"))
}

func TestGrantsHasAccess_mcpConnectDoesNotSatisfyMCPRead(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeMCPConnect, Resource: "tool_a"}}}
	require.False(t, g.hasAccess(ScopeMCPRead, "tool_a"))
}

func TestGrantsHasAccess_mcpWriteSatisfiesMCPRead(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeMCPWrite, Resource: "tool_a"}}}
	require.True(t, g.hasAccess(ScopeMCPRead, "tool_a"))
}

func TestGrantsHasAccess_rootWildcardSatisfiesAnyScope(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeRoot, Resource: WildcardResource}}}
	require.True(t, g.hasAccess(ScopeBuildRead, "proj_123"))
	require.True(t, g.hasAccess(ScopeOrgAdmin, "org_456"))
	require.True(t, g.hasAccess(ScopeMCPConnect, "tool_a"))
}

func TestGrantsHasAccess_wrongResourceNotSatisfied(t *testing.T) {
	t.Parallel()

	g := &Grants{rows: []Grant{{Scope: ScopeOrgAdmin, Resource: "org_123"}}}
	require.False(t, g.hasAccess(ScopeOrgRead, "org_999"))
}
