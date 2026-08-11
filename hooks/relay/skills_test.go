package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

func TestResolveActivatedSkillUsesNormalizedEventContent(t *testing.T) {
	content := "---\r\nname: review\r\n---\r\nbody\r\n"
	event := completedClaudeSkillEvent(t, "review", content)

	resolved := resolveActivatedSkill(event, activatedSkillPayload("review"))

	require.Equal(t, "review", resolved.name)
	require.Equal(t, content, resolved.content)
	require.Equal(t, sha256Hex([]byte(content)), resolved.rawSHA256)
	require.True(t, resolved.captureReady)
}

func TestResolveActivatedSkillCapturesEmptyNormalizedContent(t *testing.T) {
	resolved := resolveActivatedSkill(completedClaudeSkillEvent(t, "empty", ""), activatedSkillPayload("empty"))

	require.True(t, resolved.captureReady)
	require.Empty(t, resolved.content)
	require.Equal(t, sha256Hex(nil), resolved.rawSHA256)
}

func TestResolveActivatedSkillPreEventIsNameOnly(t *testing.T) {
	input, err := json.Marshal(map[string]string{"skill": "review"})
	require.NoError(t, err)
	event := &agenthooks.ToolPreEvent{
		Event: agenthooks.Event{Provider: agenthooks.ProviderClaudeCode, Kind: agenthooks.KindToolPre},
		Tool:  agenthooks.ToolCall{Name: "Skill", Input: input},
	}

	resolved := resolveActivatedSkill(event, activatedSkillPayload("review"))

	require.Equal(t, "review", resolved.name)
	require.False(t, resolved.captureReady)
	require.Empty(t, resolved.rawSHA256)
}

func TestResolveActivatedSkillRejectsOversizedNormalizedContent(t *testing.T) {
	content := strings.Repeat("x", maxSkillContentBytes+1)
	resolved := resolveActivatedSkill(completedClaudeSkillEvent(t, "large", content), activatedSkillPayload("large"))

	require.False(t, resolved.captureReady)
	require.Empty(t, resolved.rawSHA256)
}

func TestResolveActivatedSkillWithoutSkillReturnsNil(t *testing.T) {
	require.Nil(t, resolveActivatedSkill(nil, nil))
	require.Nil(t, resolveActivatedSkill(nil, &components.IngestRequestBody{}))
	require.Nil(t, resolveActivatedSkill(nil, &components.IngestRequestBody{Data: &components.HookIngestData{}}))
}

func completedClaudeSkillEvent(t *testing.T, name, content string) *agenthooks.ToolPostEvent {
	t.Helper()
	input, err := json.Marshal(map[string]string{"skill": name})
	require.NoError(t, err)
	output, err := json.Marshal(content)
	require.NoError(t, err)
	return &agenthooks.ToolPostEvent{
		Event:  agenthooks.Event{Provider: agenthooks.ProviderClaudeCode, Kind: agenthooks.KindToolPost},
		Tool:   agenthooks.ToolCall{Name: "Skill", Input: input},
		Output: output,
		Failed: false,
	}
}

func codexToolEvent(t *testing.T, cwd, name string, input any) *agenthooks.ToolPreEvent {
	t.Helper()
	encoded, err := json.Marshal(input)
	require.NoError(t, err)
	return &agenthooks.ToolPreEvent{
		Event: agenthooks.Event{Provider: agenthooks.ProviderCodex, Kind: agenthooks.KindToolPre, Session: agenthooks.SessionInfo{CWD: cwd}},
		Tool:  agenthooks.ToolCall{Name: name, Canonical: agenthooks.CanonicalToolFor(name), Input: encoded},
	}
}

func activatedSkillPayload(name string) *components.IngestRequestBody {
	return &components.IngestRequestBody{Data: &components.HookIngestData{Skill: &components.HookSkillData{Name: name, Source: nil}}}
}

func sha256Hex(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
