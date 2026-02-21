// Tests for the MCP client integer enum compatibility layer.
// See integer_enum.go for context on why this exists and when to remove it.
package openapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransformIntegerEnums_IntegerEnum(t *testing.T) {
	t.Parallel()
	schema := []byte(`{"type":"integer","enum":[1,5,15,30,60]}`)
	result := TransformIntegerEnums(schema)

	node := unmarshalMap(t, result)
	require.Nil(t, node["type"])
	require.Nil(t, node["enum"])
	require.Equal(t, []any{
		map[string]any{"type": "integer", "const": float64(1)},
		map[string]any{"type": "integer", "const": float64(5)},
		map[string]any{"type": "integer", "const": float64(15)},
		map[string]any{"type": "integer", "const": float64(30)},
		map[string]any{"type": "integer", "const": float64(60)},
	}, node["anyOf"])
}

func TestTransformIntegerEnums_NumberEnum(t *testing.T) {
	t.Parallel()
	schema := []byte(`{"type":"number","enum":[1.5,2.5,3.5]}`)
	result := TransformIntegerEnums(schema)

	node := unmarshalMap(t, result)
	require.Nil(t, node["type"])
	require.Nil(t, node["enum"])
	require.Equal(t, []any{
		map[string]any{"type": "number", "const": 1.5},
		map[string]any{"type": "number", "const": 2.5},
		map[string]any{"type": "number", "const": 3.5},
	}, node["anyOf"])
}

func TestTransformIntegerEnums_NestedInProperties(t *testing.T) {
	t.Parallel()
	schema := []byte(`{
		"type":"object",
		"properties":{
			"frequency":{"type":"integer","enum":[1,5,15]},
			"name":{"type":"string"}
		}
	}`)
	result := TransformIntegerEnums(schema)

	node := unmarshalMap(t, result)
	require.Equal(t, "object", node["type"])

	props, ok := node["properties"].(map[string]any)
	require.True(t, ok)
	freq, ok := props["frequency"].(map[string]any)
	require.True(t, ok)
	require.Nil(t, freq["type"])
	require.Nil(t, freq["enum"])
	require.Equal(t, []any{
		map[string]any{"type": "integer", "const": float64(1)},
		map[string]any{"type": "integer", "const": float64(5)},
		map[string]any{"type": "integer", "const": float64(15)},
	}, freq["anyOf"])

	name, ok := props["name"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "string", name["type"])
}

func TestTransformIntegerEnums_NestedInItems(t *testing.T) {
	t.Parallel()
	schema := []byte(`{
		"type":"array",
		"items":{"type":"integer","enum":[0,1,2]}
	}`)
	result := TransformIntegerEnums(schema)

	node := unmarshalMap(t, result)
	items, ok := node["items"].(map[string]any)
	require.True(t, ok)
	require.Nil(t, items["type"])
	require.Nil(t, items["enum"])
	require.Equal(t, []any{
		map[string]any{"type": "integer", "const": float64(0)},
		map[string]any{"type": "integer", "const": float64(1)},
		map[string]any{"type": "integer", "const": float64(2)},
	}, items["anyOf"])
}

func TestTransformIntegerEnums_NestedInOneOfAnyOf(t *testing.T) {
	t.Parallel()
	schema := []byte(`{
		"oneOf":[
			{"type":"integer","enum":[1,2]},
			{"type":"string","enum":["a","b"]}
		],
		"anyOf":[
			{"type":"number","enum":[3.0,4.0]}
		]
	}`)
	result := TransformIntegerEnums(schema)

	node := unmarshalMap(t, result)

	oneOf, ok := node["oneOf"].([]any)
	require.True(t, ok)
	first, ok := oneOf[0].(map[string]any)
	require.True(t, ok)
	require.Nil(t, first["type"])
	require.Nil(t, first["enum"])
	require.Equal(t, []any{
		map[string]any{"type": "integer", "const": float64(1)},
		map[string]any{"type": "integer", "const": float64(2)},
	}, first["anyOf"])

	// String enum should be untouched
	second, ok := oneOf[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "string", second["type"])
	require.Equal(t, []any{"a", "b"}, second["enum"])

	anyOf, ok := node["anyOf"].([]any)
	require.True(t, ok)
	third, ok := anyOf[0].(map[string]any)
	require.True(t, ok)
	require.Nil(t, third["type"])
	require.Nil(t, third["enum"])
	require.Equal(t, []any{
		map[string]any{"type": "number", "const": float64(3)},
		map[string]any{"type": "number", "const": float64(4)},
	}, third["anyOf"])
}

func TestTransformIntegerEnums_StringEnumNoOp(t *testing.T) {
	t.Parallel()
	schema := []byte(`{"type":"string","enum":["a","b","c"]}`)
	result := TransformIntegerEnums(schema)

	node := unmarshalMap(t, result)
	require.Equal(t, "string", node["type"])
	require.Equal(t, []any{"a", "b", "c"}, node["enum"])
}

func TestTransformIntegerEnums_NoEnum(t *testing.T) {
	t.Parallel()
	schema := []byte(`{"type":"integer","minimum":0,"maximum":100}`)
	result := TransformIntegerEnums(schema)

	node := unmarshalMap(t, result)
	require.Equal(t, "integer", node["type"])
	require.Nil(t, node["enum"])
	require.Nil(t, node["anyOf"])
}

func TestTransformIntegerEnums_EmptyInput(t *testing.T) {
	t.Parallel()
	require.Nil(t, TransformIntegerEnums(nil))
	require.Equal(t, []byte{}, TransformIntegerEnums([]byte{}))

	result := TransformIntegerEnums([]byte(`{}`))
	node := unmarshalMap(t, result)
	require.Empty(t, node)
}

func TestTransformIntegerEnums_PreservesSiblingKeys(t *testing.T) {
	t.Parallel()
	schema := []byte(`{"type":"integer","enum":[1,2,3],"description":"pick one","default":1}`)
	result := TransformIntegerEnums(schema)

	node := unmarshalMap(t, result)
	require.Equal(t, "pick one", node["description"])
	require.InDelta(t, float64(1), node["default"], 0)
	require.Nil(t, node["type"])
	require.Nil(t, node["enum"])
	require.Len(t, node["anyOf"], 3)
}

func unmarshalMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}
