package access

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

func TestSelector_Matches_grantKeyMissingInCheckFails(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_id": "proj_123"}
	require.False(t, grant.Matches(Selector{}))
	require.False(t, grant.Matches(Selector{"other_key": "proj_123"}))
}

func TestSelector_Matches_multipleKeys(t *testing.T) {
	t.Parallel()

	grant := Selector{"resource_id": "proj_123", "tool_id": "tool_abc"}
	require.True(t, grant.Matches(Selector{"resource_id": "proj_123", "tool_id": "tool_abc"}))
	require.False(t, grant.Matches(Selector{"resource_id": "proj_123", "tool_id": "tool_xyz"}))
	require.False(t, grant.Matches(Selector{"resource_id": "proj_123"}))
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

	require.Equal(t, "project", ResourceKindForScope(ScopeBuildRead))
	require.Equal(t, "project", ResourceKindForScope(ScopeBuildWrite))
}

func TestResourceKindForScope_mcpScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "mcp", ResourceKindForScope(ScopeMCPRead))
	require.Equal(t, "mcp", ResourceKindForScope(ScopeMCPWrite))
	require.Equal(t, "mcp", ResourceKindForScope(ScopeMCPConnect))
}

func TestResourceKindForScope_remoteMCPScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "mcp", ResourceKindForScope(ScopeRemoteMCPRead))
	require.Equal(t, "mcp", ResourceKindForScope(ScopeRemoteMCPWrite))
	require.Equal(t, "mcp", ResourceKindForScope(ScopeRemoteMCPConnect))
}

func TestResourceKindForScope_orgScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "org", ResourceKindForScope(ScopeOrgRead))
	require.Equal(t, "org", ResourceKindForScope(ScopeOrgAdmin))
}

func TestResourceKindForScope_rootScope(t *testing.T) {
	t.Parallel()

	require.Equal(t, "*", ResourceKindForScope(ScopeRoot))
}

func TestNewSelector_includesResourceKind(t *testing.T) {
	t.Parallel()

	s := NewSelector(ScopeBuildRead, "proj_123")
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
