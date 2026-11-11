package authz

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelector_Matches_wildcardGrantMatchesAnything(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_id": "*"}
	require.True(t, grant.Matches(Selector{"resource_id": "proj_123"}))
	require.True(t, grant.Matches(Selector{"resource_id": "anything"}))
	require.True(t, grant.Matches(Selector{"resource_id": "*"}))
}

func TestSelector_Matches_emptyGrantMatchesAnything(t *testing.T) {
	t.Parallel()

	// Defensive: empty selector still matches (no keys to fail on).
	grant := Selector{}
	require.True(t, grant.Matches(Selector{"resource_id": "proj_123"}))
	require.True(t, grant.Matches(Selector{}))
}

func TestSelector_Matches_exactKeyMatch(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_id": "proj_123"}
	require.True(t, grant.Matches(Selector{"resource_id": "proj_123"}))
	require.False(t, grant.Matches(Selector{"resource_id": "proj_456"}))
}

func TestSelector_Matches_grantKeyMissingInCheckSkipped(t *testing.T) {
	t.Parallel()

	// When a key exists in the grant but not in the check, it's skipped —
	// the check isn't constraining that dimension.
	grant := Selector{"resource_id": "proj_123"}
	require.True(t, grant.Matches(Selector{}))
	require.True(t, grant.Matches(Selector{"other_key": "proj_123"}))
}

func TestSelector_Matches_multipleKeys(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_id": "proj_123", "tool_id": "tool_abc"}
	require.True(t, grant.Matches(Selector{"resource_id": "proj_123", "tool_id": "tool_abc"}))
	require.False(t, grant.Matches(Selector{"resource_id": "proj_123", "tool_id": "tool_xyz"}))
	// Check without tool_id — not constraining that dimension, so grant matches.
	require.True(t, grant.Matches(Selector{"resource_id": "proj_123"}))
}

func TestSelector_Matches_nilGrantMatchesAnything(t *testing.T) {
	t.Parallel()

	var grant Selector
	require.True(t, grant.Matches(Selector{"resource_id": "proj_123"}))
}

func TestSelector_ResourceID_present(t *testing.T) {
	t.Parallel()

	require.Equal(t, "proj_123", Selector{"resource_id": "proj_123"}.ResourceID())
}

func TestSelector_ResourceID_wildcard(t *testing.T) {
	t.Parallel()

	require.Equal(t, "*", Selector{"resource_id": "*"}.ResourceID())
}

func TestSelector_ResourceID_absent(t *testing.T) {
	t.Parallel()

	require.Equal(t, "*", Selector{}.ResourceID())
	require.Equal(t, "*", Selector(nil).ResourceID())
}

func TestSelector_CoversEveryResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		selector Selector
		covers   bool
	}{
		{name: "nil", selector: nil, covers: true},
		{name: "pure wildcard", selector: Selector{"resource_kind": "*", "resource_id": "*"}, covers: true},
		{name: "stored unrestricted grant", selector: NewSelector(ScopeLogRead, WildcardResource), covers: true},
		{name: "one resource", selector: NewSelector(ScopeLogRead, "project-1"), covers: false},
		{name: "one project of a kind", selector: Selector{"resource_kind": "mcp", "resource_id": "*", "project_id": "p1"}, covers: false},
		{name: "one tool", selector: Selector{"resource_kind": "mcp", "resource_id": "*", "tool": "search"}, covers: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.covers, tt.selector.CoversEveryResource())
		})
	}
}

func TestSelector_IsRestricted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		selector   Selector
		restricted bool
	}{
		{
			name:       "pure wildcard",
			selector:   Selector{"resource_kind": "*", "resource_id": "*"},
			restricted: false,
		},
		{
			name:       "scope wildcard is still scoped",
			selector:   Selector{"resource_kind": "mcp", "resource_id": "*"},
			restricted: true,
		},
		{
			name:       "extra dimension is scoped",
			selector:   Selector{"resource_kind": "*", "resource_id": "*", "tool": "dangerous"},
			restricted: true,
		},
		{
			name:       "legacy resource wildcard is unrestricted",
			selector:   Selector{"resource_id": "*"},
			restricted: false,
		},
		{
			name:       "missing resource id is restricted",
			selector:   Selector{"resource_kind": "*"},
			restricted: true,
		},
		{
			name:       "unknown one-key selector is restricted",
			selector:   Selector{"unknown": "*"},
			restricted: true,
		},
		{
			name:       "missing resource kind with extra key is restricted",
			selector:   Selector{"resource_id": "*", "unknown": "*"},
			restricted: true,
		},
		{
			name:       "nil is unrestricted wildcard",
			selector:   nil,
			restricted: false,
		},
		{
			name:       "empty is unrestricted wildcard",
			selector:   Selector{},
			restricted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.restricted, tt.selector.IsRestricted())
		})
	}
}

