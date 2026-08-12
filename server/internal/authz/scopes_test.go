package authz

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScopeParts(t *testing.T) {
	t.Parallel()

	require.Equal(t, ScopeParts{Resource: "project", Action: "read"}, ScopeProjectRead.Parts())
	require.Equal(t, ScopeParts{Resource: "project", Action: "blocked_write"}, ScopeProjectBlockedWrite.Parts())
	require.Equal(t, ScopeParts{Resource: "risk_policy", Action: "evaluate"}, ScopeRiskPolicyEvaluate.Parts())
	require.Equal(t, ScopeParts{Resource: "risk_policy", Action: "bypass"}, ScopeRiskPolicyBypass.Parts())
	require.Equal(t, ScopeParts{Resource: "root", Action: ""}, ScopeRoot.Parts())
}

func TestUserVisibleScopesCoverDefinedPublicScopes(t *testing.T) {
	t.Parallel()

	seen := make(map[Scope]struct{}, len(scopeVisibilityByScope))
	for scope, visibility := range scopeVisibilityByScope {
		if visibility != scopeVisibilityUserVisible {
			continue
		}
		require.NotEqual(t, ScopeRoot, scope)
		require.Contains(t, scopeExpansions, scope)
		require.NotContains(t, seen, scope)
		seen[scope] = struct{}{}
	}

	for scope := range scopeExpansions {
		if scope == ScopeRoot {
			continue
		}
		if scopeVisibilityByScope[scope] == scopeVisibilityInternal {
			continue
		}
		require.Contains(t, seen, scope)
	}
}

func TestScopeVisibilityCoversKnownScopes(t *testing.T) {
	t.Parallel()

	for scope := range scopeExpansions {
		if scope == ScopeRoot {
			continue
		}

		require.Contains(t, scopeVisibilityByScope, scope)
	}
}

func TestScopeExclusionsCoversKnownScopes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		scope    Scope
		expected Scope
	}{
		{scope: ScopeOrgRead, expected: ScopeOrgBlockedRead},
		{scope: ScopeOrgAdmin, expected: ScopeOrgBlockedAdmin},
		{scope: ScopeProjectRead, expected: ScopeProjectBlockedRead},
		{scope: ScopeProjectWrite, expected: ScopeProjectBlockedWrite},
		{scope: ScopeMCPRead, expected: ScopeMCPBlockedRead},
		{scope: ScopeMCPWrite, expected: ScopeMCPBlockedWrite},
		{scope: ScopeMCPConnect, expected: ScopeMCPBlockedConnect},
		{scope: ScopeEnvironmentRead, expected: ScopeEnvironmentBlockedRead},
		{scope: ScopeEnvironmentWrite, expected: ScopeEnvironmentBlockedWrite},
		{scope: ScopeSkillRead, expected: ScopeSkillBlockedRead},
		{scope: ScopeSkillWrite, expected: ScopeSkillBlockedWrite},
		{scope: ScopeRiskPolicyEvaluate, expected: ScopeRiskPolicyBypass},
	}

	for _, tc := range cases {
		exclusion, ok := ExclusionScopeFor(tc.scope)
		require.True(t, ok)
		require.Equal(t, tc.expected, exclusion)
	}

	_, ok := ExclusionScopeFor(ScopeRiskPolicyBypass)
	require.False(t, ok)
}

func TestBlocklistScopeExpansions(t *testing.T) {
	t.Parallel()

	require.Equal(t, []Scope{ScopeOrgBlockedRead}, scopeExpansions[ScopeOrgBlockedAdmin])
	require.Equal(t, []Scope{ScopeProjectBlockedRead}, scopeExpansions[ScopeProjectBlockedWrite])
	require.Equal(t, []Scope{ScopeMCPBlockedConnect}, scopeExpansions[ScopeMCPBlockedRead])
	require.Equal(t, []Scope{ScopeMCPBlockedRead, ScopeMCPBlockedConnect}, scopeExpansions[ScopeMCPBlockedWrite])
	require.Equal(t, []Scope{ScopeEnvironmentBlockedRead}, scopeExpansions[ScopeEnvironmentBlockedWrite])
	require.Equal(t, []Scope{ScopeSkillBlockedRead}, scopeExpansions[ScopeSkillBlockedWrite])
}

