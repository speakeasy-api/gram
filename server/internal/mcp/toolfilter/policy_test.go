package toolfilter

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/stretchr/testify/require"
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
	original := &SessionPolicy{Resource: "meta_mcp_server:" + uuid.NewString(), Gateway: &GatewayOptions{DiscoveryMode: &mode}}
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

func TestSessionPolicyRejectsMisboundGatewaySelection(t *testing.T) {
	t.Parallel()
	resource := "meta_mcp_server:" + uuid.NewString()
	other, err := NewSessionSelection("meta_mcp_server:"+uuid.NewString(), uuid.New(), []AllowEntry{})
	require.NoError(t, err)
	_, err = json.Marshal(&SessionPolicy{Resource: resource, Selection: other, Gateway: &GatewayOptions{DiscoveryMode: nil}, Catalog: &CatalogReference{ID: uuid.New(), Mode: CatalogModeCached, AllTools: false}})
	require.Error(t, err)

	legacy, err := json.Marshal(other)
	require.NoError(t, err)
	_, err = ParseSessionPolicy(legacy)
	require.Error(t, err, "gateway selections are only valid inside a versioned policy")
}

func TestSessionPolicyRejectsV1GatewaySelection(t *testing.T) {
	t.Parallel()
	resource := "meta_mcp_server:" + uuid.NewString()
	selection, err := NewSessionSelection(resource, uuid.New(), []AllowEntry{})
	require.NoError(t, err)
	raw, err := json.Marshal(map[string]any{"version": 1, "resource": resource, "gateway": map[string]string{"discovery_mode": "direct"}, "selection": selection})
	require.NoError(t, err)
	_, err = ParseSessionPolicy(raw)
	require.Error(t, err, "v1 gateway policies never accepted tool selections")
}

func TestSessionPolicyCatalogDoesNotDuplicateMembersOrTools(t *testing.T) {
	t.Parallel()
	policy := &SessionPolicy{Resource: "meta_mcp_server:" + uuid.NewString(), Gateway: &GatewayOptions{}, Catalog: &CatalogReference{ID: uuid.New(), Mode: CatalogModeCached, AllTools: true}}
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	parsed, err := ParseSessionPolicy(raw)
	require.NoError(t, err)
	require.Equal(t, policy, parsed)
	require.Nil(t, parsed.Selection)
	require.Contains(t, string(raw), `"version":2`)
}

func TestSessionPolicyRejectsLiveCatalogSelections(t *testing.T) {
	t.Parallel()
	resource := "meta_mcp_server:" + uuid.NewString()
	selection, err := NewSessionSelection(resource, uuid.New(), []AllowEntry{{Type: AllowTypeTool, Name: "source--tool"}})
	require.NoError(t, err)
	policy := &SessionPolicy{Resource: resource, Gateway: &GatewayOptions{}, Catalog: &CatalogReference{ID: uuid.New(), Mode: CatalogModeLive, AllTools: false}, Selection: selection}
	_, err = json.Marshal(policy)
	require.ErrorContains(t, err, "live catalog cannot carry a tool selection")
	live := AnnotationModeLive
	selection, err = NewSessionSelection(resource, uuid.New(), []AllowEntry{{Type: AllowTypeAnnotation, Name: AnnotationReadOnly, Mode: &live}})
	require.NoError(t, err)
	policy.Selection = selection
	policy.Catalog.Mode = CatalogModeCached
	policy.Catalog.AllTools = false
	_, err = json.Marshal(policy)
	require.Error(t, err)
}

func TestSessionPolicyValidatesEditedSelectionGrants(t *testing.T) {
	t.Parallel()
	resource := "meta_mcp_server:" + uuid.NewString()
	selection, err := NewSessionSelection(resource, uuid.New(), []AllowEntry{{Type: AllowTypeTool, Name: "source--tool"}})
	require.NoError(t, err)
	// An entry appended after construction leaves the compiled projection
	// stale; marshalling must judge the grants actually being written.
	live := AnnotationModeLive
	selection.Allow = append([]AllowEntry{{Type: AllowTypeAnnotation, Name: AnnotationReadOnly, Mode: &live, Tools: nil}}, selection.Allow...)
	policy := &SessionPolicy{Resource: resource, Gateway: &GatewayOptions{}, Catalog: &CatalogReference{ID: uuid.New(), Mode: CatalogModeCached, AllTools: false}, Selection: selection}
	_, err = json.Marshal(policy)
	require.ErrorContains(t, err, "cached catalog cannot carry live annotation grants")
}
