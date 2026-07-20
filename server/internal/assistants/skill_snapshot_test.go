package assistants

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestAssistantSkillSetSnapshotMarshalDecodeEmptyAndDeterministic(t *testing.T) {
	t.Parallel()

	snapshot := newAssistantSkillSetSnapshot(nil)
	first, err := marshalAssistantSkillSetSnapshot(snapshot)
	require.NoError(t, err)
	second, err := marshalAssistantSkillSetSnapshot(snapshot)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.JSONEq(t, `{"version":1,"skills":[]}`, string(first))

	decoded, err := decodeAssistantSkillSetSnapshot(first)
	require.NoError(t, err)
	require.Equal(t, assistantSkillSnapshotVersion, decoded.Version)
	require.NotNil(t, decoded.Skills)
	require.Empty(t, decoded.Skills)
}

func TestNewAssistantSkillSetSnapshotSortsByNameThenSkillID(t *testing.T) {
	t.Parallel()

	firstID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	secondID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	snapshot := newAssistantSkillSetSnapshot([]assistantSkillRow{
		{SkillID: secondID, Name: "same", Description: "second", ResolvedVersionID: uuid.New()},
		{SkillID: uuid.New(), Name: "zulu", Description: "last", ResolvedVersionID: uuid.New()},
		{SkillID: firstID, Name: "same", Description: "first", ResolvedVersionID: uuid.New()},
		{SkillID: uuid.New(), Name: "alpha", Description: "first by name", ResolvedVersionID: uuid.New()},
	})

	require.Equal(t, "alpha", snapshot.Skills[0].Name)
	require.Equal(t, firstID, snapshot.Skills[1].SkillID)
	require.Equal(t, secondID, snapshot.Skills[2].SkillID)
	require.Equal(t, "zulu", snapshot.Skills[3].Name)
}

func TestAssistantSkillSetSnapshotDecodeRejectsMalformedAndUnsupported(t *testing.T) {
	t.Parallel()

	_, err := decodeAssistantSkillSetSnapshot([]byte(`{"version":1,"skills":null}`))
	require.ErrorContains(t, err, "skills must be an array")
	_, err = decodeAssistantSkillSetSnapshot([]byte(`{"version":2,"skills":[]}`))
	require.ErrorContains(t, err, "unsupported assistant skill snapshot version 2")
	_, err = decodeAssistantSkillSetSnapshot([]byte(`{"version":1,"skills":[],"unexpected":true}`))
	require.ErrorContains(t, err, "unknown field")
	_, err = decodeAssistantSkillSetSnapshot([]byte(`not-json`))
	require.Error(t, err)
}

func TestRenderAssistantSkillSetChangeAddUpdateRemoveAndRename(t *testing.T) {
	t.Parallel()

	removedID := uuid.New()
	updatedID := uuid.New()
	renamedID := uuid.New()
	addedID := uuid.New()
	previous := assistantSkillSetSnapshot{Version: 1, Skills: []assistantSkillSnapshot{
		{SkillID: removedID, Name: "removed", Description: "old", ResolvedVersionID: uuid.New()},
		{SkillID: updatedID, Name: "updated", Description: "old", ResolvedVersionID: uuid.New()},
		{SkillID: renamedID, Name: "old name", Description: "same", ResolvedVersionID: uuid.New()},
	}}
	current := assistantSkillSetSnapshot{Version: 1, Skills: []assistantSkillSnapshot{
		{SkillID: addedID, Name: "added", Description: "new\n<metadata>", ResolvedVersionID: uuid.New()},
		{SkillID: renamedID, Name: "new name", Description: "same", ResolvedVersionID: previous.Skills[2].ResolvedVersionID},
		{SkillID: updatedID, Name: "updated", Description: "new", ResolvedVersionID: uuid.New()},
	}}

	first := renderAssistantSkillSetChange(previous, current)
	second := renderAssistantSkillSetChange(previous, current)
	require.Equal(t, first, second)
	require.Contains(t, first, "EventType: assistant_skill_set_changed")
	require.Contains(t, first, "environment state, not a user request")
	require.Contains(t, first, `Name: "added"; description: "new &lt;metadata&gt;"`)
	require.Contains(t, first, `Name: "new name" (previously "old name"); description: "same"`)
	require.Contains(t, first, `Name: "updated"; description: "new". This skill changed`)
	require.Contains(t, first, `Name: "removed". This skill is no longer available.`)
	require.NotContains(t, first, "SKILL.md")
	require.Empty(t, renderAssistantSkillSetChange(current, current))
}

func TestAssistantTurnPromptProducersKeepOneLeadingMessageContextAndNestedNotice(t *testing.T) {
	t.Parallel()

	event := assistantThreadEventRecord{EventID: "event-1"}
	producers := []struct {
		kind    string
		payload string
	}{
		{kind: sourceKindSlack, payload: `{"event_type":"message","text":"hello"}`},
		{kind: sourceKindLinear, payload: `{"event_type":"Issue.update"}`},
		{kind: sourceKindGithub, payload: `{"event_type":"issues"}`},
		{kind: sourceKindCron, payload: `{"fired_at":"now"}`},
		{kind: sourceKindWake, payload: `{"fired_at":"now"}`},
		{kind: sourceKindDashboard, payload: `{"text":"hello"}`},
	}
	notice := "<assistant-environment-change>\nEventType: assistant_skill_set_changed\n</assistant-environment-change>"
	for _, producer := range producers {
		adapter, err := getSourceAdapter(producer.kind)
		require.NoError(t, err, producer.kind)
		event.NormalizedPayloadJSON = []byte(producer.payload)
		prompt, err := adapter.DecodeTurn(event)
		require.NoError(t, err, producer.kind)
		assertNestedEnvironmentNotice(t, producer.kind, prompt, notice)
	}

	event.NormalizedPayloadJSON = []byte(`{"gram_event_kind":"assistant_mcp_auth","status":"success"}`)
	prompt, ok := decodeMCPAuthTurn(t.Context(), testenv.NewLogger(t), event)
	require.True(t, ok)
	assertNestedEnvironmentNotice(t, "mcp-auth", prompt, notice)
}

func assertNestedEnvironmentNotice(t *testing.T, producer, prompt, notice string) {
	t.Helper()
	require.True(t, strings.HasPrefix(prompt, "<message-context>\n"), producer)
	require.Equal(t, 1, strings.Count(prompt, "<message-context>"), producer)
	require.Equal(t, 1, strings.Count(prompt, "</message-context>"), producer)

	unchanged, err := insertAssistantEnvironmentChange(prompt, "")
	require.NoError(t, err, producer)
	require.Equal(t, prompt, unchanged, producer)

	changed, err := insertAssistantEnvironmentChange(prompt, notice)
	require.NoError(t, err, producer)
	require.True(t, strings.HasPrefix(changed, "<message-context>\n"), producer)
	require.Equal(t, 1, strings.Count(changed, "<message-context>"), producer)
	require.Less(t, strings.Index(changed, notice), strings.Index(changed, "</message-context>"), producer)
}