func TestCalculateSubScopesExcludesInternalBlocklistScopes(t *testing.T) {
	t.Parallel()

	require.Empty(t, CalculateSubScopes(ScopeMCPBlockedConnect))
	require.Empty(t, CalculateSubScopes(ScopeMCPBlockedRead))
	require.Empty(t, CalculateSubScopes(ScopeProjectBlockedRead))
}

func TestSystemRoleAdminExcludesRiskPolicyScopes(t *testing.T) {
	t.Parallel()

	adminGrants := SystemRoleGrants[SystemRoleAdmin]
	adminScopes := make([]string, 0, len(adminGrants))
	for _, grant := range adminGrants {
		adminScopes = append(adminScopes, grant.Scope)
	}

	require.NotContains(t, adminScopes, string(ScopeRiskPolicyEvaluate))
	require.NotContains(t, adminScopes, string(ScopeRiskPolicyBypass))
}

// chat:read is not a default for any system role — it must be granted
// explicitly. Admins read their own sessions via owner-matching like everyone
// else; reading other members' transcripts requires an explicit chat:read.
func TestSystemRoleAdminExcludesChatRead(t *testing.T) {
	t.Parallel()

	for _, grant := range SystemRoleGrants[SystemRoleAdmin] {
		require.NotEqual(t, string(ScopeChatRead), grant.Scope)
	}
	for _, grant := range SystemRoleGrants[SystemRoleMember] {
		require.NotEqual(t, string(ScopeChatRead), grant.Scope)
	}
}

// environment:read is not a member default: environment values include secrets,
// so viewing them must be granted explicitly via a custom role. Admins retain
// environment:read/write via adminScopes.
func TestSystemRoleMemberExcludesEnvironmentRead(t *testing.T) {
	t.Parallel()

	memberScopes := make([]string, 0, len(SystemRoleGrants[SystemRoleMember]))
	for _, grant := range SystemRoleGrants[SystemRoleMember] {
		memberScopes = append(memberScopes, grant.Scope)
	}
	require.NotContains(t, memberScopes, string(ScopeEnvironmentRead))
	require.NotContains(t, memberScopes, string(ScopeEnvironmentWrite))

	adminScopes := make([]string, 0, len(SystemRoleGrants[SystemRoleAdmin]))
	for _, grant := range SystemRoleGrants[SystemRoleAdmin] {
		adminScopes = append(adminScopes, grant.Scope)
	}
	require.Contains(t, adminScopes, string(ScopeEnvironmentRead))
}

func TestSystemRolesIncludeSkillScopes(t *testing.T) {
	t.Parallel()

	admin := make([]string, 0, len(SystemRoleGrants[SystemRoleAdmin]))
	for _, grant := range SystemRoleGrants[SystemRoleAdmin] {
		admin = append(admin, grant.Scope)
	}
	require.Contains(t, admin, string(ScopeSkillRead))
	require.Contains(t, admin, string(ScopeSkillWrite))

	member := make([]string, 0, len(SystemRoleGrants[SystemRoleMember]))
	for _, grant := range SystemRoleGrants[SystemRoleMember] {
		member = append(member, grant.Scope)
	}
	require.Contains(t, member, string(ScopeSkillRead))
	require.NotContains(t, member, string(ScopeSkillWrite))
}

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
	grant, _ := matchingGrant(g, Check{Scope: ScopeOrgRead, ResourceID: "org_123"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsHasAccess_orgReadDoesNotSatisfyOrgAdmin(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeOrgRead, "org_123")}
	grant, _ := matchingGrant(g, Check{Scope: ScopeOrgAdmin, ResourceID: "org_123"}.expand())
	require.Nil(t, grant)
}

