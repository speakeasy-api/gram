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

	require.Equal(t, "project", ResourceKindForScope(ScopeProjectRead))
	require.Equal(t, "project", ResourceKindForScope(ScopeProjectBlockedRead))
	require.Equal(t, "project", ResourceKindForScope(ScopeProjectWrite))
	require.Equal(t, "project", ResourceKindForScope(ScopeProjectBlockedWrite))
}

func TestResourceKindForScope_mcpScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "mcp", ResourceKindForScope(ScopeMCPRead))
	require.Equal(t, "mcp", ResourceKindForScope(ScopeMCPBlockedRead))
	require.Equal(t, "mcp", ResourceKindForScope(ScopeMCPWrite))
	require.Equal(t, "mcp", ResourceKindForScope(ScopeMCPBlockedWrite))
	require.Equal(t, "mcp", ResourceKindForScope(ScopeMCPConnect))
	require.Equal(t, "mcp", ResourceKindForScope(ScopeMCPBlockedConnect))
}

func TestResourceKindForScope_remoteMCPScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "mcp", ResourceKindForScope(Scope("remote-mcp:read")))
	require.Equal(t, "mcp", ResourceKindForScope(Scope("remote-mcp:write")))
	require.Equal(t, "mcp", ResourceKindForScope(Scope("remote-mcp:connect")))
}

func TestResourceKindForScope_orgScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "org", ResourceKindForScope(ScopeOrgRead))
	require.Equal(t, "org", ResourceKindForScope(ScopeOrgBlockedRead))
	require.Equal(t, "org", ResourceKindForScope(ScopeOrgAdmin))
	require.Equal(t, "org", ResourceKindForScope(ScopeOrgBlockedAdmin))
}

func TestResourceKindForScope_rootScope(t *testing.T) {
	t.Parallel()

	require.Equal(t, "*", ResourceKindForScope(ScopeRoot))
}

func TestResourceKindForScope_riskPolicyScope(t *testing.T) {
	t.Parallel()

	require.Equal(t, ResourceKindRiskPolicy, ResourceKindForScope(ScopeRiskPolicyEvaluate))
	require.Equal(t, ResourceKindRiskPolicy, ResourceKindForScope(ScopeRiskPolicyBypass))
}

func TestResourceKindForScope_environmentScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, ResourceKindEnvironment, ResourceKindForScope(ScopeEnvironmentRead))
	require.Equal(t, ResourceKindEnvironment, ResourceKindForScope(ScopeEnvironmentBlockedRead))
	require.Equal(t, ResourceKindEnvironment, ResourceKindForScope(ScopeEnvironmentWrite))
	require.Equal(t, ResourceKindEnvironment, ResourceKindForScope(ScopeEnvironmentBlockedWrite))
}

func TestResourceKindForScope_skillScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, ResourceKindSkill, ResourceKindForScope(ScopeSkillRead))
	require.Equal(t, ResourceKindSkill, ResourceKindForScope(ScopeSkillBlockedRead))
	require.Equal(t, ResourceKindSkill, ResourceKindForScope(ScopeSkillWrite))
	require.Equal(t, ResourceKindSkill, ResourceKindForScope(ScopeSkillBlockedWrite))
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

func TestValidateSelector_mcpToolAnnotationsAllowed(t *testing.T) {
	t.Parallel()

	known := Selector{"resource_kind": "mcp", "resource_id": "*", "tool_annotations": "known"}
	none := Selector{"resource_kind": "mcp", "resource_id": "*", "tool_annotations": "none"}
	unknown := Selector{"resource_kind": "mcp", "resource_id": "*", "tool_annotations": "unknown"}
	require.NoError(t, ValidateSelector(ScopeMCPConnect, known))
	require.NoError(t, ValidateSelector(ScopeMCPConnect, none))
	require.NoError(t, ValidateSelector(ScopeMCPConnect, unknown))
	require.NoError(t, ValidateSelector(ScopeMCPRead, unknown))
	require.NoError(t, ValidateSelector(ScopeMCPWrite, unknown))
}

func TestValidateSelector_mcpToolAnnotationsInvalidValue(t *testing.T) {
	t.Parallel()

	sel := Selector{"resource_kind": "mcp", "resource_id": "*", "tool_annotations": "materialized"}
	require.ErrorContains(t, ValidateSelector(ScopeMCPConnect, sel), "invalid tool_annotations value")
}

func TestValidateSelector_toolAnnotationsRejectedOutsideMCP(t *testing.T) {
	t.Parallel()

	sel := Selector{"resource_kind": "project", "resource_id": "proj_123", "tool_annotations": "known"}
	require.ErrorContains(t, ValidateSelector(ScopeProjectRead, sel), "not allowed")
}

