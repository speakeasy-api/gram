package authz

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckExpand_orgRead(t *testing.T) {
	t.Parallel()

	checks := Check{Scope: ScopeOrgRead, ResourceID: "org_123"}.expand()

	require.Contains(t, checks, Check{Scope: ScopeRoot, ResourceID: "org_123", expanded: true})
	require.Contains(t, checks, Check{Scope: ScopeOrgAdmin, ResourceID: "org_123", expanded: true})
	require.Contains(t, checks, Check{Scope: ScopeOrgRead, ResourceID: "org_123"})
	// No wildcard resource variants — selector matching handles that natively.
	require.NotContains(t, checks, Check{Scope: ScopeOrgAdmin, ResourceID: WildcardResource, expanded: true})
}

func TestCheckExpand_mcpConnect(t *testing.T) {
	t.Parallel()

	checks := Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand()

	require.Contains(t, checks, Check{Scope: ScopeRoot, ResourceID: "tool_a", expanded: true})
	require.Contains(t, checks, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"})
	require.Contains(t, checks, Check{Scope: ScopeMCPRead, ResourceID: "tool_a", expanded: true})
	require.Contains(t, checks, Check{Scope: ScopeMCPWrite, ResourceID: "tool_a", expanded: true})
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

// --- Deny scope isolation tests ---
// RFC: "Deny applies to the denied scope and any explicitly defined deny
// sub-scopes." Deny must never leak into unrelated scope families.

func TestDeny_projectWriteDenyDoesNotBlockProjectRead(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, "proj_123"),
		NewGrant(ScopeProjectWrite, "proj_123"),
		NewDenyGrant(ScopeProjectWrite, "proj_123"),
	}
	// project:read should still work even though project:write is denied.
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)

	// project:write itself should be denied.
	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeProjectWrite, ResourceID: "proj_123"}.expand())
	require.Nil(t, allow)
	require.True(t, denied)
}

func TestDeny_projectWriteDenyWithInheritedRead(t *testing.T) {
	t.Parallel()

	// User has ONLY project:write (which inherits project:read via scope expansion).
	// Deny project:write should block writes but NOT block the inherited read.
	grants := []Grant{
		NewGrant(ScopeProjectWrite, "proj_123"),
		NewDenyGrant(ScopeProjectWrite, "proj_123"),
	}

	// project:read check should still work via inheritance from the (now-denied) write grant.
	// The allow on project:write satisfies the expanded project:read check,
	// and the deny on project:write does not cascade to project:read.
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"}.expand())
	require.NotNil(t, allow, "inherited project:read should still be allowed when project:write is denied")
	require.False(t, denied)

	// project:write itself should be denied.
	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeProjectWrite, ResourceID: "proj_123"}.expand())
	require.Nil(t, allow)
	require.True(t, denied)
}

func TestDeny_orgAdminDenyDoesNotBlockOrgRead(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeOrgRead, "org_1"),
		NewGrant(ScopeOrgAdmin, "org_1"),
		NewDenyGrant(ScopeOrgAdmin, "org_1"),
	}
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeOrgRead, ResourceID: "org_1"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)

	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeOrgAdmin, ResourceID: "org_1"}.expand())
	require.Nil(t, allow)
	require.True(t, denied)
}

func TestDeny_mcpWriteDenyDoesNotBlockMcpReadOrConnect(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeMCPRead, WildcardResource),
		NewGrant(ScopeMCPConnect, WildcardResource),
		NewDenyGrant(ScopeMCPWrite, "server_a"),
	}
	// mcp:read and mcp:connect should be unaffected by mcp:write deny.
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeMCPRead, ResourceID: "server_a"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)

	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeMCPConnect, ResourceID: "server_a"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)
}