func TestGrantsHasAccess_buildWriteSatisfiesBuildRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeProjectWrite, "proj_123")}
	grant, _ := matchingGrant(g, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsHasAccess_buildReadDoesNotSatisfyBuildWrite(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeProjectRead, "proj_123")}
	grant, _ := matchingGrant(g, Check{Scope: ScopeProjectWrite, ResourceID: "proj_123"}.expand())
	require.Nil(t, grant)
}

func TestGrantsHasAccess_orgAdminDoesNotSatisfyBuildRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeOrgAdmin, "org_123")}
	grant, _ := matchingGrant(g, Check{Scope: ScopeProjectRead, ResourceID: "org_123"}.expand())
	require.Nil(t, grant)
}

func TestGrantsHasAccess_mcpConnectDoesNotSatisfyMCPRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeMCPConnect, "tool_a")}
	grant, _ := matchingGrant(g, Check{Scope: ScopeMCPRead, ResourceID: "tool_a"}.expand())
	require.Nil(t, grant)
}

func TestGrantsHasAccess_mcpReadSatisfiesMCPConnect(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeMCPRead, "tool_a")}
	grant, _ := matchingGrant(g, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsHasAccess_mcpWriteSatisfiesMCPConnect(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeMCPWrite, "tool_a")}
	grant, _ := matchingGrant(g, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsHasAccess_mcpWriteSatisfiesMCPRead(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeMCPWrite, "tool_a")}
	grant, _ := matchingGrant(g, Check{Scope: ScopeMCPRead, ResourceID: "tool_a"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsHasAccess_rootWildcardSatisfiesAnyScope(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeRoot, WildcardResource)}

	grant, _ := matchingGrant(g, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"}.expand())
	require.NotNil(t, grant)

	grant, _ = matchingGrant(g, Check{Scope: ScopeOrgAdmin, ResourceID: "org_456"}.expand())
	require.NotNil(t, grant)

	grant, _ = matchingGrant(g, Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"}.expand())
	require.NotNil(t, grant)

	grant, _ = matchingGrant(g, Check{Scope: ScopeEnvironmentRead, ResourceID: "env_a"}.expand())
	require.NotNil(t, grant)
}

func TestGrantsHasAccess_wrongResourceNotSatisfied(t *testing.T) {
	t.Parallel()

	g := []Grant{NewGrant(ScopeOrgAdmin, "org_123")}
	grant, _ := matchingGrant(g, Check{Scope: ScopeOrgRead, ResourceID: "org_999"}.expand())
	require.Nil(t, grant)
}

func TestMatchingGrant_riskPolicyBypassRequiresCheckDimensionsForScopedGrant(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrantWithSelector(ScopeRiskPolicyBypass, Selector{
		SelectorKeyResourceKind: ResourceKindRiskPolicy,
		SelectorKeyResourceID:   "policy_123",
		SelectorKeyServerURL:    "https://api.example.com",
	})}
	checks := RiskPolicyBypassCheck("policy_123", RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""}).expand()

	grant, _ := matchingGrant(grants, checks)
	require.Nil(t, grant)
}

func TestMatchingGrant_riskPolicyBypassWholePolicyGrantMatchesScopedCheck(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrant(ScopeRiskPolicyBypass, "policy_123")}
	checks := RiskPolicyBypassCheck("policy_123", RiskPolicyDimensions{ServerURL: "https://api.example.com", ServerIdentity: ""}).expand()

	grant, _ := matchingGrant(grants, checks)
	require.NotNil(t, grant)
}

func TestMatchingGrant_riskPolicyBypassScopedGrantMatchesScopedCheck(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrantWithSelector(ScopeRiskPolicyBypass, Selector{
		SelectorKeyResourceKind: ResourceKindRiskPolicy,
		SelectorKeyResourceID:   "policy_123",
		SelectorKeyServerURL:    "https://api.example.com",
	})}
	checks := RiskPolicyBypassCheck("policy_123", RiskPolicyDimensions{ServerURL: "https://api.example.com", ServerIdentity: ""}).expand()

	grant, _ := matchingGrant(grants, checks)
	require.NotNil(t, grant)
}