func TestSelector_MarshalJSON_nil(t *testing.T) {
	t.Parallel()

	var s Selector
	b, err := json.Marshal(s)
	require.NoError(t, err)
	require.JSONEq(t, `{"resource_kind":"*","resource_id":"*"}`, string(b))
}

func TestSelector_MarshalJSON_withKeys(t *testing.T) {
	t.Parallel()

	s := Selector{"resource_id": "proj_123"}
	b, err := json.Marshal(s)
	require.NoError(t, err)
	require.JSONEq(t, `{"resource_id":"proj_123"}`, string(b))
}

func TestSelector_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	var s Selector
	err := json.Unmarshal([]byte(`{"resource_id":"proj_123","tool_id":"t1"}`), &s)
	require.NoError(t, err)
	require.Equal(t, "proj_123", s["resource_id"])
	require.Equal(t, "t1", s["tool_id"])
}

func TestResourceKindForScope_buildScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "project", ScopeProjectRead.ResourceKind())
	require.Equal(t, "project", ScopeProjectBlockedRead.ResourceKind())
	require.Equal(t, "project", ScopeProjectWrite.ResourceKind())
	require.Equal(t, "project", ScopeProjectBlockedWrite.ResourceKind())
}

func TestResourceKindForScope_mcpScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "mcp", ScopeMCPRead.ResourceKind())
	require.Equal(t, "mcp", ScopeMCPBlockedRead.ResourceKind())
	require.Equal(t, "mcp", ScopeMCPWrite.ResourceKind())
	require.Equal(t, "mcp", ScopeMCPBlockedWrite.ResourceKind())
	require.Equal(t, "mcp", ScopeMCPConnect.ResourceKind())
	require.Equal(t, "mcp", ScopeMCPBlockedConnect.ResourceKind())
}

func TestResourceKindForScope_remoteMCPScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "mcp", Scope("remote-mcp:read").ResourceKind())
	require.Equal(t, "mcp", Scope("remote-mcp:write").ResourceKind())
	require.Equal(t, "mcp", Scope("remote-mcp:connect").ResourceKind())
}

func TestResourceKindForScope_orgScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "org", ScopeOrgRead.ResourceKind())
	require.Equal(t, "org", ScopeOrgBlockedRead.ResourceKind())
	require.Equal(t, "org", ScopeOrgAdmin.ResourceKind())
	require.Equal(t, "org", ScopeOrgBlockedAdmin.ResourceKind())
}

func TestResourceKindForScope_rootScope(t *testing.T) {
	t.Parallel()

	require.Equal(t, "*", ScopeRoot.ResourceKind())
}

func TestResourceKindForScope_riskPolicyScope(t *testing.T) {
	t.Parallel()

	require.Equal(t, ResourceKindRiskPolicy, ScopeRiskPolicyEvaluate.ResourceKind())
	require.Equal(t, ResourceKindRiskPolicy, ScopeRiskPolicyBypass.ResourceKind())
}

func TestResourceKindForScope_environmentScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, ResourceKindEnvironment, ScopeEnvironmentRead.ResourceKind())
	require.Equal(t, ResourceKindEnvironment, ScopeEnvironmentBlockedRead.ResourceKind())
	require.Equal(t, ResourceKindEnvironment, ScopeEnvironmentWrite.ResourceKind())
	require.Equal(t, ResourceKindEnvironment, ScopeEnvironmentBlockedWrite.ResourceKind())
}

func TestResourceKindForScope_skillScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, ResourceKindSkill, ScopeSkillRead.ResourceKind())
	require.Equal(t, ResourceKindSkill, ScopeSkillBlockedRead.ResourceKind())
	require.Equal(t, ResourceKindSkill, ScopeSkillWrite.ResourceKind())
	require.Equal(t, ResourceKindSkill, ScopeSkillBlockedWrite.ResourceKind())
}

