package openapi

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInlineDefs_FanOutDAGFallsBackToRawSchema(t *testing.T) {
	t.Parallel()

	// Each level references the next twice, so full expansion would produce
	// 2^depth nodes.
	const depth = 40
	defs := make(map[string]json.RawMessage, depth+1)
	for i := range depth {
		defs[fmt.Sprintf("L%d", i)] = json.RawMessage(fmt.Sprintf(
			`{"type":"object","properties":{"a":{"$ref":"#/$defs/L%d"},"b":{"$ref":"#/$defs/L%d"}}}`,
			i+1, i+1,
		))
	}
	defs[fmt.Sprintf("L%d", depth)] = json.RawMessage(`{"type":"string"}`)

	raw := json.RawMessage(`{"$ref":"#/$defs/L0"}`)

	require.JSONEq(t, string(raw), string(inlineDefs(raw, defs)))
}

func TestInlineDefs_LeavesRefShapedLiteralsAlone(t *testing.T) {
	t.Parallel()

	defs := map[string]json.RawMessage{
		"Owner": json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`),
	}
	raw := json.RawMessage(`{
		"type": "object",
		"default": {"$ref": "#/$defs/Owner"},
		"examples": [{"$ref": "#/$defs/Owner"}],
		"properties": {"owner": {"$ref": "#/$defs/Owner"}}
	}`)

	want := `{
		"type": "object",
		"default": {"$ref": "#/$defs/Owner"},
		"examples": [{"$ref": "#/$defs/Owner"}],
		"properties": {"owner": {"type":"object","properties":{"name":{"type":"string"}}}}
	}`

	require.JSONEq(t, want, string(inlineDefs(raw, defs)))
}
