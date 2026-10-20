package triggers

import (
	"encoding/json"
	"testing"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/stretchr/testify/require"
)

func TestGoogleChatConfigurationIsAvailableToProjectAndAssistantTools(t *testing.T) {
	t.Parallel()
	definition, ok := bgtriggers.GetDefinition("googlechat")
	require.True(t, ok)
	var configSchema map[string]any
	require.NoError(t, json.Unmarshal(definition.ConfigSchema, &configSchema))
	for _, selfScoped := range []bool{false, true} {
		var schema map[string]any
		require.NoError(t, json.Unmarshal(buildConfigureTriggerInputSchema(selfScoped), &schema))
		input := getMap(getMap(schema, "properties"), "input")
		properties := getMap(input, "properties")
		require.Contains(t, getMap(properties, "definition_slug")["enum"], "googlechat")
		found := false
		branches, ok := input["oneOf"].([]any)
		require.True(t, ok)
		for _, branch := range branches {
			branchMap, ok := branch.(map[string]any)
			require.True(t, ok)
			props := getMap(branchMap, "properties")
			if getMap(props, "definition_slug")["const"] == "googlechat" {
				require.Equal(t, configSchema, props["config"])
				found = true
			}
		}
		require.True(t, found, "Google Chat must expose its config through existing management tools")
		if selfScoped {
			require.NotContains(t, properties, "target_ref")
			require.NotContains(t, properties, "target_kind")
		}
	}
}