func TestDeny_envWriteDenyDoesNotBlockEnvRead(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeEnvironmentRead, "env_1"),
		NewGrant(ScopeEnvironmentWrite, "env_1"),
		NewDenyGrant(ScopeEnvironmentWrite, "env_1"),
	}
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeEnvironmentRead, ResourceID: "env_1"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)

	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeEnvironmentWrite, ResourceID: "env_1"}.expand())
	require.Nil(t, allow)
	require.True(t, denied)
}

// --- Deny-wins truth table (RFC §Enforcement Semantics) ---
// | allow match | deny match | result |
// |-------------|------------|--------|
// | no          | no         | deny   |
// | yes         | no         | allow  |
// | no          | yes        | deny   |
// | yes         | yes        | deny   |

func TestDeny_truthTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		grants    []Grant
		check     Check
		wantAllow bool
		wantDeny  bool
	}{
		{
			name:      "no allow, no deny → nil, not denied",
			grants:    []Grant{},
			check:     Check{Scope: ScopeProjectRead, ResourceID: "proj_1"},
			wantAllow: false,
			wantDeny:  false,
		},
		{
			name:      "allow match, no deny → allowed",
			grants:    []Grant{NewGrant(ScopeProjectRead, "proj_1")},
			check:     Check{Scope: ScopeProjectRead, ResourceID: "proj_1"},
			wantAllow: true,
			wantDeny:  false,
		},
		{
			name:      "no allow, deny match → denied",
			grants:    []Grant{NewDenyGrant(ScopeProjectRead, "proj_1")},
			check:     Check{Scope: ScopeProjectRead, ResourceID: "proj_1"},
			wantAllow: false,
			wantDeny:  true,
		},
		{
			name: "allow match + deny match → denied (deny wins)",
			grants: []Grant{
				NewGrant(ScopeProjectRead, WildcardResource),
				NewDenyGrant(ScopeProjectRead, "proj_1"),
			},
			check:     Check{Scope: ScopeProjectRead, ResourceID: "proj_1"},
			wantAllow: false,
			wantDeny:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			allow, _, denied := evaluateGrants(tt.grants, tt.check.expand())
			if tt.wantAllow {
				require.NotNil(t, allow)
			} else {
				require.Nil(t, allow)
			}
			require.Equal(t, tt.wantDeny, denied)
		})
	}
}

// --- Multi-role composition (RFC §Multiple Allows and Denies) ---

func TestDeny_multiRoleComposition_denyFromSecondRoleBlocks(t *testing.T) {
	t.Parallel()

	// Role A: allow mcp:connect on *
	// Role B: deny mcp:connect on tool_dangerous
	// Merged: tool_dangerous should be denied.
	grants := []Grant{
		NewGrant(ScopeMCPConnect, WildcardResource),
		NewDenyGrant(ScopeMCPConnect, "tool_dangerous"),
	}

	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeMCPConnect, ResourceID: "tool_safe"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)

	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeMCPConnect, ResourceID: "tool_dangerous"}.expand())
	require.Nil(t, allow)
	require.True(t, denied)
}

func TestDeny_multiRoleComposition_allowOnlyRolePermitsAll(t *testing.T) {
	t.Parallel()

	// User with only the allow role — no deny. Everything should be permitted.
	grants := []Grant{
		NewGrant(ScopeMCPConnect, WildcardResource),
	}
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeMCPConnect, ResourceID: "tool_dangerous"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)
}

// --- Wildcard deny tests ---

func TestDeny_wildcardDenyBlocksAllResources(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, WildcardResource),
		NewDenyGrant(ScopeProjectRead, WildcardResource),
	}
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeProjectRead, ResourceID: "proj_any"}.expand())
	require.Nil(t, allow)
	require.True(t, denied)
}

func TestDeny_specificDenyDoesNotBlockOtherResources(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, WildcardResource),
		NewDenyGrant(ScopeProjectRead, "proj_secret"),
	}
	// proj_secret denied
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeProjectRead, ResourceID: "proj_secret"}.expand())
	require.Nil(t, allow)
	require.True(t, denied)

	// proj_public still allowed
	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeProjectRead, ResourceID: "proj_public"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)
}

