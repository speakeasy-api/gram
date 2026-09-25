package toolfilter

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
)

func TestSessionPolicyLegacyUnrestricted(t *testing.T) {
	t.Parallel()
	policy, err := ParseSessionPolicy(nil)
	require.NoError(t, err)
	require.Nil(t, policy)
}

func TestSessionPolicyEmptyAllowRemainsRestrictive(t *testing.T) {
	t.Parallel()
	selection, err := NewSessionSelection("mcp_server:"+uuid.NewString(), uuid.New(), []AllowEntry{})
	require.NoError(t, err)
	raw, err := json.Marshal(selection)
	require.NoError(t, err)
	policy, err := ParseSessionPolicy(raw)
	require.NoError(t, err)
	require.NotNil(t, policy.Selection)
	require.False(t, policy.Selection.AllowsName("anything"))
	require.Nil(t, policy.Gateway)
	encoded, err := json.Marshal(policy)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(encoded))
}

func TestSessionPolicyGatewayModeHasNoRestriction(t *testing.T) {
	t.Parallel()
	mode := metamcp.DiscoveryModeDirect
	original := &SessionPolicy{Resource: "meta_mcp_server:" + uuid.NewString(), Gateway: &GatewayOptions{Frozen: nil, DiscoveryMode: &mode}}
	raw, err := json.Marshal(original)
	require.NoError(t, err)
	parsed, err := ParseSessionPolicy(raw)
	require.NoError(t, err)
	require.Nil(t, parsed.Selection)
	require.Equal(t, original.Resource, parsed.Resource)
	require.Equal(t, mode, *parsed.Gateway.DiscoveryMode)
	var cached SessionPolicy
	require.NoError(t, json.Unmarshal(raw, &cached))
	require.Equal(t, parsed, &cached)
	_, err = ParseSessionSelection(raw)
	require.Error(t, err, "old readers must reject the versioned document")
}

func TestSessionPolicyRejectsMalformedGatewayOptions(t *testing.T) {
	t.Parallel()
	resource := "meta_mcp_server:" + uuid.NewString()
	valid := `{"version":1,"resource":"` + resource + `","gateway":{"discovery_mode":"direct"}}`
	invalid := []string{
		`null`, `{}`, `{"version":2,"resource":"` + resource + `","gateway":{"discovery_mode":"direct"}}`,
		`{"version":1,"resource":"toolset:` + uuid.NewString() + `","gateway":{"discovery_mode":"direct"}}`,
		`{"version":1,"resource":"` + resource + `","gateway":{"discovery_mode":"unknown"}}`,
		`{"version":1,"resource":"` + resource + `","gateway":{}}`,
		`{"version":1,"resource":"` + resource + `","gateway":{"discovery_mode":"direct","extra":true}}`,
		`{"version":1,"resource":"` + resource + `","gateway":{"discovery_mode":"direct"},"selection":{}}`,
		valid + ` {}`,
	}
	for _, raw := range invalid {
		policy, err := ParseSessionPolicy([]byte(raw))
		require.Error(t, err, raw)
		require.Nil(t, policy, raw)
	}
}
