package core

import (
	"encoding/json"
	"reflect"
	"testing"

	gjsonschema "github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
)

func TestBuildInputSchema(t *testing.T) {
	t.Parallel()

	type nested struct {
		Value string `json:"value"`
	}

	type input struct {
		From   *string `json:"from,omitempty" jsonschema:"Start time."`
		Limit  int     `json:"limit,omitempty" jsonschema:"Result limit."`
		Filter nested  `json:"filter" jsonschema:"Filter payload."`
	}

	schemaBytes := BuildInputSchema[input](
		WithTypeSchema(reflect.TypeFor[nested](), PermissiveObjectSchema()),
		WithPropertyFormat("from", "date-time"),
		WithPropertyEnum("limit", 10, 20),
		WithPropertyMutator("limit", func(prop *gjsonschema.Schema) {
			prop.Description = "Overridden limit."
		}),
	)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &schema))

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	fromSchema, ok := properties["from"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Start time.", fromSchema["description"])
	require.Equal(t, "date-time", fromSchema["format"])

	limitSchema, ok := properties["limit"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Overridden limit.", limitSchema["description"])
	require.ElementsMatch(t, []any{float64(10), float64(20)}, limitSchema["enum"])

	filterSchema, ok := properties["filter"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "object", filterSchema["type"])
}
