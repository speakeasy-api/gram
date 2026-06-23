package celenv

// These tests live in-package (not celenv_test) so they can reach the unexported
// descriptor internals (bindings, celType, Descriptor) directly. They assert the
// descriptor is internally consistent AND that it faithfully describes the real
// cel-go env — the two halves that let the FE type-checker be configured from
// the served descriptor without drifting from what the backend compiles.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDescriptor_BindingsBijection asserts every declared overload has exactly
// one Go implementation and vice-versa: no declared-but-unbound overload, no
// orphan binding. buildEnv hard-errors on a missing binding, but this catches
// orphans (a binding with no declaration) which buildEnv would silently ignore.
func TestDescriptor_BindingsBijection(t *testing.T) {
	t.Parallel()
	desc := Descriptor()

	declared := make(map[string]bool, len(desc.Functions))
	for _, f := range desc.Functions {
		require.NotEmpty(t, f.OverloadID, "function %q has no overload id", f.Name)
		require.False(t, declared[f.OverloadID], "duplicate overload id %q", f.OverloadID)
		declared[f.OverloadID] = true
		_, ok := bindings[f.OverloadID]
		require.True(t, ok, "declared overload %q has no binding", f.OverloadID)
	}
	for id := range bindings {
		require.True(t, declared[id], "orphan binding %q has no declaration", id)
	}
}

// TestDescriptor_TypesResolve asserts every machine type-string used anywhere in
// the descriptor resolves via celType, and every referenced custom type is
// declared in Types. This is the static half of the cel-go/cel-js type-string
// contract.
func TestDescriptor_TypesResolve(t *testing.T) {
	t.Parallel()
	desc := Descriptor()

	declared := make(map[string]bool, len(desc.Types))
	for _, ty := range desc.Types {
		declared[ty.Name] = true
	}

	check := func(machine, where string) {
		_, err := celType(machine)
		require.NoError(t, err, "type %q (%s) does not resolve", machine, where)
		// A bare custom-type reference must be declared.
		base := machine
		if inner, ok := strings.CutPrefix(base, "list<"); ok {
			base, _ = strings.CutSuffix(inner, ">")
		}
		switch base {
		case "bool", "string", "int", "double", "bytes", "field":
		default:
			require.True(t, declared[base], "type %q (%s) references undeclared type %q", machine, where, base)
		}
	}

	for _, ty := range desc.Types {
		for _, f := range ty.Fields {
			check(f.Type, fmt.Sprintf("field %s.%s", ty.Name, f.Name))
		}
	}
	for _, v := range desc.Variables {
		check(v.Type, "variable "+v.Name)
	}
	for _, f := range desc.Functions {
		check(f.ReturnType, "return of "+f.Name)
		if f.Member {
			check(f.ReceiverType, "receiver of "+f.Name)
		}
		for _, p := range f.Params {
			check(p.Type, fmt.Sprintf("param %s.%s", f.Name, p.Name))
		}
	}
}

// TestDescriptor_EnvParity is the strong assertion: the descriptor faithfully
// describes the real cel-go env. cel-go exposes no stable decl-enumeration API,
// so we probe-compile a minimal expression per declaration that only type-checks
// if the env declares it with the expected type.
func TestDescriptor_EnvParity(t *testing.T) {
	t.Parallel()
	eng, err := New()
	require.NoError(t, err)
	desc := Descriptor()

	mustCompile := func(expr string) {
		t.Helper()
		_, err := eng.Compile(expr)
		require.NoError(t, err, "expected %q to compile against the env", expr)
	}
	mustReject := func(expr string) {
		t.Helper()
		_, err := eng.Compile(expr)
		require.Error(t, err, "expected %q to be rejected by the env", expr)
	}

	// Variables: a probe that forces each variable's declared type.
	for _, v := range desc.Variables {
		switch v.Type {
		case "string":
			mustCompile(v.Name + ` == ""`)
		case "field":
			mustCompile(v.Name + `.present()`) // proves opaque field receiver
		default:
			if strings.HasPrefix(v.Type, "list<") {
				// list<celenv.celTool>: iterate and touch a declared member field.
				mustCompile(v.Name + `.exists(t, t.name.present())`)
			} else {
				t.Fatalf("variable %q has unhandled probe type %q", v.Name, v.Type)
			}
		}
	}

	// Member overloads: exercise each with correct arg types off `content`
	// (a field). bool-returning ones stand alone; get returns a field so it must
	// chain through .present() to reach bool.
	for _, f := range desc.Functions {
		require.True(t, f.Member, "probe only models member overloads; %q is not", f.Name)
		args := make([]string, 0, len(f.Params))
		for range f.Params {
			args = append(args, `"x"`) // every param today is a string
		}
		call := fmt.Sprintf("content.%s(%s)", f.Name, strings.Join(args, ", "))
		switch f.ReturnType {
		case "bool":
			mustCompile(call)
		case "field":
			mustCompile(call + ".present()")
		default:
			t.Fatalf("overload %q has unhandled return type %q", f.Name, f.ReturnType)
		}
	}

	// celTool fields: each declared field is reachable; a fabricated one is not.
	for _, ty := range desc.Types {
		if ty.Opaque {
			continue
		}
		for _, fld := range ty.Fields {
			mustCompile(fmt.Sprintf("tool_calls.exists(t, t.%s.present())", fld.Name))
		}
		mustReject(`tool_calls.exists(t, t.definitelyNotAField.present())`)
	}
}

// TestDescriptor_RoundTrip guards the wire contract the FE depends on: the
// descriptor marshals to JSON and back without loss.
func TestDescriptor_RoundTrip(t *testing.T) {
	t.Parallel()
	desc := Descriptor()
	raw, err := json.Marshal(desc)
	require.NoError(t, err)
	var back EnvDescriptor
	require.NoError(t, json.Unmarshal(raw, &back))
	require.Equal(t, desc, back)
}