// --- Server negation: exclude one MCP server, allow all others ---
// The core use case: wildcard allow + targeted deny. Future servers
// automatically get the allow grant without needing grant updates.

func TestDeny_excludeOneServerAllowAllOthers(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeMCPConnect, WildcardResource),
		NewDenyGrant(ScopeMCPConnect, "server_dangerous"),
	}

	tests := []struct {
		server    string
		wantAllow bool
		wantDeny  bool
	}{
		{"server_safe", true, false},
		{"server_new_future", true, false}, // future server auto-allowed
		{"server_also_new", true, false},   // another future server
		{"server_dangerous", false, true},  // excluded server denied
	}
	for _, tt := range tests {
		t.Run(tt.server, func(t *testing.T) {
			t.Parallel()
			allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeMCPConnect, ResourceID: tt.server}.expand())
			if tt.wantAllow {
				require.NotNil(t, allow)
			} else {
				require.Nil(t, allow)
			}
			require.Equal(t, tt.wantDeny, denied)
		})
	}
}

// --- Tool negation: exclude one tool, allow all others ---
// Uses dimension-based selectors. Wildcard tool allow + specific tool deny.
// New tools added later are automatically permitted.

func TestDeny_excludeOneToolAllowAllOthers(t *testing.T) {
	t.Parallel()

	allowSel := Selector{"resource_kind": "mcp", "resource_id": "server_1", "tool": "*"}
	denySel := Selector{"resource_kind": "mcp", "resource_id": "server_1", "tool": "dangerous_tool"}

	grants := []Grant{
		NewGrantWithSelector(ScopeMCPConnect, allowSel),
		NewDenyGrantWithSelector(ScopeMCPConnect, denySel),
	}

	tests := []struct {
		tool      string
		wantAllow bool
		wantDeny  bool
	}{
		{"safe_tool", true, false},
		{"another_tool", true, false},
		{"future_tool_not_yet_added", true, false}, // future tool auto-allowed
		{"dangerous_tool", false, true},            // excluded tool denied
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			t.Parallel()
			check := Check{
				Scope:      ScopeMCPConnect,
				ResourceID: "server_1",
				Dimensions: map[string]string{"tool": tt.tool},
			}
			allow, _, denied := evaluateGrants(grants, check.expand())
			if tt.wantAllow {
				require.NotNil(t, allow)
			} else {
				require.Nil(t, allow)
			}
			require.Equal(t, tt.wantDeny, denied)
		})
	}
}

// --- Multiple exclusions: deny several servers/tools, allow the rest ---

func TestDeny_excludeMultipleServersAllowRest(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeMCPConnect, WildcardResource),
		NewDenyGrant(ScopeMCPConnect, "server_banned_1"),
		NewDenyGrant(ScopeMCPConnect, "server_banned_2"),
	}

	tests := []struct {
		server    string
		wantAllow bool
		wantDeny  bool
	}{
		{"server_ok", true, false},
		{"server_future", true, false},
		{"server_banned_1", false, true},
		{"server_banned_2", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.server, func(t *testing.T) {
			t.Parallel()
			allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeMCPConnect, ResourceID: tt.server}.expand())
			if tt.wantAllow {
				require.NotNil(t, allow)
			} else {
				require.Nil(t, allow)
			}
			require.Equal(t, tt.wantDeny, denied)
		})
	}
}

