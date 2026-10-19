package openapi

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// fanOutDefs builds shared schemas where each level references the next
// twice, so full expansion would produce 2^depth nodes.
func fanOutDefs(depth int) map[string]json.RawMessage {
	defs := make(map[string]json.RawMessage, depth+1)
	for i := range depth {
		defs[fmt.Sprintf("L%d", i)] = json.RawMessage(fmt.Sprintf(
			`{"type":"object","properties":{"a":{"$ref":"#/$defs/L%d"},"b":{"$ref":"#/$defs/L%d"}}}`,
			i+1, i+1,
		))
	}
	defs[fmt.Sprintf("L%d", depth)] = json.RawMessage(`{"type":"string"}`)
	return defs
}

func TestInlineDefs_FanOutDAGReportsBudgetExceeded(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"$ref":"#/$defs/L0"}`)

	out, ok := inlineDefs(raw, fanOutDefs(40))
	require.False(t, ok)
	require.JSONEq(t, string(raw), string(out))
}

func TestCompareToolInputs_OneSideOverBudgetGivesSingleWarning(t *testing.T) {
	t.Parallel()

	defs, err := json.Marshal(fanOutDefs(40))
	require.NoError(t, err)

	before := []byte(`{"type":"object","properties":{"body":{"$ref":"#/$defs/L0"}},"$defs":` + string(defs) + `}`)
	after := []byte(`{"type":"object","properties":{"body":{"type":"object","properties":{"name":{"type":"string"}}}}}`)

	warnings, info := compareToolInputs(before, after)
	require.Equal(t, []string{"shared input schema too large to compare in detail"}, warnings)
	require.Empty(t, info)
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

	out, ok := inlineDefs(raw, defs)
	require.True(t, ok)
	require.JSONEq(t, want, string(out))
}
