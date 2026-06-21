// Package celenv defines the single CEL environment that is the source of truth
// for risk rule expressions. Every expression is an ordinary boolean predicate;
// the two evaluation modes differ only in what they read off the result:
//
//   - Scope predicates: decide whether a message is in scope for a policy.
//     EvalScope returns the boolean verdict.
//     e.g. `tools.exists(t, t.server.matchExact("shell"))`
//
//   - Detection matchers: the same boolean grammar, but EvalDetection also
//     returns the SPANS that the matcher methods recorded — the substrings that
//     matched, attributed to the field (tool call and JSON path) they matched
//     in — so the dashboard can highlight them.
//     e.g. `tools.exists(t, t.function.matchRegex("bash") &&
//     t.args.get("command").matchRegex("DROP TABLE"))`
//
// Fields:
//
//	kind       string                   message type (escape hatch; rarely needed)
//	content    field                    the raw body, any message type
//	prompt     field                    the body of a user_message (else empty)
//	assistant  field                    the body of an assistant_message (else empty)
//	output     field                    the body of a tool_response (else empty)
//	tools      list of tool             the calls on a tool_request; each tool has
//	                                     .name .server .function .args fields
//
// Body fields are auto-scoped: `prompt.matchText(x)` only matches user messages
// because prompt is empty otherwise, so an explicit `kind ==` check is usually
// unnecessary. tool_request calls are correlated: inside `tools.exists(t, ...)`,
// `t.function` and `t.args` are bound to the SAME tool.
//
// JSON drill-down: any field's `.get(path)` returns a sub-field scoped to the
// gjson value at path (`command`, `payload.sql`, `rows.0.ssn`), so every matcher
// composes over it — `t.args.get("command").matchRegex(x)`, `output.get("error")`.
//
// Authoring stays natural: conditions combine with `&&`/`||`, exactly what a
// guided query builder emits. Spans are captured as a side effect of the matcher
// methods, not by contorting the expression into a span-returning shape: the
// field values are custom values built fresh per evaluation, each holding a
// pointer to that evaluation's span collector, so a matcher records simply by
// being a method on the field. Evaluation is therefore thread-safe with no
// shared mutable state.
//
// celenv is self-contained (its own Message/Tool/Span types) so the analysis
// engine can import it without an import cycle; callers adapt their own message
// view into a celenv.Message at the boundary.
package celenv

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/gobwas/glob"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
	"github.com/tidwall/gjson"

	"github.com/speakeasy-api/gram/server/internal/message"
)

// Tool is one tool call exposed to expressions as an element of `tools`.
type Tool struct {
	Name     string
	Server   string
	Function string
	Args     string
}

// Message is the structured input an expression evaluates against.
type Message struct {
	Content string
	Type    string
	Tools   []Tool
}

// Span is one matched substring, attributed to the field (and optionally the
// tool call and JSON path) it matched in. Detection evaluation returns a slice.
type Span struct {
	Target     string
	ToolCallID string
	Path       string
	Start      int
	End        int
	Value      string
}

// fieldType is the opaque CEL type of the message field variables. Authors never
// construct one; they only call matcher methods on the bound fields.
var fieldType = types.NewOpaqueType("celenv.field")

// celTool is the registered object type for an element of `tools`. Its fields
// are the matchable tool attributes; declaring them explicitly (rather than a
// map(string, field)) gives compile-time validation, so `t.functionn` is a
// compile error, not a silent runtime miss.
type celTool struct {
	Name     *fieldVal `cel:"name"`
	Server   *fieldVal `cel:"server"`
	Function *fieldVal `cel:"function"`
	Args     *fieldVal `cel:"args"`
}

// toolTypeName is celTool's CEL type name (pkg alias + struct name), used to
// declare the element type of the `tools` list.
const toolTypeName = "celenv.celTool"

// Engine holds the compiled-once CEL environment.
type Engine struct {
	env *cel.Env
}

// New builds the CEL environment with the risk fields and matchers.
func New() (*Engine, error) {
	env, err := buildEnv()
	if err != nil {
		return nil, err
	}
	return &Engine{env: env}, nil
}