func TestResourceKindForScope_agentScopes(t *testing.T) {
	t.Parallel()

	for _, scope := range []Scope{ScopeAgentRead, ScopeAgentWrite, ScopeAgentAuthorize, ScopeAgentTransfer} {
		require.Equal(t, ResourceKindAgent, scope.ResourceKind())
	}
}

func TestValidateSelector_agentRequiresAgentResourceKind(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateSelector(ScopeAgentRead, Selector{
		SelectorKeyResourceKind: ResourceKindAgent,
		SelectorKeyResourceID:   "0196cbd1-9328-74e7-b7bb-6e5357565573",
	}))
	require.ErrorContains(t, ValidateSelector(ScopeAgentWrite, Selector{
		SelectorKeyResourceKind: ResourceKindProject,
		SelectorKeyResourceID:   "0196cbd1-9328-74e7-b7bb-6e5357565573",
	}), `requires resource_kind="agent"`)
}

func TestResourceKindForScope_assistantScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, ResourceKindAssistant, ScopeAssistantRead.ResourceKind())
	require.Equal(t, ResourceKindAssistant, ScopeAssistantBlockedRead.ResourceKind())
	require.Equal(t, ResourceKindAssistant, ScopeAssistantWrite.ResourceKind())
	require.Equal(t, ResourceKindAssistant, ScopeAssistantBlockedWrite.ResourceKind())
}

func TestValidateSelector_assistantUsesProjectIDWithAssistantResourceKind(t *testing.T) {
	t.Parallel()

	projectID := "0196cbd1-9328-74e7-b7bb-6e5357565573"
	selector := NewSelector(ScopeAssistantRead, projectID)
	require.Equal(t, ResourceKindAssistant, selector[SelectorKeyResourceKind])
	require.Equal(t, projectID, selector[SelectorKeyResourceID])
	require.NoError(t, ValidateSelector(ScopeAssistantRead, selector))
	require.ErrorContains(t, ValidateSelector(ScopeAssistantWrite, NewSelector(ScopeProjectWrite, projectID)), `requires resource_kind="assistant"`)
}

func TestValidateSelector_skillRequiresSkillResourceKind(t *testing.T) {
	t.Parallel()

	projectID := "0196cbd1-9328-74e7-b7bb-6e5357565573"
	require.NoError(t, ValidateSelector(ScopeSkillRead, Selector{
		SelectorKeyResourceKind: ResourceKindSkill,
		SelectorKeyResourceID:   projectID,
	}))
	require.ErrorContains(t, ValidateSelector(ScopeSkillWrite, Selector{
		SelectorKeyResourceKind: ResourceKindProject,
		SelectorKeyResourceID:   projectID,
	}), `requires resource_kind="skill"`)
}

func TestNewSelector_includesResourceKind(t *testing.T) {
	t.Parallel()

	s := NewSelector(ScopeProjectRead, "proj_123")
	require.Equal(t, Selector{"resource_kind": "project", "resource_id": "proj_123"}, s)
}

func TestNewSelector_wildcardResource(t *testing.T) {
	t.Parallel()

	s := NewSelector(ScopeOrgAdmin, WildcardResource)
	require.Equal(t, Selector{"resource_kind": "org", "resource_id": "*"}, s)
}

func TestNewGrant_combinesScopeAndSelector(t *testing.T) {
	t.Parallel()

	g := NewGrant(ScopeMCPConnect, "tool_a")
	require.Equal(t, ScopeMCPConnect, g.Scope)
	require.Equal(t, Selector{"resource_kind": "mcp", "resource_id": "tool_a"}, g.Selector)
}

func TestSelector_Matches_resourceKindMismatchFails(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_kind": "project", "resource_id": "proj_123"}
	require.False(t, grant.Matches(Selector{"resource_kind": "mcp", "resource_id": "proj_123"}))
}

func TestSelector_Matches_resourceKindWildcardMatchesAny(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_kind": "*", "resource_id": "*"}
	require.True(t, grant.Matches(Selector{"resource_kind": "project", "resource_id": "proj_123"}))
	require.True(t, grant.Matches(Selector{"resource_kind": "mcp", "resource_id": "tool_a"}))
}

