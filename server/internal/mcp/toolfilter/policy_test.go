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

	githubID := uuid.New()
	raw, err := json.Marshal(&SessionPolicy{Resource: resource, Selection: selection, Gateway: &GatewayOptions{DiscoveryMode: nil}, Members: map[string]uuid.UUID{"github": githubID}})
	require.NoError(t, err)
	parsed, err := ParseSessionPolicy(raw)
	require.NoError(t, err)
	require.Nil(t, parsed.Gateway.DiscoveryMode)
	require.Equal(t, map[string]uuid.UUID{"github": githubID}, parsed.Members)
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
	slackID := uuid.New()
	consented := map[string]uuid.UUID{"github": memberID, "linear": uuid.New(), "slack": slackID}
	member, err := selection.ForGatewayMember("github", memberID, consented)
	require.NoError(t, err)
	require.Equal(t, "mcp_server:"+memberID.String(), member.Resource)
	require.NotEqual(t, grantID, member.GrantID)
	require.True(t, member.AllowsName("create_issue"))
	require.True(t, member.AllowsName("list_issues"))
	require.False(t, member.AllowsName("github--create_issue"))
	require.Equal(t, []string{AnnotationIdempotent}, member.LiveAnnotations())

	unlisted, err := selection.ForGatewayMember("slack", slackID, consented)
	require.NoError(t, err)
	require.False(t, unlisted.AllowsName("create_issue"))
	require.Equal(t, []string{AnnotationIdempotent}, unlisted.LiveAnnotations())

	_, err = (&SessionSelection{Resource: testResource}).ForGatewayMember("github", memberID, consented)
	require.Error(t, err)
}

// A member replaced by a different server under the same slug inherits
// nothing the user approved for the server shown at consent.
func TestForGatewayMemberRefusesReplacedMemberServer(t *testing.T) {
	t.Parallel()
	live := AnnotationModeLive
	selection, err := NewSessionSelection("meta_mcp_server:"+uuid.NewString(), uuid.New(), []AllowEntry{
		{Type: AllowTypeTool, Name: "github--create_issue", Mode: nil, Tools: nil},
		{Type: AllowTypeAnnotation, Name: AnnotationReadOnly, Mode: &live, Tools: nil},
	})
	require.NoError(t, err)

	replacement := uuid.New()
	member, err := selection.ForGatewayMember("github", replacement, map[string]uuid.UUID{"github": uuid.New()})
	require.NoError(t, err)
	require.Equal(t, "mcp_server:"+replacement.String(), member.Resource)
	require.False(t, member.AllowsName("create_issue"))
	require.Empty(t, member.LiveAnnotations())
}

// Live annotation grants reach only members whose tools were shown at
// consent, not members that were unavailable then or were added later.
func TestForGatewayMemberScopesLiveGrantsToConsentedMembers(t *testing.T) {
	t.Parallel()
	live := AnnotationModeLive
	selection, err := NewSessionSelection("meta_mcp_server:"+uuid.NewString(), uuid.New(), []AllowEntry{
		{Type: AllowTypeAnnotation, Name: AnnotationReadOnly, Mode: &live, Tools: nil},
	})
	require.NoError(t, err)
	shown := uuid.New()
	consented := map[string]uuid.UUID{"docs": shown}

	member, err := selection.ForGatewayMember("docs", shown, consented)
	require.NoError(t, err)
	require.Equal(t, []string{AnnotationReadOnly}, member.LiveAnnotations())

	added, err := selection.ForGatewayMember("added-later", uuid.New(), consented)
	require.NoError(t, err)
	require.Empty(t, added.LiveAnnotations())
	require.False(t, added.AllowsName("anything"))
}

func TestSessionPolicyGatewaySelectionRequiresConsentedMembers(t *testing.T) {
	t.Parallel()
	resource := "meta_mcp_server:" + uuid.NewString()
	selection, err := NewSessionSelection(resource, uuid.New(), []AllowEntry{
		{Type: AllowTypeTool, Name: "github--create_issue", Mode: nil, Tools: nil},
	})
	require.NoError(t, err)
	_, err = json.Marshal(&SessionPolicy{Resource: resource, Selection: selection, Gateway: &GatewayOptions{DiscoveryMode: nil}, Members: nil})
	require.Error(t, err)

	stored := `{"version":1,"resource":"` + resource + `","gateway":{},"selection":{"resource":"` + resource + `","grant_id":"` + uuid.NewString() + `","allow":[{"type":"tool","name":"github--create_issue"}]}}`
	_, err = ParseSessionPolicy([]byte(stored))
	require.Error(t, err, "a stored gateway selection without consented members fails closed")

	empty, err := NewSessionSelection(resource, uuid.New(), []AllowEntry{})
	require.NoError(t, err)
	raw, err := json.Marshal(&SessionPolicy{Resource: resource, Selection: empty, Gateway: &GatewayOptions{DiscoveryMode: nil}, Members: nil})
	require.NoError(t, err, "a selection granting nothing needs no members")
	_, err = ParseSessionPolicy(raw)
	require.NoError(t, err)

	_, err = json.Marshal(&SessionPolicy{Resource: resource, Selection: nil, Gateway: &GatewayOptions{DiscoveryMode: nil}, Members: map[string]uuid.UUID{"github": uuid.New()}})
	require.Error(t, err)
}