func TestDeny_excludeMultipleToolsAllowRest(t *testing.T) {
	t.Parallel()

	allowSel := Selector{"resource_kind": "mcp", "resource_id": "server_1", "tool": "*"}
	deny1 := Selector{"resource_kind": "mcp", "resource_id": "server_1", "tool": "rm"}
	deny2 := Selector{"resource_kind": "mcp", "resource_id": "server_1", "tool": "drop_table"}

	grants := []Grant{
		NewGrantWithSelector(ScopeMCPConnect, allowSel),
		NewDenyGrantWithSelector(ScopeMCPConnect, deny1),
		NewDenyGrantWithSelector(ScopeMCPConnect, deny2),
	}

	tests := []struct {
		tool      string
		wantAllow bool
		wantDeny  bool
	}{
		{"ls", true, false},
		{"cat", true, false},
		{"new_tool_added_later", true, false},
		{"rm", false, true},
		{"drop_table", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			t.Parallel()
			check := Check{
				Scope:      ScopeMCPConnect,
				ResourceID: "server_1",
				Dimensions: map[string]string{"tool": tt.tool},
			}
			allow, _, denied := evaluateGrants(grants, check.expand())
			if tt.wantAllow {
				require.NotNil(t, allow)
			} else {
				require.Nil(t, allow)
			}
			require.Equal(t, tt.wantDeny, denied)
		})
	}
}

// --- Server exclusion does not affect read/write scopes ---
// Denying mcp:connect on a server should not block mcp:read on that server.

func TestDeny_serverConnectDenyDoesNotBlockReadOrWrite(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeMCPRead, WildcardResource),
		NewGrant(ScopeMCPWrite, WildcardResource),
		NewGrant(ScopeMCPConnect, WildcardResource),
		NewDenyGrant(ScopeMCPConnect, "server_dangerous"),
	}

	// mcp:connect denied on server_dangerous
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeMCPConnect, ResourceID: "server_dangerous"}.expand())
	require.Nil(t, allow)
	require.True(t, denied)

	// mcp:read still works on same server
	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeMCPRead, ResourceID: "server_dangerous"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)

	// mcp:write still works on same server
	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeMCPWrite, ResourceID: "server_dangerous"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)
}

// --- Dimensionless connect probe must not be blocked by tool-scoped deny ---
// Production MCP connect checks carry no tool/disposition dimensions.
// A deny scoped to a specific tool must NOT block the server-level probe.

func TestDeny_toolDenyDoesNotBlockDimensionlessConnectProbe(t *testing.T) {
	t.Parallel()

	allowSel := Selector{"resource_kind": "mcp", "resource_id": "toolset_1"}
	denySel := Selector{"resource_kind": "mcp", "resource_id": "toolset_1", "tool": "dangerous_tool"}

	grants := []Grant{
		NewGrantWithSelector(ScopeMCPConnect, allowSel),
		NewDenyGrantWithSelector(ScopeMCPConnect, denySel),
	}

	// Server-level connect probe (no tool dimension) — must be ALLOWED.
	// This is the exact check production runs at mcp/impl.go and xmcp/handler.go.
	connectCheck := Check{Scope: ScopeMCPConnect, ResourceID: "toolset_1"}
	allow, _, denied := evaluateGrants(grants, connectCheck.expand())
	require.NotNil(t, allow, "dimensionless connect probe must not be blocked by tool-scoped deny")
	require.False(t, denied)

	// Tool-level check WITH the dimension — deny must fire.
	toolCheck := Check{
		Scope:      ScopeMCPConnect,
		ResourceID: "toolset_1",
		Dimensions: map[string]string{"tool": "dangerous_tool"},
	}
	allow, _, denied = evaluateGrants(grants, toolCheck.expand())
	require.Nil(t, allow)
	require.True(t, denied)

	// Different tool — allowed.
	safeCheck := Check{
		Scope:      ScopeMCPConnect,
		ResourceID: "toolset_1",
		Dimensions: map[string]string{"tool": "safe_tool"},
	}
	allow, _, denied = evaluateGrants(grants, safeCheck.expand())
	require.NotNil(t, allow)
	require.False(t, denied)
}

