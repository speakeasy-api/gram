package toolfilter

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFrozenDefinitionsCanonicalAndComplete(t *testing.T) {
	t.Parallel()
	member := uuid.New()
	first, err := NewFrozenTool("server--read", member, "route", json.RawMessage(`{"name":"read","inputSchema":{"type":"object"},"description":"old","outputSchema":{"type":"string"},"annotations":{"readOnlyHint":true},"extension":{"a":1}}`))
	require.NoError(t, err)
	same, err := NewFrozenTool("server--read", member, "route", json.RawMessage(`{"extension":{"a":1},"annotations":{"readOnlyHint":true},"outputSchema":{"type":"string"},"description":"old","inputSchema":{"type":"object"},"name":"read"}`))
	require.NoError(t, err)
	require.Equal(t, first.Fingerprint, same.Fingerprint)
	snapshot := &FrozenToolset{Tools: []FrozenTool{first}}
	require.True(t, snapshot.Allows(same))
	for _, field := range []string{"description", "inputSchema", "outputSchema", "annotations", "extension"} {
		var def map[string]any
		require.NoError(t, json.Unmarshal(first.Definition, &def))
		def[field] = "changed"
		raw, err := json.Marshal(def)
		require.NoError(t, err)
		changed, err := NewFrozenTool(first.Name, member, "route", raw)
		require.NoError(t, err)
		require.False(t, snapshot.Allows(changed), field)
	}
	changedRoute, err := NewFrozenTool(first.Name, member, "replacement-route", first.Definition)
	require.NoError(t, err)
	require.False(t, snapshot.Allows(changedRoute))
	changedMember, err := NewFrozenTool(first.Name, uuid.New(), "route", first.Definition)
	require.NoError(t, err)
	require.False(t, snapshot.Allows(changedMember))
}
func TestFrozenReviewRejectsStaleAndKeepsEmptySelection(t *testing.T) {
	t.Parallel()
	tool, err := NewFrozenTool("server--read", uuid.New(), "route", json.RawMessage(`{"name":"read","inputSchema":{"type":"object"}}`))
	require.NoError(t, err)
	inventory := &FrozenToolset{Tools: []FrozenTool{tool}}
	hash, err := inventory.Fingerprint()
	require.NoError(t, err)
	selected, err := inventory.Select(hash, nil)
	require.NoError(t, err)
	require.NotNil(t, selected.Tools)
	require.Empty(t, selected.Tools)
	require.False(t, selected.Allows(tool))
	_, err = inventory.Select("stale", []string{tool.Name})
	require.Error(t, err)
	_, err = inventory.Select(hash, []string{"guessed"})
	require.Error(t, err)
	_, err = inventory.Select(hash, []string{tool.Name, tool.Name})
	require.Error(t, err)
}
func TestFrozenPolicyVersionsAndFingerprints(t *testing.T) {
	t.Parallel()
	policy := &SessionPolicy{Resource: "meta_mcp_server:" + uuid.NewString(), Selection: nil, Gateway: &GatewayOptions{DiscoveryMode: nil, Frozen: &FrozenToolset{Tools: []FrozenTool{}}}}
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"version":2`)
	decoded, err := ParseSessionPolicy(raw)
	require.NoError(t, err)
	require.NotNil(t, decoded.Gateway.Frozen)
	require.Nil(t, decoded.Selection)
	var document map[string]any
	require.NoError(t, json.Unmarshal(raw, &document))
	document["version"] = 1
	oldVersion, err := json.Marshal(document)
	require.NoError(t, err)
	_, err = ParseSessionPolicy(oldVersion)
	require.ErrorContains(t, err, "version 2")
}