func buildEnv() (*cel.Env, error) {
	desc := Descriptor()

	// Register celTool as an object type so `tools` elements have validated
	// fields (name/server/function/args), each of the opaque field type. This
	// reflection registration and the opaque fieldType are the two declarations
	// that can't be pure data; the descriptor mirrors their shape (the celTool
	// TypeDecl and the "field" TypeDecl) and the parity test asserts agreement.
	opts := []cel.EnvOption{
		ext.NativeTypes(reflect.TypeFor[celTool](), ext.ParseStructTags(true)),
	}

	for _, v := range desc.Variables {
		t, err := celType(v.Type)
		if err != nil {
			return nil, fmt.Errorf("variable %q: %w", v.Name, err)
		}
		opts = append(opts, cel.Variable(v.Name, t))
	}

	// Every declared overload must have a Go implementation in bindings; a
	// missing key is a hard error so a declaration can't ship without behaviour.
	// Method names avoid CEL's string built-ins (matches/contains/startsWith/
	// endsWith), which cannot be re-overloaded onto a custom type.
	for _, f := range desc.Functions {
		b, ok := bindings[f.OverloadID]
		if !ok {
			return nil, fmt.Errorf("function %q: no binding for overload %q", f.Name, f.OverloadID)
		}
		ret, err := celType(f.ReturnType)
		if err != nil {
			return nil, fmt.Errorf("function %q return type: %w", f.Name, err)
		}
		args := make([]*cel.Type, 0, 1+len(f.Params))
		if f.Member {
			rt, err := celType(f.ReceiverType)
			if err != nil {
				return nil, fmt.Errorf("function %q receiver type: %w", f.Name, err)
			}
			args = append(args, rt)
		}
		for _, p := range f.Params {
			pt, err := celType(p.Type)
			if err != nil {
				return nil, fmt.Errorf("function %q param %q: %w", f.Name, p.Name, err)
			}
			args = append(args, pt)
		}

		var binding cel.OverloadOpt
		switch {
		case b.binary != nil:
			binding = cel.BinaryBinding(b.binary)
		case b.unary != nil:
			binding = cel.UnaryBinding(b.unary)
		default:
			return nil, fmt.Errorf("function %q: empty binding for overload %q", f.Name, f.OverloadID)
		}

		// All current overloads are member (receiver) overloads.
		opts = append(opts, cel.Function(f.Name, cel.MemberOverload(f.OverloadID, args, ret, binding)))
	}

	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, fmt.Errorf("build cel env: %w", err)
	}
	return env, nil
}

// Compile type-checks an expression. Every expression — scope or detection — is
// a boolean predicate, so save-time validation is the same for both.
func (e *Engine) Compile(expr string) (cel.Program, error) {
	ast, iss := e.env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("compile %q: %w", expr, iss.Err())
	}
	if out := ast.OutputType(); !out.IsExactType(cel.BoolType) {
		return nil, fmt.Errorf("expression must evaluate to bool, got %s", out)
	}
	prg, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("program %q: %w", expr, err)
	}
	return prg, nil
}

// EvalScope evaluates the predicate and returns its boolean verdict. Spans are
// not collected.
func (e *Engine) EvalScope(prg cel.Program, msg Message) (bool, error) {
	out, _, err := prg.Eval(activation(msg, nil))
	if err != nil {
		return false, fmt.Errorf("eval scope: %w", err)
	}
	b, ok := out.(types.Bool)
	if !ok {
		return false, fmt.Errorf("scope evaluated to %s, want bool", out.Type())
	}
	return bool(b), nil
}

// EvalDetection evaluates the predicate and, when it matches, returns the spans
// recorded by the matcher methods. When the predicate is false the rule did not
// fire and no spans are returned.
func (e *Engine) EvalDetection(prg cel.Program, msg Message) ([]Span, bool, error) {
	coll := &collector{spans: nil}
	out, _, err := prg.Eval(activation(msg, coll))
	if err != nil {
		return nil, false, fmt.Errorf("eval detection: %w", err)
	}
	b, ok := out.(types.Bool)
	if !ok {
		return nil, false, fmt.Errorf("detection evaluated to %s, want bool", out.Type())
	}
	if !bool(b) {
		return nil, false, nil
	}
	return coll.spans, true, nil
}

// activation builds the field variables for one evaluation, wiring each to coll
// (nil for scope eval, which records no spans).
func activation(msg Message, coll *collector) map[string]any {
	body := func(name string, applies bool) *fieldVal {
		if !applies {
			return &fieldVal{name: name, values: nil, coll: coll}
		}
		return &fieldVal{name: name, values: []fieldValue{{toolCallID: "", path: "", text: msg.Content}}, coll: coll}
	}

	tools := make([]any, len(msg.Tools))
	for i, t := range msg.Tools {
		one := func(name, text string) *fieldVal {
			return &fieldVal{name: name, values: []fieldValue{{toolCallID: t.Name, path: "", text: text}}, coll: coll}
		}
		tools[i] = celTool{
			Name:     one("tool.name", t.Name),
			Server:   one("tool.server", t.Server),
			Function: one("tool.function", t.Function),
			Args:     one("tool.args", t.Args),
		}
	}

	return map[string]any{
		"kind":      msg.Type,
		"content":   body("content", true),
		"prompt":    body("prompt", msg.Type == message.User),
		"assistant": body("assistant", msg.Type == message.Assistant),
		"output":    body("output", msg.Type == message.ToolResponse),
		"tools":     tools,
	}
}

