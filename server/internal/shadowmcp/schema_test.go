package shadowmcp_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestInjectToolsetIDConstant_AddsRequiredConstProperty(t *testing.T) {
	t.Parallel()

	schema := json.RawMessage(`{"type":"object","properties":{"foo":{"type":"string"}}}`)
	out, err := shadowmcp.InjectToolsetIDConstant(schema, "scope-abc")
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))

	props, ok := got["properties"].(map[string]any)
	require.True(t, ok, "properties must be a JSON object")
	injected, ok := props[shadowmcp.XGramToolsetIDField].(map[string]any)
	require.True(t, ok, "injected entry must be a JSON object")
	require.Equal(t, "string", injected["type"])
	require.Equal(t, "scope-abc", injected["const"])

	required, ok := got["required"].([]any)
	require.True(t, ok, "required must be a JSON array")
	require.Contains(t, required, shadowmcp.XGramToolsetIDField)
	// Existing properties survive.
	require.Contains(t, props, "foo")
}

func TestInjectToolsetIDConstant_DefaultsTypeWhenAbsent(t *testing.T) {
	t.Parallel()

	// An empty schema input (no type, no properties) still produces a
	// well-formed object schema with the injected property.
	out, err := shadowmcp.InjectToolsetIDConstant(json.RawMessage("{}"), "scope-1")
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	require.Equal(t, "object", got["type"], "type must default to object when absent")
	require.NotEmpty(t, got["properties"])
	required, ok := got["required"].([]any)
	require.True(t, ok, "required must be a JSON array")
	require.Contains(t, required, shadowmcp.XGramToolsetIDField)
}

func TestInjectToolsetIDConstant_DoesNotDuplicateRequiredEntry(t *testing.T) {
	t.Parallel()

	// Re-injecting into an already-injected schema must not duplicate the
	// required entry — idempotency matters because the helper may run on
	// schemas that already carry the property from a prior pass.
	schema := json.RawMessage(`{"type":"object","required":["x-gram-toolset-id"]}`)
	out, err := shadowmcp.InjectToolsetIDConstant(schema, "scope-1")
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	required, ok := got["required"].([]any)
	require.True(t, ok, "required must be a JSON array")
	count := 0
	for _, r := range required {
		if s, ok := r.(string); ok && s == shadowmcp.XGramToolsetIDField {
			count++
		}
	}
	require.Equal(t, 1, count, "required entry must be present exactly once even on re-injection")
}

func TestInjectToolsetIDConstant_ReturnsInputOnParseFailure(t *testing.T) {
	t.Parallel()

	// Garbage input bytes must be returned unchanged with an error so
	// callers can choose between propagating the failure or relaying the
	// original schema verbatim.
	original := json.RawMessage("{not json")
	out, err := shadowmcp.InjectToolsetIDConstant(original, "scope-1")
	require.Error(t, err)
	require.Equal(t, string(original), string(out))
}

func TestStripToolsetIDProperty_RemovesProperty(t *testing.T) {
	t.Parallel()

	args := json.RawMessage(`{"x-gram-toolset-id":"scope-1","location":"sf"}`)
	out, err := shadowmcp.StripToolsetIDProperty(args)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	require.NotContains(t, got, shadowmcp.XGramToolsetIDField)
	require.Equal(t, "sf", got["location"])
}

func TestStripToolsetIDProperty_NoOpWhenPropertyAbsent(t *testing.T) {
	t.Parallel()

	// When the property isn't there, the helper short-circuits and
	// returns the original byte slice — important because the tool's
	// declared schema may rely on specific key ordering or formatting
	// that re-marshaling would normalize away.
	args := json.RawMessage(`{"location":"sf"}`)
	out, err := shadowmcp.StripToolsetIDProperty(args)
	require.NoError(t, err)
	require.Equal(t, string(args), string(out))
}

func TestStripToolsetIDProperty_NoOpForNonObjectInput(t *testing.T) {
	t.Parallel()

	// Non-object payloads (arrays, scalars, empty) pass through unchanged.
	// The helper is shape-tolerant so misbehaving upstreams don't trip a
	// hard error inside the strip path.
	for _, args := range []json.RawMessage{
		json.RawMessage(``),
		json.RawMessage(`null`),
		json.RawMessage(`[]`),
		json.RawMessage(`"plain string"`),
	} {
		out, err := shadowmcp.StripToolsetIDProperty(args)
		require.NoError(t, err, "input %q must pass through without error", string(args))
		require.Equal(t, string(args), string(out))
	}
}

func TestStripToolsetIDProperty_ErrorOnMalformedObject(t *testing.T) {
	t.Parallel()

	// Bytes that look like an object (leading "{") but don't parse as
	// JSON surface an error so misuse is loud rather than silently
	// passed downstream.
	args := json.RawMessage(`{not json`)
	_, err := shadowmcp.StripToolsetIDProperty(args)
	require.Error(t, err)
}
