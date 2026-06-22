package celenv

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/functions"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Descriptor is the single declarative description of the CEL environment: the
// types, variables, function overloads, and macros an author may use. It is the
// source of truth that both the Go engine (buildEnv consumes it for all
// declarations) and the dashboard's CEL type-checker (served over the wire and
// fed to @marcbachmann/cel-js) are built from, so the editor cannot drift from
// what the backend actually compiles.
//
// Behaviour is NOT described here: the Go matcher implementations stay in the
// bindings map (keyed by OverloadID) and the opaque field type / native celTool
// struct stay as Go constructs in celenv.go. The descriptor declares their
// shape; the parity test (descriptor_test.go) asserts the two agree.
type EnvDescriptor struct {
	Types     []TypeDecl  `json:"types"`
	Variables []VarDecl   `json:"variables"`
	Functions []FuncDecl  `json:"functions"`
	Macros    []MacroDecl `json:"macros"`
}

// TypeDecl declares a CEL type. Name is the engine type name used in machine
// type-strings and registered identically in cel-go (cel.ObjectType) and cel-js
// (registerType) — confirmed cel-js accepts the dotted form. DisplayName is the
// short human label the editor shows.
type TypeDecl struct {
	Name        string      `json:"name"`        // engine name, e.g. "field", "celenv.celTool"
	Opaque      bool        `json:"opaque"`      // no readable members; only receiver methods
	Fields      []FieldDecl `json:"fields"`      // typed members (empty when Opaque)
	DisplayName string      `json:"displayName"` // human label, e.g. "field", "tool"
	Description string      `json:"description"`
}

// FieldDecl is one typed member of a non-opaque object type.
type FieldDecl struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // machine type-string
	Description string `json:"description"`
}

// VarDecl is one author-visible variable. Type is the machine type-string the
// checkers consume; DisplayType is the human tag shown in the editor.
type VarDecl struct {
	Name        string `json:"name"`
	Type        string `json:"type"`        // machine, e.g. "field", "string", "list<celenv.celTool>"
	DisplayType string `json:"displayType"` // human, e.g. "field", "list(tool)"
	Description string `json:"description"`
}

// FuncDecl is one function overload. OverloadID is the stable key into the
// bindings map AND a serialized contract identifier; treat it as immutable.
type FuncDecl struct {
	Name         string      `json:"name"`
	OverloadID   string      `json:"overloadId"`
	Member       bool        `json:"member"`       // receiver-style (x.fn(...)) vs global
	ReceiverType string      `json:"receiverType"` // machine type-string when Member
	Params       []ParamDecl `json:"params"`       // non-receiver args
	ReturnType   string      `json:"returnType"`   // machine type-string
	Signature    string      `json:"signature"`    // human, e.g. "field.match(pattern: string) -> bool"
	Description  string      `json:"description"`
}

// ParamDecl is one non-receiver argument of a function overload.
type ParamDecl struct {
	Name string `json:"name"`
	Type string `json:"type"` // machine type-string
}

