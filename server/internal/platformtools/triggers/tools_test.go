package triggers

import (
	"encoding/json"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/stretchr/testify/require"
)

func TestConfigureTriggerToolDescriptor(t *testing.T) {
	t.Parallel()

	descriptor := NewConfigureTriggerTool(nil, nil, audit.NewLogger()).Descriptor()

	require.Equal(t, toolNameConfigure, descriptor.Name)
	require.Equal(t, sourceTriggers, descriptor.SourceSlug)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema))

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, properties, "definition_slug")
	require.Contains(t, properties, "environment_slug")
	require.Contains(t, properties, "target_ref")
	require.Contains(t, properties, "config")

	targetKindProperty, ok := properties["target_kind"].(map[string]any)
	require.True(t, ok)

	targetKindEnum, ok := targetKindProperty["enum"].([]any)
	require.True(t, ok)
	require.Contains(t, targetKindEnum, targetKindAssistant)
	require.Contains(t, targetKindEnum, targetKindNoop)

	oneOf, ok := schema["oneOf"].([]any)
	require.True(t, ok)
	require.Len(t, oneOf, 2)

	branches := make(map[string]map[string]any, len(oneOf))
	for _, rawBranch := range oneOf {
		branch, ok := rawBranch.(map[string]any)
		require.True(t, ok)

		branchProperties, ok := branch["properties"].(map[string]any)
		require.True(t, ok)

		definitionProperty, ok := branchProperties["definition_slug"].(map[string]any)
		require.True(t, ok)

		slug, ok := definitionProperty["const"].(string)
		require.True(t, ok)
		branches[slug] = branch
	}

	require.Contains(t, branches, "slack")
	require.Contains(t, branches, "cron")

	slackConfig := branchConfig(t, branches["slack"])
	slackProperties, ok := slackConfig["properties"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, slackProperties, "event_types")
	require.Contains(t, slackProperties, "filter")

	cronConfig := branchConfig(t, branches["cron"])
	cronProperties, ok := cronConfig["properties"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, cronProperties, "schedule")
	require.NotContains(t, cronProperties, "filter")
}

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

func branchConfig(t *testing.T, branch map[string]any) map[string]any {
	t.Helper()

	properties, ok := branch["properties"].(map[string]any)
	require.True(t, ok)

	config, ok := properties["config"].(map[string]any)
	require.True(t, ok)
	return config
}
