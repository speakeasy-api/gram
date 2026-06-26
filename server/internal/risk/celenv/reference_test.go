package celenv

// In-package so the smoke test can drive the engine through the public API while
// asserting the editor reference does not advertise a matcher the engine lacks.
// There is no machine-level parity to assert anymore: the env declares itself in
// New(), and the reference is human doc only.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReference_RoundTrip guards the wire shape the editor consumes (the wasm
// build marshals Describe() to JSON): it survives a JSON round-trip without loss.
func TestReference_RoundTrip(t *testing.T) {
	t.Parallel()
	ref := Describe()
	raw, err := json.Marshal(ref)
	require.NoError(t, err)
	var back Reference
	require.NoError(t, json.Unmarshal(raw, &back))
	require.Equal(t, ref, back)
}

// TestReference_MatchersCompile is a light smoke check: every matcher the editor
// advertises actually exists on the engine with the documented field-receiver,
// string-arg shape. A renamed or dropped matcher whose doc lingers is caught
// here rather than only when an author hits a confusing compile error.
func TestReference_MatchersCompile(t *testing.T) {
	t.Parallel()
	eng, err := New()
	require.NoError(t, err)

	for _, f := range Describe().Matchers {
		var call string
		switch {
		case f.Name == "present":
			call = "content.present()"
		case f.ReturnsField: // get() yields a field; chain to a bool
			call = fmt.Sprintf("content.%s(%q).present()", f.Name, "x")
		default:
			call = fmt.Sprintf("content.%s(%q)", f.Name, "x")
		}
		_, err := eng.Compile(call)
		require.NoErrorf(t, err, "matcher %q (%s) does not compile against the engine", f.Name, f.Signature)
	}
}

// TestReference_ToolFieldsCompile asserts each advertised tool field is reachable
// inside an exists() and a fabricated one is not — the editor offers these for
// completion, so they must match the engine's celTool declaration.
func TestReference_ToolFieldsCompile(t *testing.T) {
	t.Parallel()
	eng, err := New()
	require.NoError(t, err)

	var toolCalls *VarRef
	for i := range Describe().Variables {
		if v := Describe().Variables[i]; v.Name == "tool_calls" {
			toolCalls = &v
		}
	}
	require.NotNil(t, toolCalls)
	require.NotEmpty(t, toolCalls.Fields)

	for _, fld := range toolCalls.Fields {
		_, err := eng.Compile(fmt.Sprintf("tool_calls.exists(t, t.%s.present())", fld.Name))
		require.NoErrorf(t, err, "tool field %q not reachable on the engine", fld.Name)
	}
	_, err = eng.Compile(`tool_calls.exists(t, t.definitelyNotAField.present())`)
	require.Error(t, err, "fabricated tool field should be rejected")
}

// TestReference_VariablesCompile asserts each advertised variable exists with a
// shape consistent with its Matchable flag.
func TestReference_VariablesCompile(t *testing.T) {
	t.Parallel()
	eng, err := New()
	require.NoError(t, err)

	for _, v := range Describe().Variables {
		var probe string
		switch {
		case v.Matchable:
			probe = v.Name + ".present()"
		case v.Type == "string":
			probe = v.Name + ` == ""`
		case strings.HasPrefix(v.Type, "list("):
			probe = v.Name + ".exists(t, t.name.present())"
		default:
			t.Fatalf("variable %q has no probe for type %q", v.Name, v.Type)
		}
		_, err := eng.Compile(probe)
		require.NoErrorf(t, err, "variable %q (%s) does not compile", v.Name, v.Type)
	}
}
