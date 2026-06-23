// Package celenv defines the single CEL environment for risk rule expressions.
// Every expression is a boolean predicate; the eval modes differ only in what
// they read off the result — EvalScope returns the verdict, EvalDetection also
// returns the spans the matcher methods recorded so the dashboard can highlight.
//
// Fields: kind (string); content/prompt/assistant/tool_result (field); tool_calls
// (list of tool, each with .name/.server/.function/.args). Body fields are
// auto-scoped (prompt is empty outside user messages, so no kind check needed);
// inside tool_calls.exists(t, ...) a tool's fields are correlated to the same
// call; any field's .get(path) drills into its JSON. Spans are captured as a
// side effect of the matchers via a per-eval collector, so eval is thread-safe.
//
// celenv is self-contained (its own Message/Tool/Span types) so the analysis
// engine can import it without a cycle.
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

// Tool is one tool call exposed to expressions as an element of `tool_calls`.
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

// celTool is the registered object type for a tool_calls element; declaring its
// fields explicitly gives compile-time validation (t.functionn is an error).
type celTool struct {
	Name     *fieldVal `cel:"name"`
	Server   *fieldVal `cel:"server"`
	Function *fieldVal `cel:"function"`
	Args     *fieldVal `cel:"args"`
}

const toolTypeName = "celenv.celTool"

type Engine struct {
	env *cel.Env
}

// New builds the CEL environment — the single source of truth for what an
// expression may reference.
func New() (*Engine, error) {
	env, err := cel.NewEnv(
		ext.NativeTypes(reflect.TypeFor[celTool](), ext.ParseStructTags(true)),

		cel.Variable("kind", cel.StringType),
		cel.Variable("content", fieldType),
		cel.Variable("prompt", fieldType),
		cel.Variable("assistant", fieldType),
		cel.Variable("tool_result", fieldType),
		cel.Variable("tool_calls", cel.ListType(cel.ObjectType(toolTypeName))),

		matcher("matchRegex", (*fieldVal).matchRegex),
		matcher("matchText", (*fieldVal).matchText),
		matcher("matchExact", (*fieldVal).matchExact),
		matcher("matchPrefix", (*fieldVal).matchPrefix),
		matcher("matchSuffix", (*fieldVal).matchSuffix),
		matcher("matchGlob", (*fieldVal).matchGlob),

		cel.Function("get", cel.MemberOverload("field_get_string",
			[]*cel.Type{fieldType, cel.StringType}, fieldType,
			cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
				f, ok := lhs.(*fieldVal)
				if !ok {
					return types.NewErr("get: receiver is not a field")
				}
				p, ok := rhs.(types.String)
				if !ok {
					return types.NewErr("get: path must be string")
				}
				return f.get(string(p))
			}))),
		cel.Function("present", cel.MemberOverload("field_present",
			[]*cel.Type{fieldType}, cel.BoolType,
			cel.UnaryBinding(func(v ref.Val) ref.Val {
				f, ok := v.(*fieldVal)
				if !ok {
					return types.NewErr("present: receiver is not a field")
				}
				return types.Bool(f.present())
			}))),
	)
	if err != nil {
		return nil, fmt.Errorf("build cel env: %w", err)
	}
	return &Engine{env: env}, nil
}

// matcher declares one field.matchX(string) -> bool overload bound to fn.
func matcher(name string, fn func(*fieldVal, string) bool) cel.EnvOption {
	overloadID := "field_" + strings.ToLower(name) + "_string"
	return cel.Function(name, cel.MemberOverload(overloadID,
		[]*cel.Type{fieldType, cel.StringType}, cel.BoolType,
		cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
			f, ok := lhs.(*fieldVal)
			if !ok {
				return types.NewErr("%s: receiver is not a field", name)
			}
			s, ok := rhs.(types.String)
			if !ok {
				return types.NewErr("%s: argument must be string", name)
			}
			return types.Bool(fn(f, string(s)))
		})))
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
		"kind":        msg.Type,
		"content":     body("content", true),
		"prompt":      body("prompt", msg.Type == message.User),
		"assistant":   body("assistant", msg.Type == message.Assistant),
		"tool_result": body("tool_result", msg.Type == message.ToolResponse),
		"tool_calls":  tools,
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