// MacroDecl is one CEL macro. Macros are built into both engines (not declared),
// so they are listed here for documentation and completion only — there is no
// parity assertion possible for them. ReturnsBool marks the predicate macros
// (has/all/exists/exists_one) that can stand as a whole rule, versus the
// list-producing ones (map/filter).
type MacroDecl struct {
	Name        string `json:"name"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	ReturnsBool bool   `json:"returnsBool"`
}

// Descriptor returns the environment description. This is the one place the env
// shape is authored; buildEnv and Describe both read it.
func Descriptor() EnvDescriptor {
	return EnvDescriptor{
		Types: []TypeDecl{
			{
				Name: "field", Opaque: true, DisplayName: "field",
				Fields:      nil,
				Description: "A matchable message field. Call the matcher methods on it (match/includes/eq/...); drill into JSON with get(path).",
			},
			{
				Name: toolTypeName, Opaque: false, DisplayName: "tool",
				Description: "A single tool call on a tool_request.",
				Fields: []FieldDecl{
					{Name: "name", Type: "field", Description: "The tool call's name (e.g. shell, query)."},
					{Name: "server", Type: "field", Description: "The MCP server the tool belongs to."},
					{Name: "function", Type: "field", Description: "The function/operation the tool invokes."},
					{Name: "args", Type: "field", Description: "The tool call's arguments; drill in with .get(path)."},
				},
			},
		},
		Variables: []VarDecl{
			{Name: "kind", Type: "string", DisplayType: "string", Description: "Message type: user_message, assistant_message, tool_request, or tool_response. Usually unnecessary — body fields are auto-scoped."},
			{Name: "content", Type: "field", DisplayType: "field", Description: "The message's raw text body, any message type."},
			{Name: "prompt", Type: "field", DisplayType: "field", Description: "The body of a user_message (empty otherwise)."},
			{Name: "assistant", Type: "field", DisplayType: "field", Description: "The body of an assistant_message (empty otherwise)."},
			{Name: "tool_result", Type: "field", DisplayType: "field", Description: "The output of a tool_response message (empty otherwise). Singular: one response message carries one tool's output."},
			{Name: "tool_calls", Type: "list<" + toolTypeName + ">", DisplayType: "list(tool)", Description: "The tool calls on a tool_request message. Plural: one request can fan out parallel calls. Iterate with tool_calls.exists(t, ...); each tool has .name, .server, .function, .args (correlated to the same call)."},
		},
		// The match* family: span-recording matchers, one per strategy. The
		// shared match prefix marks them as detectors (a true verdict records a
		// highlighted span) and keeps them clear of the stdlib string functions
		// (matches/contains/startsWith/...), which cel-go can't re-overload onto a
		// custom type anyway. get (navigation) and present (presence) sit outside
		// the family.
		Functions: []FuncDecl{
			matchFn("matchRegex", "field_matchregex_string", "pattern", "RE2 regex match anywhere in the value; records a span per occurrence. Case-insensitive via the (?i) flag, e.g. matchRegex(\"(?i)secret\")."),
			matchFn("matchText", "field_matchtext_string", "substring", "Case-insensitive literal substring; records a span per occurrence."),
			matchFn("matchExact", "field_matchexact_string", "value", "Whole-value equality; records the whole-value span on match."),
			matchFn("matchPrefix", "field_matchprefix_string", "prefix", "Prefix match; records the prefix span."),
			matchFn("matchSuffix", "field_matchsuffix_string", "suffix", "Suffix match; records the suffix span."),
			matchFn("matchGlob", "field_matchglob_string", "pattern", "Glob match over the whole value (e.g. *_exec, shell:*); records the whole-value span."),
			{
				Name: "get", OverloadID: "field_get_string", Member: true, ReceiverType: "field",
				Params:      []ParamDecl{{Name: "path", Type: "string"}},
				ReturnType:  "field",
				Signature:   "field.get(path: string) -> field",
				Description: "Drill into the field's JSON at a gjson path (command, payload.sql, rows.0.ssn); returns a sub-field the matchers compose over.",
			},
			{
				Name: "present", OverloadID: "field_present", Member: true, ReceiverType: "field",
				Params:      nil,
				ReturnType:  "bool",
				Signature:   "field.present() -> bool",
				Description: "True when the field has a non-empty value.",
			},
		},
		Macros: []MacroDecl{
			{Name: "exists", Signature: "list.exists(x, predicate) -> bool", Description: "True when the predicate holds for at least one element. The usual way to match tool calls: tool_calls.exists(t, t.function.matchExact(\"bash\")).", ReturnsBool: true},
			{Name: "all", Signature: "list.all(x, predicate) -> bool", Description: "True when the predicate holds for every element (vacuously true when the list is empty).", ReturnsBool: true},
			{Name: "exists_one", Signature: "list.exists_one(x, predicate) -> bool", Description: "True when the predicate holds for exactly one element.", ReturnsBool: true},
			{Name: "has", Signature: "has(field) -> bool", Description: "True when a field/path is present and set, e.g. has(t.args.command). Use field.present() to also require non-empty.", ReturnsBool: true},
			{Name: "map", Signature: "list.map(x, expr) -> list", Description: "Transforms each element to expr, yielding a new list; the 3-arg form list.map(x, predicate, expr) maps only elements matching predicate. Returns a list, not a verdict — feed it to another macro (e.g. tool_calls.map(t, t.name).exists(...)).", ReturnsBool: false},
			{Name: "filter", Signature: "list.filter(x, predicate) -> list", Description: "Keeps the elements where the predicate holds, yielding a sublist. Returns a list, not a verdict — combine with another macro to reach a bool.", ReturnsBool: false},
		},
	}
}

// matchFn builds the FuncDecl for one of the string matchers, which all share
// the field.fn(string) -> bool shape; param names the single argument.
func matchFn(name, overloadID, param, description string) FuncDecl {
	return FuncDecl{
		Name: name, OverloadID: overloadID, Member: true, ReceiverType: "field",
		Params:      []ParamDecl{{Name: param, Type: "string"}},
		ReturnType:  "bool",
		Signature:   fmt.Sprintf("field.%s(%s: string) -> bool", name, param),
		Description: description,
	}
}

// celType resolves a machine type-string to a cel-go type. It is the single
// bridge from the descriptor's string grammar to *cel.Type and must agree with
// the runtime registrations (the opaque fieldType and the native celTool object
// type) — the probe-compile parity test enforces that agreement.
func celType(s string) (*cel.Type, error) {
	switch s {
	case "bool":
		return cel.BoolType, nil
	case "string":
		return cel.StringType, nil
	case "int":
		return cel.IntType, nil
	case "double":
		return cel.DoubleType, nil
	case "bytes":
		return cel.BytesType, nil
	case "field":
		return fieldType, nil
	}
	if inner, ok := strings.CutPrefix(s, "list<"); ok {
		elem, ok2 := strings.CutSuffix(inner, ">")
		if !ok2 {
			return nil, fmt.Errorf("malformed list type %q", s)
		}
		et, err := celType(elem)
		if err != nil {
			return nil, err
		}
		return cel.ListType(et), nil
	}
	// A declared, non-opaque object type registered under its Name (e.g.
	// celenv.celTool via ext.NativeTypes).
	for _, t := range Descriptor().Types {
		if t.Name == s && !t.Opaque {
			return cel.ObjectType(t.Name), nil
		}
	}
	return nil, fmt.Errorf("unknown cel type %q", s)
}

// funcBinding holds the Go implementation of one overload. Exactly one of binary
// (receiver + 1 arg, or any 2-ary op) or unary (receiver-only) is set; the
// binary/unary constructors keep the other nil.
type funcBinding struct {
	binary functions.BinaryOp
	unary  functions.UnaryOp
}

func binary(op functions.BinaryOp) funcBinding { return funcBinding{binary: op, unary: nil} }
func unary(op functions.UnaryOp) funcBinding   { return funcBinding{binary: nil, unary: op} }

// stringMatcher adapts a (*fieldVal).method(string) bool to the CEL binary op
// shape, guarding the receiver/argument types. Shared by the six matchers.
func stringMatcher(name string, fn func(*fieldVal, string) bool) functions.BinaryOp {
	return func(lhs, rhs ref.Val) ref.Val {
		f, ok := lhs.(*fieldVal)
		if !ok {
			return types.NewErr("%s: receiver is not a field", name)
		}
		s, ok := rhs.(types.String)
		if !ok {
			return types.NewErr("%s: argument must be string", name)
		}
		return types.Bool(fn(f, string(s)))
	}
}

// bindings maps each overload ID to its Go implementation. buildEnv hard-errors
// on a declared overload with no binding here (and the parity test asserts the
// map and the descriptor are a bijection), so a declaration can never ship
// without a behaviour.
var bindings = map[string]funcBinding{
	"field_matchregex_string":  binary(stringMatcher("matchRegex", (*fieldVal).matchRegex)),
	"field_matchtext_string":   binary(stringMatcher("matchText", (*fieldVal).matchText)),
	"field_matchexact_string":  binary(stringMatcher("matchExact", (*fieldVal).matchExact)),
	"field_matchprefix_string": binary(stringMatcher("matchPrefix", (*fieldVal).matchPrefix)),
	"field_matchsuffix_string": binary(stringMatcher("matchSuffix", (*fieldVal).matchSuffix)),
	"field_matchglob_string":   binary(stringMatcher("matchGlob", (*fieldVal).matchGlob)),
	"field_get_string": binary(func(lhs, rhs ref.Val) ref.Val {
		f, ok := lhs.(*fieldVal)
		if !ok {
			return types.NewErr("get: receiver is not a field")
		}
		p, ok := rhs.(types.String)
		if !ok {
			return types.NewErr("get: path must be string")
		}
		return f.get(string(p))
	}),
	"field_present": unary(func(v ref.Val) ref.Val {
		f, ok := v.(*fieldVal)
		if !ok {
			return types.NewErr("present: receiver is not a field")
		}
		return types.Bool(f.present())
	}),
}