func TestDeny_dispositionDenyDoesNotBlockDimensionlessProbe(t *testing.T) {
	t.Parallel()

	allowSel := Selector{"resource_kind": "mcp", "resource_id": "srv"}
	denySel := Selector{"resource_kind": "mcp", "resource_id": "srv", "disposition": "destructive"}

	grants := []Grant{
		NewGrantWithSelector(ScopeMCPConnect, allowSel),
		NewDenyGrantWithSelector(ScopeMCPConnect, denySel),
	}

	// Dimensionless connect — allowed.
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeMCPConnect, ResourceID: "srv"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)

	// Destructive disposition — denied.
	allow, _, denied = evaluateGrants(grants, Check{
		Scope:      ScopeMCPConnect,
		ResourceID: "srv",
		Dimensions: map[string]string{"disposition": "destructive"},
	}.expand())
	require.Nil(t, allow)
	require.True(t, denied)

	// Read-only disposition — allowed.
	allow, _, denied = evaluateGrants(grants, Check{
		Scope:      ScopeMCPConnect,
		ResourceID: "srv",
		Dimensions: map[string]string{"disposition": "read_only"},
	}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)
}

// --- Grant order independence (RFC: "unordered set") ---

func TestDeny_orderIndependence(t *testing.T) {
	t.Parallel()

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_1"}

	// deny first, allow second
	grants1 := []Grant{
		NewDenyGrant(ScopeProjectRead, "proj_1"),
		NewGrant(ScopeProjectRead, WildcardResource),
	}
	allow1, _, denied1 := evaluateGrants(grants1, check.expand())

	// allow first, deny second
	grants2 := []Grant{
		NewGrant(ScopeProjectRead, WildcardResource),
		NewDenyGrant(ScopeProjectRead, "proj_1"),
	}
	allow2, _, denied2 := evaluateGrants(grants2, check.expand())

	require.Nil(t, allow1)
	require.True(t, denied1)
	require.Nil(t, allow2)
	require.True(t, denied2)
}

// --- Multiple deny grants on same scope, different resources ---

func TestDeny_multipleDenyGrants(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, WildcardResource),
		NewDenyGrant(ScopeProjectRead, "proj_a"),
		NewDenyGrant(ScopeProjectRead, "proj_b"),
	}

	// proj_a denied
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeProjectRead, ResourceID: "proj_a"}.expand())
	require.Nil(t, allow)
	require.True(t, denied)

	// proj_b denied
	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeProjectRead, ResourceID: "proj_b"}.expand())
	require.Nil(t, allow)
	require.True(t, denied)

	// proj_c still allowed
	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeProjectRead, ResourceID: "proj_c"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)
}

// --- Default effect (backward compatibility) ---

func TestDeny_emptyEffectDefaultsToAllow(t *testing.T) {
	t.Parallel()

	// Grant with empty Effect should behave as allow (backward compat).
	grants := []Grant{{
		Scope:    ScopeProjectRead,
		Effect:   "",
		Selector: NewSelector(ScopeProjectRead, "proj_1"),
	}}
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeProjectRead, ResourceID: "proj_1"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)
}

// --- Cross-family isolation ---
// Deny on one scope family must never affect a different family.

func TestDeny_crossFamilyIsolation(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, WildcardResource),
		NewGrant(ScopeMCPRead, WildcardResource),
		NewGrant(ScopeOrgRead, WildcardResource),
		NewDenyGrant(ScopeMCPRead, "server_x"),
	}

	// mcp:read on server_x denied
	allow, _, denied := evaluateGrants(grants, Check{Scope: ScopeMCPRead, ResourceID: "server_x"}.expand())
	require.Nil(t, allow)
	require.True(t, denied)

	// project:read on server_x (as resource) unaffected
	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeProjectRead, ResourceID: "server_x"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)

	// org:read unaffected
	allow, _, denied = evaluateGrants(grants, Check{Scope: ScopeOrgRead, ResourceID: "org_1"}.expand())
	require.NotNil(t, allow)
	require.False(t, denied)
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
