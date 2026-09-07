package openrouter

import (
	"encoding/json"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/stretchr/testify/require"
)

// What actually goes on the wire for a strict json_schema response format.
func TestResponseFormatJSONSchemaMarshal(t *testing.T) {
	t.Parallel()

	strict := true
	cfg := or.ChatJSONSchemaConfig{
		Name:        "mcp_research_report",
		Description: nil,
		Schema:      map[string]any{"type": "object", "required": []any{"summary"}, "properties": map[string]any{"summary": map[string]any{"type": "string"}}},
		Strict:      optionalnullable.From(&strict),
	}
	jsonSchemaConfig := or.ChatFormatJSONSchemaConfig{
		Type:       or.ChatFormatJSONSchemaConfigTypeJSONSchema,
		JSONSchema: cfg,
	}
	responseFormat := or.CreateResponseFormatJSONSchema(jsonSchemaConfig)

	reqBody := OpenAIChatRequest{
		Model:          "anthropic/claude-sonnet-5",
		ResponseFormat: &responseFormat,
	}
	raw, err := json.Marshal(reqBody)
	require.NoError(t, err)
	t.Logf("wire: %s", raw)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	rf, ok := decoded["response_format"].(map[string]any)
	require.True(t, ok, "response_format missing entirely: %s", raw)
	require.Equal(t, "json_schema", rf["type"])
	js, ok := rf["json_schema"].(map[string]any)
	require.True(t, ok, "json_schema envelope missing: %s", raw)
	schema, ok := js["schema"].(map[string]any)
	require.True(t, ok, "schema payload missing: %s", raw)
	require.Contains(t, schema, "properties")
	require.Equal(t, true, js["strict"])
}