func TestSelector_Matches_toolAnnotationsGrantMatchesConnectionCheck(t *testing.T) {
	t.Parallel()

	// An allow grant constrained by tool_annotations still matches checks
	// that don't carry the dimension (check isn't constraining it) — same
	// semantics as disposition and project_id.
	grant := Selector{"resource_kind": "mcp", "resource_id": "*", "tool_annotations": "known"}
	check := Selector{"resource_kind": "mcp", "resource_id": "toolsetA"}
	require.True(t, grant.Matches(check))
}

func TestSelector_Matches_toolAnnotationsGrantDeniesWrongValue(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_kind": "mcp", "resource_id": "*", "tool_annotations": "known"}
	require.True(t, grant.Matches(Selector{"resource_kind": "mcp", "resource_id": "toolsetA", "tool_annotations": "known"}))
	require.False(t, grant.Matches(Selector{"resource_kind": "mcp", "resource_id": "toolsetA", "tool_annotations": "unknown"}))
}

func TestSelector_StrictMatches_toolAnnotationsDenyRequiresDimension(t *testing.T) {
	t.Parallel()

	// A deny grant on tool_annotations must not match a check that doesn't
	// carry the dimension — otherwise the deny would fire on every tool call
	// on paths that don't emit the key yet. Default permissive.
	deny := Selector{"resource_kind": "mcp", "resource_id": "*", "tool_annotations": "unknown"}
	withoutDimension := Selector{"resource_kind": "mcp", "resource_id": "toolsetA", "tool": "search"}
	require.False(t, deny.StrictMatches(withoutDimension))

	unknownCheck := Selector{"resource_kind": "mcp", "resource_id": "toolsetA", "tool": "search", "tool_annotations": "unknown"}
	require.True(t, deny.StrictMatches(unknownCheck))

	knownCheck := Selector{"resource_kind": "mcp", "resource_id": "toolsetA", "tool": "search", "tool_annotations": "known"}
	require.False(t, deny.StrictMatches(knownCheck))
}

func TestMCPToolCallCheck_injectsToolAnnotations(t *testing.T) {
	t.Parallel()

	check := MCPToolCallCheck("toolsetA", MCPToolCallDimensions{
		Tool:            "search",
		Disposition:     "",
		ProjectID:       "",
		ToolAnnotations: ToolAnnotationsUnknown,
	})
	require.Equal(t, ScopeMCPConnect, check.Scope)
	require.Equal(t, "unknown", check.Dimensions[SelectorKeyToolAnnotations])
}

func TestMCPToolCallCheck_emptyToolAnnotationsOmitsDimension(t *testing.T) {
	t.Parallel()

	check := MCPToolCallCheck("toolsetA", MCPToolCallDimensions{
		Tool:            "search",
		Disposition:     "",
		ProjectID:       "",
		ToolAnnotations: "",
	})
	_, ok := check.Dimensions[SelectorKeyToolAnnotations]
	require.False(t, ok)
}

func TestSelector_StrictMatches_toolAnnotationsPolicyGates(t *testing.T) {
	t.Parallel()

	// The three values encode two policy strengths. The review gate
	// (deny "unknown") lets "none" through: recording a zero-token metadata
	// row moves a tool from unknown to none, which is the data-level escape
	// hatch — deny grants accept no allow-side exceptions. The classification
	// gate (deny "unknown" plus deny "none") catches both non-"known" states.
	denyUnknown := Selector{"resource_kind": "mcp", "resource_id": "*", "tool_annotations": "unknown"}
	denyNone := Selector{"resource_kind": "mcp", "resource_id": "*", "tool_annotations": "none"}

	unknownCheck := Selector{"resource_kind": "mcp", "resource_id": "srv", "tool": "unvetted", "tool_annotations": "unknown"}
	noneCheck := Selector{"resource_kind": "mcp", "resource_id": "srv", "tool": "vetted_plain", "tool_annotations": "none"}
	knownCheck := Selector{"resource_kind": "mcp", "resource_id": "srv", "tool": "classified", "tool_annotations": "known"}

	// Review gate: only "unknown" is denied.
	require.True(t, denyUnknown.StrictMatches(unknownCheck))
	require.False(t, denyUnknown.StrictMatches(noneCheck))
	require.False(t, denyUnknown.StrictMatches(knownCheck))

	// Classification gate adds the "none" deny: only "known" survives.
	require.True(t, denyNone.StrictMatches(noneCheck))
	require.False(t, denyNone.StrictMatches(knownCheck))
}