// collector accumulates the spans recorded during one evaluation.
type collector struct {
	spans []Span
}

// fieldValue is one underlying string a field matches against, with the tool
// call and JSON path it came from for span attribution.
type fieldValue struct {
	toolCallID string
	path       string
	text       string
}

// fieldVal is the custom CEL value bound to each field variable. It carries the
// per-eval collector so its matcher methods can record spans.
type fieldVal struct {
	name   string
	values []fieldValue
	coll   *collector
}

var _ ref.Val = (*fieldVal)(nil)

func (f *fieldVal) Type() ref.Type        { return fieldType }
func (f *fieldVal) Value() any            { return f }
func (f *fieldVal) Equal(ref.Val) ref.Val { return types.False }

func (f *fieldVal) ConvertToType(t ref.Type) ref.Val {
	if t.TypeName() == fieldType.TypeName() {
		return f
	}
	return types.NewErr("type conversion not supported for field")
}

func (f *fieldVal) ConvertToNative(reflect.Type) (any, error) {
	return nil, fmt.Errorf("field is not convertible to a native value")
}

// get drills into each value's JSON at path, returning a sub-field.
func (f *fieldVal) get(path string) *fieldVal {
	norm := normalizeJSONPath(path)
	out := &fieldVal{name: f.name, values: make([]fieldValue, 0, len(f.values)), coll: f.coll}
	for _, v := range f.values {
		res := gjson.Get(v.text, norm)
		if !res.Exists() {
			continue
		}
		out.values = append(out.values, fieldValue{
			toolCallID: v.toolCallID,
			path:       joinPath(v.path, norm),
			text:       res.String(),
		})
	}
	return out
}

// record appends a span for the [start,end) match within v.
func (f *fieldVal) record(v fieldValue, start, end int) {
	if f.coll == nil {
		return
	}
	f.coll.spans = append(f.coll.spans, Span{
		Target:     f.name,
		ToolCallID: v.toolCallID,
		Path:       v.path,
		Start:      start,
		End:        end,
		Value:      v.text[start:end],
	})
}

func (f *fieldVal) matchRegex(pattern string) bool {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	matched := false
	for _, v := range f.values {
		for _, m := range re.FindAllStringIndex(v.text, -1) {
			matched = true
			f.record(v, m[0], m[1])
		}
	}
	return matched
}

func (f *fieldVal) matchText(sub string) bool {
	if sub == "" {
		return false
	}
	// Case-insensitive substring via a quoted-literal regex, so match offsets are
	// computed against the original text. Lowercasing a copy and reusing its
	// offsets can be wrong, or panic, when a case-fold changes byte length.
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(sub))
	if err != nil {
		return false
	}
	matched := false
	for _, v := range f.values {
		for _, m := range re.FindAllStringIndex(v.text, -1) {
			matched = true
			f.record(v, m[0], m[1])
		}
	}
	return matched
}

func (f *fieldVal) matchExact(s string) bool {
	matched := false
	for _, v := range f.values {
		if v.text == s {
			matched = true
			f.record(v, 0, len(v.text))
		}
	}
	return matched
}

func (f *fieldVal) matchPrefix(p string) bool {
	matched := false
	for _, v := range f.values {
		if strings.HasPrefix(v.text, p) {
			matched = true
			f.record(v, 0, len(p))
		}
	}
	return matched
}

func (f *fieldVal) matchSuffix(s string) bool {
	matched := false
	for _, v := range f.values {
		if strings.HasSuffix(v.text, s) {
			matched = true
			f.record(v, len(v.text)-len(s), len(v.text))
		}
	}
	return matched
}

func (f *fieldVal) matchGlob(pattern string) bool {
	g, err := glob.Compile(pattern)
	if err != nil {
		return false
	}
	matched := false
	for _, v := range f.values {
		if g.Match(v.text) {
			matched = true
			f.record(v, 0, len(v.text))
		}
	}
	return matched
}

func (f *fieldVal) present() bool {
	matched := false
	for _, v := range f.values {
		if v.text != "" {
			matched = true
			f.record(v, 0, len(v.text))
		}
	}
	return matched
}

// normalizeJSONPath converts a leading $-rooted / bracket-indexed path into the
// dot-separated form gjson expects: `$.a[0].b` -> `a.0.b`.
func normalizeJSONPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "$")
	p = strings.ReplaceAll(p, "[", ".")
	p = strings.ReplaceAll(p, "]", "")
	p = strings.ReplaceAll(p, "\"", "")
	p = strings.ReplaceAll(p, "'", "")
	return strings.TrimPrefix(p, ".")
}

func joinPath(base, next string) string {
	if base == "" {
		return next
	}
	if next == "" {
		return base
	}
	return base + "." + next
}
