package triggers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeTargetKindRequiresExplicitValue(t *testing.T) {
	t.Parallel()

	got, err := normalizeTargetKind("")

	require.Error(t, err)
	require.Empty(t, got)
	require.ErrorContains(t, err, "target_kind is required")
}

func TestNormalizeTargetKindAcceptsNoop(t *testing.T) {
	t.Parallel()

	got, err := normalizeTargetKind(targetKindNoop)

	require.NoError(t, err)
	require.Equal(t, targetKindNoop, got)
}

func TestBuildConfigureTriggerInputSchemaProjectScopedExposesTargetFields(t *testing.T) {
	t.Parallel()

	schema := decodeConfigureSchema(t, buildConfigureTriggerInputSchema(false))

	props := configureSchemaProperties(t, schema)
	require.Contains(t, props, "target_kind")
	require.Contains(t, props, "target_ref")
	require.Contains(t, props, "target_display")
}

func TestBuildConfigureTriggerInputSchemaAssistantScopedStripsTargetFields(t *testing.T) {
	t.Parallel()

	schema := decodeConfigureSchema(t, buildConfigureTriggerInputSchema(true))

	props := configureSchemaProperties(t, schema)
	require.NotContains(t, props, "target_kind", "assistant-scoped schema must not expose target_kind to the LLM")
	require.NotContains(t, props, "target_ref", "assistant-scoped schema must not expose target_ref to the LLM")
	require.NotContains(t, props, "target_display", "assistant-scoped schema must not expose target_display to the LLM")

	required := configureSchemaRequired(t, schema)
	for _, name := range []string{"target_kind", "target_ref", "target_display"} {
		require.NotContains(t, required, name, "%s must not be required when stripped", name)
	}
}

func decodeConfigureSchema(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func configureSchemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	outer, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "schema missing top-level properties")
	input, ok := outer["input"].(map[string]any)
	require.True(t, ok, "schema missing input envelope")
	props, ok := input["properties"].(map[string]any)
	require.True(t, ok, "schema missing input.properties")
	return props
}

func configureSchemaRequired(t *testing.T, schema map[string]any) []string {
	t.Helper()
	outer, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	input, ok := outer["input"].(map[string]any)
	require.True(t, ok)
	raw, ok := input["required"].([]any)
	require.True(t, ok, "schema missing input.required")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
