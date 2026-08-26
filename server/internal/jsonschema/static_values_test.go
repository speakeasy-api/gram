package jsonschema

import (
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
				"example": "<sample&>",
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
		{SchemaPath: "/$defs/Choice", Keyword: "default", ValueJSON: "{\n  \"mode\": \"fast\"\n}"},
		{SchemaPath: "/$defs/Nullable", Keyword: "const", ValueJSON: "null"},
		{SchemaPath: "/properties/choice/oneOf/0", Keyword: "const", ValueJSON: `"a"`},
		{SchemaPath: "/properties/default/properties/nested", Keyword: "const", ValueJSON: "9007199254740993"},
		{SchemaPath: "/properties/header~1value", Keyword: "const", ValueJSON: `"fixed"`},
		{SchemaPath: "/properties/header~1value", Keyword: "default", ValueJSON: `"fallback"`},
		{SchemaPath: "/properties/header~1value", Keyword: "enum", ValueJSON: "[\n  \"fixed\",\n  \"other\"\n]"},
		{SchemaPath: "/properties/header~1value", Keyword: "example", ValueJSON: `"<sample&>"`},
		{SchemaPath: "/properties/header~1value", Keyword: "examples", ValueJSON: "[\n  \"one\",\n  \"two\"\n]"},
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
		{SchemaPath: "", Keyword: "example", ValueJSON: "{\n  \"const\": \"instance data\",\n  \"default\": \"instance data\"\n}"},
	}, values)
}

func TestStaticValues_RejectsMalformedSchema(t *testing.T) {
	t.Parallel()

	_, err := StaticValues([]byte(`{"type": "object", "properties": `))
	require.ErrorContains(t, err, "decode schema")

	_, err = StaticValues([]byte(`{"type": "object"} {"type": "string"}`))
	require.ErrorContains(t, err, "decode schema: trailing data")
}