func TestSelector_Matches_dispositionGrantMatchesConnectionCheck(t *testing.T) {
	t.Parallel()

	// A disposition-scoped grant matches a connection-level check that
	// doesn't specify disposition (check isn't constraining that dimension).
	grant := Selector{"resource_kind": "mcp", "resource_id": "*", "disposition": "read_only"}
	check := Selector{"resource_kind": "mcp", "resource_id": "toolsetA"}
	require.True(t, grant.Matches(check))
}

func TestSelector_Matches_projectScopedGrantMatchesSameProject(t *testing.T) {
	t.Parallel()

	// A project-scoped MCP grant matches any server in that project.
	grant := Selector{"resource_kind": "mcp", "resource_id": "*", "project_id": "proj_123"}
	check := Selector{"resource_kind": "mcp", "resource_id": "server_456", "project_id": "proj_123"}
	require.True(t, grant.Matches(check))
}

func TestSelector_Matches_projectScopedGrantDeniesDifferentProject(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_kind": "mcp", "resource_id": "*", "project_id": "proj_123"}
	check := Selector{"resource_kind": "mcp", "resource_id": "server_456", "project_id": "proj_789"}
	require.False(t, grant.Matches(check))
}

func TestSelector_Matches_projectScopedGrantMatchesConnectionWithoutProjectID(t *testing.T) {
	t.Parallel()

	// A project-scoped grant still matches checks that don't carry project_id
	// (dimension skipped), matching the disposition behavior.
	grant := Selector{"resource_kind": "mcp", "resource_id": "*", "project_id": "proj_123"}
	check := Selector{"resource_kind": "mcp", "resource_id": "server_456"}
	require.True(t, grant.Matches(check))
}

func TestSelector_Matches_serverGrantStillMatchesWithProjectDimension(t *testing.T) {
	t.Parallel()

	// Existing server-level grants remain backward-compatible when checks
	// carry the new project_id dimension.
	grant := Selector{"resource_kind": "mcp", "resource_id": "server_456"}
	check := Selector{"resource_kind": "mcp", "resource_id": "server_456", "project_id": "proj_123"}
	require.True(t, grant.Matches(check))
}

func TestValidateSelector_mcpProjectIDAllowed(t *testing.T) {
	t.Parallel()

	sel := Selector{"resource_kind": "mcp", "resource_id": "*", "project_id": "proj_123"}
	require.NoError(t, ValidateSelector(ScopeMCPConnect, sel))
	require.NoError(t, ValidateSelector(ScopeMCPRead, sel))
	require.NoError(t, ValidateSelector(ScopeMCPWrite, sel))
}

func TestValidateSelector_riskPolicyAllowsServerURL(t *testing.T) {
	t.Parallel()

	valid := Selector{"resource_kind": ResourceKindRiskPolicy, "resource_id": "policy_123"}
	require.NoError(t, ValidateSelector(ScopeRiskPolicyEvaluate, valid))

	withServerURL := Selector{"resource_kind": ResourceKindRiskPolicy, "resource_id": "policy_123", "server_url": "https://api.example.com"}
	require.NoError(t, ValidateSelector(ScopeRiskPolicyBypass, withServerURL))
	withServerIdentity := Selector{"resource_kind": ResourceKindRiskPolicy, "resource_id": "policy_123", "server_identity": "github"}
	require.NoError(t, ValidateSelector(ScopeRiskPolicyBypass, withServerIdentity))
	withHostOnlyServerURL := Selector{"resource_kind": ResourceKindRiskPolicy, "resource_id": "policy_123", "server_url": "api.example.com"}
	require.ErrorContains(t, ValidateSelector(ScopeRiskPolicyBypass, withHostOnlyServerURL), "must include URI scheme and host")

	withExtraKey := Selector{"resource_kind": ResourceKindRiskPolicy, "resource_id": "policy_123", "tool": "search"}
	require.ErrorContains(t, ValidateSelector(ScopeRiskPolicyEvaluate, withExtraKey), "not allowed")
}