func TestMatchingGrant_riskPolicyBypassServerIdentityGrantMatchesScopedCheck(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrantWithSelector(ScopeRiskPolicyBypass, Selector{
		SelectorKeyResourceKind:   ResourceKindRiskPolicy,
		SelectorKeyResourceID:     "policy_123",
		SelectorKeyServerIdentity: "github",
	})}
	checks := RiskPolicyBypassCheck("policy_123", RiskPolicyDimensions{ServerURL: "", ServerIdentity: "github"}).expand()

	grant, _ := matchingGrant(grants, checks)
	require.NotNil(t, grant)
}

func TestGrantsToScopedGrants_groupsSelectorsByScope(t *testing.T) {
	t.Parallel()

	rows := []Grant{
		NewGrant(ScopeProjectRead, "proj_a"),
		NewGrant(ScopeProjectRead, "proj_b"),
		NewGrant(ScopeMCPConnect, "server_a"),
	}

	require.Equal(t, []*ScopedGrant{
		{
			Scope:     string(ScopeMCPConnect),
			SubScopes: []string{},
			Selectors: []Selector{NewSelector(ScopeMCPConnect, "server_a")},
		},
		{
			Scope:     string(ScopeProjectRead),
			SubScopes: []string{},
			Selectors: []Selector{
				NewSelector(ScopeProjectRead, "proj_a"),
				NewSelector(ScopeProjectRead, "proj_b"),
			},
		},
	}, GrantsToScopedGrants(rows))
}

func TestCollapseUnrestrictedSelectors(t *testing.T) {
	t.Parallel()

	key := string(ScopeMCPConnect)
	scopedWildcard := NewSelector(ScopeMCPConnect, WildcardResource)
	collapsed := collapseUnrestrictedSelectors(map[string][]Selector{
		key: {
			scopedWildcard,
			{"resource_kind": "*", "resource_id": "*"},
		},
	})

	require.Equal(t, map[string]scopeAgg{
		key: {unrestricted: true, selectors: nil},
	}, collapsed)

	scopedOnly := collapseUnrestrictedSelectors(map[string][]Selector{
		key: {scopedWildcard},
	})
	require.Equal(t, map[string]scopeAgg{
		key: {unrestricted: false, selectors: []Selector{scopedWildcard}},
	}, scopedOnly)
}

func TestFlattenRoleGrants_deduplicatesByScopeAndSelector(t *testing.T) {
	t.Parallel()

	selector := NewSelector(ScopeProjectRead, "proj_1")
	rows, err := flattenRoleGrants([]*RoleGrant{
		{Scope: string(ScopeProjectRead), Selectors: []Selector{selector}},
		{Scope: string(ScopeProjectRead), Selectors: []Selector{selector}},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
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
		{scope: string(ScopeOrgAdmin), want: []string{string(ScopeOrgRead), string(ScopeRiskPolicyRead)}},
		{scope: string(ScopeProjectWrite), want: []string{string(ScopeProjectRead)}},
		{scope: string(ScopeMCPWrite), want: []string{string(ScopeMCPConnect), string(ScopeMCPRead)}},
		{scope: string(ScopeMCPRead), want: []string{string(ScopeMCPConnect)}},
		{scope: string(ScopeOrgRead), want: []string{}},
		{scope: string(ScopeProjectRead), want: []string{}},
		{scope: string(ScopeRoot), want: []string{}},
		{scope: string(ScopeMCPConnect), want: []string{}},
		{scope: string(ScopeEnvironmentRead), want: []string{}},
		{scope: string(ScopeEnvironmentWrite), want: []string{string(ScopeEnvironmentRead)}},
		{scope: string(ScopeSkillRead), want: []string{}},
		{scope: string(ScopeSkillWrite), want: []string{string(ScopeSkillRead)}},
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
		if scopeVisibilityByScope[lower] != scopeVisibilityUserVisible {
			continue
		}
		for _, h := range highers {
			if scopeVisibilityByScope[h] != scopeVisibilityUserVisible {
				continue
			}
			require.Contains(t, CalculateSubScopes(h), string(lower),
				"higher scope %q should imply lower scope %q", h, lower)
		}
	}
}
