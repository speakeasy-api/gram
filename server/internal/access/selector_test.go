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

func TestForResource_wildcard(t *testing.T) {
	t.Parallel()

	s := ForResource("*")
	require.Equal(t, Selector{"resource_id": "*"}, s)
}

func TestForResource_specific(t *testing.T) {
	t.Parallel()

	s := ForResource("proj_123")
	require.Equal(t, Selector{"resource_id": "proj_123"}, s)
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
	require.JSONEq(t, `{"resource_id":"*"}`, string(b))
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
