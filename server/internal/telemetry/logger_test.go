package telemetry

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

func TestParseAttributesInfersProviderFromBlankExplicitValue(t *testing.T) {
	t.Parallel()

	spanJSON, _, err := parseAttributesWithExplicitResources(map[attr.Key]any{
		attr.GenAIRequestModelKey: "anthropic/claude-sonnet-4",
		attr.GenAIProviderNameKey: "  ",
	}, nil)
	require.NoError(t, err)

	var spanAttrs map[string]any
	require.NoError(t, json.Unmarshal([]byte(spanJSON), &spanAttrs))
	require.Equal(t, "anthropic", spanAttrs[string(attr.GenAIProviderNameKey)])
}

func TestParseAttributesInfersProviderWhenAbsent(t *testing.T) {
	t.Parallel()

	spanJSON, _, err := parseAttributesWithExplicitResources(map[attr.Key]any{
		attr.GenAIRequestModelKey: "openai/gpt-4o",
	}, nil)
	require.NoError(t, err)

	var spanAttrs map[string]any
	require.NoError(t, json.Unmarshal([]byte(spanJSON), &spanAttrs))
	require.Equal(t, "openai", spanAttrs[string(attr.GenAIProviderNameKey)])
}

func TestParseAttributesInfersProviderFromNonStringValue(t *testing.T) {
	t.Parallel()

	spanJSON, _, err := parseAttributesWithExplicitResources(map[attr.Key]any{
		attr.GenAIRequestModelKey: "anthropic/claude-sonnet-4",
		attr.GenAIProviderNameKey: 42,
	}, nil)
	require.NoError(t, err)

	var spanAttrs map[string]any
	require.NoError(t, json.Unmarshal([]byte(spanJSON), &spanAttrs))
	require.Equal(t, "anthropic", spanAttrs[string(attr.GenAIProviderNameKey)])
}

func TestParseAttributesPreservesExplicitProvider(t *testing.T) {
	t.Parallel()

	spanJSON, _, err := parseAttributesWithExplicitResources(map[attr.Key]any{
		attr.GenAIRequestModelKey: "anthropic/claude-sonnet-4",
		attr.GenAIProviderNameKey: "custom-provider",
	}, nil)
	require.NoError(t, err)

	var spanAttrs map[string]any
	require.NoError(t, json.Unmarshal([]byte(spanJSON), &spanAttrs))
	require.Equal(t, "custom-provider", spanAttrs[string(attr.GenAIProviderNameKey)])
}

func TestScrubToolIODropsToolContentButKeepsTheSkillName(t *testing.T) {
	t.Parallel()

	tool := map[attr.Key]any{
		attr.ToolNameKey:               "Bash",
		attr.GenAIToolCallArgumentsKey: `{"command":"cat secrets.txt"}`,
		attr.GenAIToolCallResultKey:    "hunter2",
		attr.HookEventKey:              "PostToolUse",
	}
	ScrubToolIO(tool)
	require.NotContains(t, tool, attr.GenAIToolCallArgumentsKey)
	require.NotContains(t, tool, attr.GenAIToolCallResultKey)
	require.Equal(t, "PostToolUse", tool[attr.HookEventKey], "only tool IO is scrubbed")

	skill := map[attr.Key]any{
		attr.ToolNameKey:               "Skill",
		attr.GenAIToolCallArgumentsKey: `{"skill":"repo-review","extra":"dropped"}`,
		attr.GenAIToolCallResultKey:    "activated",
	}
	ScrubToolIO(skill)
	kept, ok := skill[attr.GenAIToolCallArgumentsKey].(string)
	require.True(t, ok)
	require.JSONEq(t, `{"skill":"repo-review"}`, kept, "skill analytics need the name")
	require.NotContains(t, skill, attr.GenAIToolCallResultKey)
}
