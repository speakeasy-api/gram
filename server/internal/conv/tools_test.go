package conv

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripSchemaField(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]interface{}
	}{
		{
			name: "strips $schema field from valid schema",
			input: `{
				"$schema": "https://json-schema.org/draft/2020-12/schema",
				"type": "object",
				"properties": {
					"name": {
						"type": "string"
					}
				}
			}`,
			expected: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
		{
			name: "handles schema without $schema field",
			input: `{
				"type": "object",
				"properties": {}
			}`,
			expected: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			name:  "handles empty string",
			input: "",
			expected: map[string]interface{}{
				"empty": true,
			},
		},
		{
			name:  "handles invalid JSON gracefully",
			input: "{invalid json",
			expected: map[string]interface{}{
				"unchanged": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripSchemaField(tt.input)

			if tt.name == "handles empty string" {
				require.Equal(t, "", result)
				return
			}

			if tt.name == "handles invalid JSON gracefully" {
				require.Equal(t, tt.input, result)
				return
			}

			// Parse result and compare
			var resultMap map[string]interface{}
			err := json.Unmarshal([]byte(result), &resultMap)
			require.NoError(t, err)

			// Verify $schema field is not present
			_, hasSchemaField := resultMap["$schema"]
			require.False(t, hasSchemaField, "$schema field should be removed")

			// Verify all other fields are preserved
			require.Equal(t, tt.expected["type"], resultMap["type"])
			require.NotNil(t, resultMap["properties"])
		})
	}
}

func TestStripSchemaFieldPreservesOtherFields(t *testing.T) {
	input := `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"title": "Test Schema",
		"description": "A test schema",
		"required": ["name", "age"],
		"properties": {
			"name": {
				"type": "string",
				"description": "Person's name"
			},
			"age": {
				"type": "integer",
				"minimum": 0
			}
		}
	}`

	result := stripSchemaField(input)

	var resultMap map[string]interface{}
	err := json.Unmarshal([]byte(result), &resultMap)
	require.NoError(t, err)

	// Verify $schema is removed
	_, hasSchemaField := resultMap["$schema"]
	require.False(t, hasSchemaField)

	// Verify all other important fields are preserved
	require.Equal(t, "object", resultMap["type"])
	require.Equal(t, "Test Schema", resultMap["title"])
	require.Equal(t, "A test schema", resultMap["description"])
	require.NotNil(t, resultMap["required"])
	require.NotNil(t, resultMap["properties"])

	// Verify required array is preserved
	requiredArray, ok := resultMap["required"].([]interface{})
	require.True(t, ok)
	require.Len(t, requiredArray, 2)
}
