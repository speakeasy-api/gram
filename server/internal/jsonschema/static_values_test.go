package jsonschema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStaticValues(t *testing.T) {
	t.Parallel()

	values, err := StaticValues([]byte(`{
		"type": "object",
		"properties": {
			"header/value": {
				"type": "string",
				"const": "fixed",
				"default": "fallback",
				"enum": ["fixed", "other"],
				"example": "sample",
				"examples": ["one", "two"]
			},
			"default": {
				"type": "object",
				"properties": {
					"nested": {"const": 9007199254740993}
				}
			},
			"choice": {
				"oneOf": [
					{"const": "a"},
					{"$ref": "#/$defs/Choice"}
				]
			}
		},
		"$defs": {
			"Choice": {"default": {"mode": "fast"}},
			"Nullable": {"const": null}
		}
	}`))
	require.NoError(t, err)
	require.Equal(t, []StaticValue{
		{SchemaPath: "/$defs/Choice", Keyword: "default", Value: map[string]any{"mode": "fast"}},
		{SchemaPath: "/$defs/Nullable", Keyword: "const", Value: nil},
		{SchemaPath: "/properties/choice/oneOf/0", Keyword: "const", Value: "a"},
		{SchemaPath: "/properties/default/properties/nested", Keyword: "const", Value: json.Number("9007199254740993")},
		{SchemaPath: "/properties/header~1value", Keyword: "const", Value: "fixed"},
		{SchemaPath: "/properties/header~1value", Keyword: "default", Value: "fallback"},
		{SchemaPath: "/properties/header~1value", Keyword: "enum", Value: []any{"fixed", "other"}},
		{SchemaPath: "/properties/header~1value", Keyword: "example", Value: "sample"},
		{SchemaPath: "/properties/header~1value", Keyword: "examples", Value: []any{"one", "two"}},
	}, values)
}

func TestStaticValues_DoesNotInterpretExampleDataAsSchema(t *testing.T) {
	t.Parallel()

	values, err := StaticValues([]byte(`{
		"type": "object",
		"example": {"default": "instance data", "const": "instance data"},
		"properties": {
			"default": {"type": "string"}
		}
	}`))
	require.NoError(t, err)
	require.Equal(t, []StaticValue{
		{SchemaPath: "", Keyword: "example", Value: map[string]any{"default": "instance data", "const": "instance data"}},
	}, values)
}

func TestStaticValues_RejectsMalformedSchema(t *testing.T) {
	t.Parallel()

	_, err := StaticValues([]byte(`{"type": "object", "properties": `))
	require.ErrorContains(t, err, "decode schema")
}
