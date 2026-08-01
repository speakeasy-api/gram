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