func TestRiskPolicyBypassCheck_injectsServerURL(t *testing.T) {
	t.Parallel()

	check := RiskPolicyBypassCheck("policy_123", RiskPolicyDimensions{ServerURL: "https://api.example.com", ServerIdentity: ""})
	require.Equal(t, ScopeRiskPolicyBypass, check.Scope)
	require.Equal(t, "policy_123", check.ResourceID)
	require.Equal(t, "https://api.example.com", check.Dimensions[SelectorKeyServerURL])
}

func TestRiskPolicyBypassCheck_injectsServerIdentity(t *testing.T) {
	t.Parallel()

	check := RiskPolicyBypassCheck("policy_123", RiskPolicyDimensions{ServerURL: "", ServerIdentity: "github"})
	require.Equal(t, ScopeRiskPolicyBypass, check.Scope)
	require.Equal(t, "policy_123", check.ResourceID)
	require.Equal(t, "github", check.Dimensions[SelectorKeyServerIdentity])
}

func TestRiskPolicyBypassCheck_emptyServerURLOmitsDimension(t *testing.T) {
	t.Parallel()

	check := RiskPolicyBypassCheck("policy_123", RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""})
	require.Nil(t, check.Dimensions)
}

func TestSelector_Matches_riskPolicyServerURL(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_kind": "risk_policy", "resource_id": "policy_123", "server_url": "https://api.example.com"}
	require.True(t, grant.Matches(Selector{"resource_kind": "risk_policy", "resource_id": "policy_123", "server_url": "https://api.example.com"}))
	require.False(t, grant.Matches(Selector{"resource_kind": "risk_policy", "resource_id": "policy_123", "server_url": "https://api.other.com"}))
}

func TestMCPCheck_injectsProjectID(t *testing.T) {
	t.Parallel()

	check := MCPCheck(ScopeMCPRead, "server_456", "proj_123")
	require.Equal(t, ScopeMCPRead, check.Scope)
	require.Equal(t, "server_456", check.ResourceID)
	require.Equal(t, "proj_123", check.Dimensions[SelectorKeyProjectID])
}

func TestMCPCheck_emptyProjectIDOmitsDimension(t *testing.T) {
	t.Parallel()

	check := MCPCheck(ScopeMCPRead, "server_456", "")
	require.Nil(t, check.Dimensions)
}

func TestSelector_Matches_dispositionGrantDeniesWrongDisposition(t *testing.T) {
	t.Parallel()

	// When the check specifies a disposition, it must match the grant's.
	grant := Selector{"resource_kind": "mcp", "resource_id": "*", "disposition": "read_only"}
	require.True(t, grant.Matches(Selector{"resource_kind": "mcp", "resource_id": "toolsetA", "disposition": "read_only"}))
	require.False(t, grant.Matches(Selector{"resource_kind": "mcp", "resource_id": "toolsetA", "disposition": "destructive"}))
}

func TestValidateSelector_ProjectBoundsHooksIngestOnly(t *testing.T) {
	t.Parallel()

	bounded := func(scope Scope) Selector {
		selector := NewSelector(scope, "org-123")
		selector[SelectorKeyProjectID] = "project-1"
		return selector
	}
	require.NoError(t, ValidateSelector(ScopeOrgHooksIngest, bounded(ScopeOrgHooksIngest)))
	require.Error(t, ValidateSelector(ScopeOrgDeviceAgentSync, bounded(ScopeOrgDeviceAgentSync)), "project_id must not read as a restriction on scopes not checked by project")
	require.Error(t, ValidateSelector(ScopeOrgRead, bounded(ScopeOrgRead)))
}

func TestHooksIngestCheck_MatchesOnlyItsProjectBound(t *testing.T) {
	t.Parallel()

	selector := NewSelector(ScopeOrgHooksIngest, "org-123")
	selector[SelectorKeyProjectID] = "project-1"
	bounded := []Grant{NewGrantWithSelector(ScopeOrgHooksIngest, selector)}
	unbounded := []Grant{NewGrant(ScopeOrgHooksIngest, "org-123")}

	require.True(t, GrantsSatisfy(bounded, HooksIngestCheck("org-123", "project-1")))
	require.False(t, GrantsSatisfy(bounded, HooksIngestCheck("org-123", "project-2")))
	require.True(t, GrantsSatisfy(unbounded, HooksIngestCheck("org-123", "project-2")), "a grant naming no project covers every project")
}
