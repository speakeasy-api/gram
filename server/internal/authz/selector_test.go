package authz

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// A grant key the check does not carry never matches: absence is not
// permission.
func TestSelector_StrictMatches_missingKeyNeverMatches(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_id": "proj_123"}
	require.False(t, grant.StrictMatches(Selector{}))
	require.False(t, grant.StrictMatches(Selector{"other_key": "proj_123"}))
}

// A scope the registry does not declare, such as a retired one still stored
// in an old policy, keeps the kind its prefix has always meant. Declared
// scopes are pinned by the generated tables and the decision snapshot.
func TestResourceKindForScope_undeclaredScopesFallBackToTheirPrefix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		scope Scope
		want  string
	}{
		{"plugin:retired", ResourceKindProject},
		{"log:retired", ResourceKindProject},
		{"remote-mcp:read", ResourceKindMCP},
		{"skill:retired", ResourceKindSkill},
		{"mcp_auth_client:x", ResourceKindWildcard},
		{"unknown:read", ResourceKindWildcard},
	} {
		require.Equal(t, tc.want, ResourceKindForScope(tc.scope), tc.scope)
	}
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
	require.True(t, grant.StrictMatches(Selector{"resource_kind": "risk_policy", "resource_id": "policy_123", "server_url": "https://api.example.com"}))
	require.False(t, grant.StrictMatches(Selector{"resource_kind": "risk_policy", "resource_id": "policy_123", "server_url": "https://api.other.com"}))
}

func TestMCPCheck_injectsProjectID(t *testing.T) {
	t.Parallel()

	check := MCPServer(MCPServerIDFrom("server_456"), ProjectIDFrom("proj_123")).Read()
	require.Equal(t, ScopeMCPRead, check.Scope)
	require.Equal(t, "server_456", check.ResourceID)
	require.Equal(t, "proj_123", check.Dimensions[SelectorKeyProjectID])
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

	require.True(t, GrantsSatisfy(bounded, Org(OrgIDFrom("org-123")).HooksIngest(ProjectIDFrom("project-1"))))
	require.False(t, GrantsSatisfy(bounded, Org(OrgIDFrom("org-123")).HooksIngest(ProjectIDFrom("project-2"))))
	require.True(t, GrantsSatisfy(unbounded, Org(OrgIDFrom("org-123")).HooksIngest(ProjectIDFrom("project-2"))), "a grant naming no project covers every project")
}

func TestGrantsContainSelectorWildcards(t *testing.T) {
	t.Parallel()
	broad := NewGrant(ScopeMCPConnect, "*")
	candidate := NewSelector(ScopeMCPConnect, "*")
	require.True(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	require.False(t, GrantsContainSelector([]Grant{NewGrant(ScopeMCPConnect, "one-server")}, ScopeMCPConnect, candidate))
	require.False(t, GrantsContainSelector([]Grant{NewGrant(ScopeMCPWrite, "*")}, ScopeMCPConnect, candidate), "mcp:write does not contain mcp:connect")
	broad.Selector[SelectorKeyTool] = "allowed-tool"
	require.False(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	candidate[SelectorKeyTool] = "allowed-tool"
	require.True(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	candidate[SelectorKeyTool] = "other-tool"
	require.False(t, GrantsContainSelector([]Grant{broad}, ScopeMCPConnect, candidate))
	// Delegation support must not relax runtime's concrete-resource requirement.
	_, err := GrantsAuthorize([]Grant{broad}, mcpCheck(ScopeMCPConnect, "*", "example-project"))
	require.ErrorIs(t, err, ErrInvalidCheck)
}

func limitedSelector(scope Scope, key, value string) Selector {
	selector := NewSelector(scope, "server-one")
	selector[key] = value
	return selector
}

// A tool or disposition limit is saved on connect rules and connect blocks
// only. Viewing or managing a server names no tool.
func TestValidateSelector_LimitsAreForConnectOnly(t *testing.T) {
	t.Parallel()

	limits := map[string]string{SelectorKeyTool: "search", SelectorKeyDisposition: DispositionReadOnly}
	for key, value := range limits {
		for _, scope := range []Scope{ScopeMCPConnect, ScopeMCPBlockedConnect} {
			require.NoError(t, ValidateSelector(scope, limitedSelector(scope, key, value)), "%s limited by %s", scope, key)
		}
		for _, scope := range []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPBlockedRead, ScopeMCPBlockedWrite} {
			require.ErrorContains(t, ValidateSelector(scope, limitedSelector(scope, key, value)), "is not allowed for scope", "%s limited by %s", scope, key)
		}
	}
}
