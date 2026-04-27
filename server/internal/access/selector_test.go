package access

import (
	"encoding/json"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestSelector_Matches_wildcardGrantMatchesAnything(t *testing.T) {
	t.Parallel()

	grant := authz.Selector{"resource_id": "*"}
	require.True(t, grant.Matches(authz.Selector{"resource_id": "proj_123"}))
	require.True(t, grant.Matches(authz.Selector{"resource_id": "anything"}))
	require.True(t, grant.Matches(authz.Selector{"resource_id": "*"}))
}

func TestSelector_Matches_emptyGrantMatchesAnything(t *testing.T) {
	t.Parallel()

	// Defensive: empty selector still matches (no keys to fail on).
	grant := authz.Selector{}
	require.True(t, grant.Matches(authz.Selector{"resource_id": "proj_123"}))
	require.True(t, grant.Matches(authz.Selector{}))
}

func TestSelector_Matches_exactKeyMatch(t *testing.T) {
	t.Parallel()

	grant := authz.Selector{"resource_id": "proj_123"}
	require.True(t, grant.Matches(authz.Selector{"resource_id": "proj_123"}))
	require.False(t, grant.Matches(authz.Selector{"resource_id": "proj_456"}))
}

func TestSelector_Matches_grantKeyMissingInCheckFails(t *testing.T) {
	t.Parallel()

	grant := authz.Selector{"resource_id": "proj_123"}
	require.False(t, grant.Matches(authz.Selector{}))
	require.False(t, grant.Matches(authz.Selector{"other_key": "proj_123"}))
}

func TestSelector_Matches_multipleKeys(t *testing.T) {
	t.Parallel()

	grant := authz.Selector{"resource_id": "proj_123", "tool_id": "tool_abc"}
	require.True(t, grant.Matches(authz.Selector{"resource_id": "proj_123", "tool_id": "tool_abc"}))
	require.False(t, grant.Matches(authz.Selector{"resource_id": "proj_123", "tool_id": "tool_xyz"}))
	require.False(t, grant.Matches(authz.Selector{"resource_id": "proj_123"}))
}

func TestSelector_Matches_nilGrantMatchesAnything(t *testing.T) {
	t.Parallel()

	var grant authz.Selector
	require.True(t, grant.Matches(authz.Selector{"resource_id": "proj_123"}))
}

func TestSelector_ResourceID_present(t *testing.T) {
	t.Parallel()

	require.Equal(t, "proj_123", authz.Selector{"resource_id": "proj_123"}.ResourceID())
}

func TestSelector_ResourceID_wildcard(t *testing.T) {
	t.Parallel()

	require.Equal(t, "*", authz.Selector{"resource_id": "*"}.ResourceID())
}

func TestSelector_ResourceID_absent(t *testing.T) {
	t.Parallel()

	require.Equal(t, "*", authz.Selector{}.ResourceID())
	require.Equal(t, "*", authz.Selector(nil).ResourceID())
}

func TestSelector_MarshalJSON_nil(t *testing.T) {
	t.Parallel()

	var s authz.Selector
	b, err := json.Marshal(s)
	require.NoError(t, err)
	require.JSONEq(t, `{"resource_kind":"*","resource_id":"*"}`, string(b))
}

func TestSelector_MarshalJSON_withKeys(t *testing.T) {
	t.Parallel()

	s := authz.Selector{"resource_id": "proj_123"}
	b, err := json.Marshal(s)
	require.NoError(t, err)
	require.JSONEq(t, `{"resource_id":"proj_123"}`, string(b))
}

func TestSelector_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	var s authz.Selector
	err := json.Unmarshal([]byte(`{"resource_id":"proj_123","tool_id":"t1"}`), &s)
	require.NoError(t, err)
	require.Equal(t, "proj_123", s["resource_id"])
	require.Equal(t, "t1", s["tool_id"])
}

func TestResourceKindForScope_buildScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "project", authz.ResourceKindForScope(authz.ScopeProjectRead))
	require.Equal(t, "project", authz.ResourceKindForScope(authz.ScopeProjectWrite))
}

func TestResourceKindForScope_mcpScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "mcp", authz.ResourceKindForScope(authz.ScopeMCPRead))
	require.Equal(t, "mcp", authz.ResourceKindForScope(authz.ScopeMCPWrite))
	require.Equal(t, "mcp", authz.ResourceKindForScope(authz.ScopeMCPConnect))
}

func TestResourceKindForScope_remoteMCPScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "mcp", authz.ResourceKindForScope(authz.Scope("remote-mcp:read")))
	require.Equal(t, "mcp", authz.ResourceKindForScope(authz.Scope("remote-mcp:write")))
	require.Equal(t, "mcp", authz.ResourceKindForScope(authz.Scope("remote-mcp:connect")))
}

func TestResourceKindForScope_orgScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "org", authz.ResourceKindForScope(authz.ScopeOrgRead))
	require.Equal(t, "org", authz.ResourceKindForScope(authz.ScopeOrgAdmin))
}

func TestResourceKindForScope_rootScope(t *testing.T) {
	t.Parallel()

	require.Equal(t, "*", authz.ResourceKindForScope(authz.ScopeRoot))
}

func TestNewSelector_includesResourceKind(t *testing.T) {
	t.Parallel()

	s := authz.NewSelector(authz.ScopeProjectRead, "proj_123")
	require.Equal(t, authz.Selector{"resource_kind": "project", "resource_id": "proj_123"}, s)
}

func TestNewSelector_wildcardResource(t *testing.T) {
	t.Parallel()

	s := authz.NewSelector(authz.ScopeOrgAdmin, authz.WildcardResource)
	require.Equal(t, authz.Selector{"resource_kind": "org", "resource_id": "*"}, s)
}

func TestNewGrant_combinesScopeAndSelector(t *testing.T) {
	t.Parallel()

	g := authz.NewGrant(authz.ScopeMCPConnect, "tool_a")
	require.Equal(t, authz.ScopeMCPConnect, g.Scope)
	require.Equal(t, authz.Selector{"resource_kind": "mcp", "resource_id": "tool_a"}, g.Selector)
}

func TestSelector_Matches_resourceKindMismatchFails(t *testing.T) {
	t.Parallel()

	grant := authz.Selector{"resource_kind": "project", "resource_id": "proj_123"}
	require.False(t, grant.Matches(authz.Selector{"resource_kind": "mcp", "resource_id": "proj_123"}))
}

func TestSelector_Matches_resourceKindWildcardMatchesAny(t *testing.T) {
	t.Parallel()

	grant := authz.Selector{"resource_kind": "*", "resource_id": "*"}
	require.True(t, grant.Matches(authz.Selector{"resource_kind": "project", "resource_id": "proj_123"}))
	require.True(t, grant.Matches(authz.Selector{"resource_kind": "mcp", "resource_id": "tool_a"}))
}
