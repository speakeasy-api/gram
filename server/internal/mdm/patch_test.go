package mdm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPatch_InjectsAllKeys(t *testing.T) {
	settings := map[string]any{}
	patch(settings, "myproject", "mykey", "https://example.com")

	env := settings["env"].(map[string]any)
	assert.Equal(t, "1", env["CLAUDE_CODE_ENABLE_TELEMETRY"])
	assert.Equal(t, "https://example.com/rpc/hooks.otel", env["OTEL_EXPORTER_OTLP_ENDPOINT"])
	assert.Equal(t, "Gram-Project=myproject,Gram-Key=mykey", env["OTEL_EXPORTER_OTLP_HEADERS"])
	assert.Equal(t, "http/json", env["OTEL_EXPORTER_OTLP_PROTOCOL"])
	assert.Equal(t, "otlp", env["OTEL_LOGS_EXPORTER"])
	assert.Equal(t, "otlp", env["OTEL_METRICS_EXPORTER"])

	mp := settings["extraKnownMarketplaces"].(map[string]any)
	gram := mp["gram"].(map[string]any)
	assert.Equal(t, true, gram["autoUpdate"])

	plugins := settings["enabledPlugins"].(map[string]any)
	assert.Equal(t, true, plugins["gram-hooks@gram"])
}

func TestPatch_PreservesExistingSettings(t *testing.T) {
	settings := map[string]any{
		"env": map[string]any{
			"MY_CUSTOM_VAR": "keep-me",
		},
		"theme": "dark",
	}
	patch(settings, "proj", "key", "https://example.com")

	env := settings["env"].(map[string]any)
	assert.Equal(t, "keep-me", env["MY_CUSTOM_VAR"])
	assert.Equal(t, "dark", settings["theme"])
}

func TestPatch_OverwritesExistingGramKeys(t *testing.T) {
	settings := map[string]any{
		"env": map[string]any{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "https://old.example.com/otel",
		},
	}
	patch(settings, "proj", "key", "https://new.example.com")

	env := settings["env"].(map[string]any)
	assert.Equal(t, "https://new.example.com/rpc/hooks.otel", env["OTEL_EXPORTER_OTLP_ENDPOINT"])
}

func TestPatch_EmptySettingsCreatesAllKeys(t *testing.T) {
	settings := map[string]any{}
	patch(settings, "", "", "https://example.com")

	assert.NotNil(t, settings["env"])
	assert.NotNil(t, settings["extraKnownMarketplaces"])
	assert.NotNil(t, settings["enabledPlugins"])
}
