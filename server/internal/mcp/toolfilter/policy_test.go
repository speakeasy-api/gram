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

func TestSessionPolicyGatewaySelectionRoundTrip(t *testing.T) {
	t.Parallel()
	resource := "meta_mcp_server:" + uuid.NewString()
	live := AnnotationModeLive
	selection, err := NewSessionSelection(resource, uuid.New(), []AllowEntry{
		{Type: AllowTypeTool, Name: "github--create_issue", Mode: nil, Tools: nil},
		{Type: AllowTypeAnnotation, Name: AnnotationReadOnly, Mode: &live, Tools: nil},
	})
	require.NoError(t, err)

	raw, err := json.Marshal(&SessionPolicy{Resource: resource, Selection: selection, Gateway: &GatewayOptions{DiscoveryMode: nil}})
	require.NoError(t, err)
	parsed, err := ParseSessionPolicy(raw)
	require.NoError(t, err)
	require.Nil(t, parsed.Gateway.DiscoveryMode)
	require.True(t, parsed.Selection.AllowsName("github--create_issue"))
	require.Equal(t, []string{AnnotationReadOnly}, parsed.Selection.LiveAnnotations())
}

func TestSessionPolicyRejectsMisboundGatewaySelection(t *testing.T) {
	t.Parallel()
	resource := "meta_mcp_server:" + uuid.NewString()
	other, err := NewSessionSelection("meta_mcp_server:"+uuid.NewString(), uuid.New(), []AllowEntry{})
	require.NoError(t, err)
	_, err = json.Marshal(&SessionPolicy{Resource: resource, Selection: other, Gateway: &GatewayOptions{DiscoveryMode: nil}})
	require.Error(t, err)

	legacy, err := json.Marshal(other)
	require.NoError(t, err)
	_, err = ParseSessionPolicy(legacy)
	require.Error(t, err, "gateway selections are only valid inside a versioned policy")
}

func TestForGatewayMemberProjectsMemberTools(t *testing.T) {
	t.Parallel()
	resource := "meta_mcp_server:" + uuid.NewString()
	grantID := uuid.New()
	snapshot := AnnotationModeSnapshot
	live := AnnotationModeLive
	frozen := []string{"github--list_issues", "linear--list_issues"}
	selection, err := NewSessionSelection(resource, grantID, []AllowEntry{
		{Type: AllowTypeTool, Name: "github--create_issue", Mode: nil, Tools: nil},
		{Type: AllowTypeTool, Name: "linear--create_issue", Mode: nil, Tools: nil},
		{Type: AllowTypeAnnotation, Name: AnnotationReadOnly, Mode: &snapshot, Tools: &frozen},
		{Type: AllowTypeAnnotation, Name: AnnotationIdempotent, Mode: &live, Tools: nil},
	})
	require.NoError(t, err)

	memberID := uuid.New()
	member, err := selection.ForGatewayMember("github", memberID)
	require.NoError(t, err)
	require.Equal(t, "mcp_server:"+memberID.String(), member.Resource)
	require.NotEqual(t, grantID, member.GrantID)
	require.True(t, member.AllowsName("create_issue"))
	require.True(t, member.AllowsName("list_issues"))
	require.False(t, member.AllowsName("github--create_issue"))
	require.Equal(t, []string{AnnotationIdempotent}, member.LiveAnnotations())

	unlisted, err := selection.ForGatewayMember("slack", uuid.New())
	require.NoError(t, err)
	require.False(t, unlisted.AllowsName("create_issue"))
	require.Equal(t, []string{AnnotationIdempotent}, unlisted.LiveAnnotations())

	_, err = (&SessionSelection{Resource: testResource}).ForGatewayMember("github", memberID)
	require.Error(t, err)
}
