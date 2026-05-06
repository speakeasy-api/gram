package gateway

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const nestedObjectSchema = `{
  "type": "object",
  "properties": {
    "user": {
      "type": "object",
      "properties": {
        "name": { "type": "string" },
        "age": { "type": "integer" }
      },
      "required": ["name"]
    }
  },
  "required": ["user"]
}`

func TestValidateAndAttemptHealing_AlreadyValid_Passthrough(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	body := []byte(`{"user":{"name":"a","age":1}}`)

	got, err := validateAndAttemptHealing(t.Context(), logger, body, nestedObjectSchema)
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(got))
}

func TestValidateAndAttemptHealing_HealsStringifiedObject(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	// The "user" field should be an object but is provided as a JSON-encoded string.
	body := []byte(`{"user":"{\"name\":\"a\",\"age\":1}"}`)

	got, err := validateAndAttemptHealing(t.Context(), logger, body, nestedObjectSchema)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(got, &parsed))
	user, ok := parsed["user"].(map[string]any)
	require.True(t, ok, "user should be healed into an object")
	require.Equal(t, "a", user["name"])
}

func TestValidateAndAttemptHealing_UnhealableReturnsOriginalAndError(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	body := []byte(`{"user":{"age":"not-a-number"}}`)

	got, err := validateAndAttemptHealing(t.Context(), logger, body, nestedObjectSchema)
	require.Error(t, err)
	require.Equal(t, body, []byte(got))
}

func TestValidateAndAttemptHealing_EmptyBody(t *testing.T) {
	t.Parallel()

	// Empty body short-circuits validation (json.Unmarshal of "" fails, which
	// validateToolCallBody treats as "skip"), so healing isn't triggered and
	// the empty body is returned unchanged.
	logger := testenv.NewLogger(t)

	got, err := validateAndAttemptHealing(t.Context(), logger, []byte{}, nestedObjectSchema)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestHealStringifiedJSON_NoChange(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	body := json.RawMessage(`{"user":{"name":"a"}}`)

	_, healed := healStringifiedJSON(t.Context(), logger, body, nestedObjectSchema)
	require.False(t, healed)
}

func TestHealStringifiedJSON_HealsArrayItems(t *testing.T) {
	t.Parallel()

	schema := `{
		"type": "object",
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": { "name": { "type": "string" } }
				}
			}
		}
	}`

	logger := testenv.NewLogger(t)
	body := json.RawMessage(`{"users":["{\"name\":\"a\"}","{\"name\":\"b\"}"]}`)

	out, healed := healStringifiedJSON(t.Context(), logger, body, schema)
	require.True(t, healed)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(out, &parsed))
	users, ok := parsed["users"].([]any)
	require.True(t, ok)
	require.Len(t, users, 2)
	require.Equal(t, "a", users[0].(map[string]any)["name"])
	require.Equal(t, "b", users[1].(map[string]any)["name"])
}

func TestHealStringifiedJSON_PreservesUnknownProperties(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	body := json.RawMessage(`{"unknown":"value","user":"{\"name\":\"a\"}"}`)

	out, healed := healStringifiedJSON(t.Context(), logger, body, nestedObjectSchema)
	require.True(t, healed)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(out, &parsed))
	require.Equal(t, "value", parsed["unknown"])
}

func TestHealStringifiedJSON_InvalidSchemaReturnsUnchanged(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	body := json.RawMessage(`{"user":"{\"name\":\"a\"}"}`)

	out, healed := healStringifiedJSON(t.Context(), logger, body, "not-valid-json")
	require.False(t, healed)
	require.Equal(t, body, out)
}

func TestHealStringifiedJSON_InvalidBodyReturnsUnchanged(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	body := json.RawMessage(`{not json`)

	out, healed := healStringifiedJSON(t.Context(), logger, body, nestedObjectSchema)
	require.False(t, healed)
	require.Equal(t, body, out)
}

func TestHealValue_PrimitiveStringNotJSON(t *testing.T) {
	t.Parallel()

	// A non-JSON string value is returned unchanged.
	got := healValue("plain text", map[string]any{"type": "string"})
	require.Equal(t, "plain text", got)
}

func TestHealValue_OneOfBranchProperties(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"oneOf": []any{
			map[string]any{
				"properties": map[string]any{
					"foo": map[string]any{"type": "object"},
				},
			},
		},
	}
	got := healValue(map[string]any{"foo": `{"x":1}`}, schema)
	asMap, ok := got.(map[string]any)
	require.True(t, ok)
	foo, ok := asMap["foo"].(map[string]any)
	require.True(t, ok, "stringified foo should have been healed via oneOf")
	require.EqualValues(t, 1, foo["x"])
}

func TestHealValue_AnyOfBranchProperties(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string"}, // skipped — no properties
			map[string]any{
				"properties": map[string]any{
					"foo": map[string]any{"type": "object"},
				},
			},
		},
	}
	got := healValue(map[string]any{"foo": `{"x":2}`}, schema)
	asMap, ok := got.(map[string]any)
	require.True(t, ok)
	foo, ok := asMap["foo"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 2, foo["x"])
}
