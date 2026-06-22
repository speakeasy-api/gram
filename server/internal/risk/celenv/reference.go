package celenv

// Reference is the editor-facing catalog of the CEL environment: the variables,
// matcher methods, and macros an author may use, each with a human signature and
// description. It drives the dashboard's autocomplete and reference panel and
// nothing else.
//
// It is deliberately NOT a machine description of the env. The engine declares
// itself in New(), and that engine — compiled to wasm — is what type-checks
// expressions in the browser, so the reference carries no machine type-strings,
// overload ids, or binding keys, and there is no parity to maintain: at worst a
// stale doc string lags a renamed matcher, which the real engine surfaces as a
// compile error the moment the author uses it. Adding a matcher is one line in
// New() and one Func entry here.
type Reference struct {
	Variables []VarRef   `json:"variables"`
	Matchers  []FuncRef  `json:"matchers"`
	Macros    []MacroRef `json:"macros"`
}

// VarRef is one author-visible variable. Matchable marks the field-typed
// variables the matcher methods apply to (everything but kind). Fields is set
// only on tool_calls, whose elements expose name/server/function/args.
type VarRef struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // human label: "field", "string", "list(tool)"
	Description string   `json:"description"`
	Matchable   bool     `json:"matchable"`
	Fields      []VarRef `json:"fields,omitempty"`
}

// FuncRef is one member method on a field. ReturnsField marks get(), which
// yields a field the matchers chain off; the rest return bool.
type FuncRef struct {
	Name         string `json:"name"`
	Signature    string `json:"signature"`
	Description  string `json:"description"`
	ReturnsField bool   `json:"returnsField"`
}

// MacroRef is one CEL macro. ReturnsBool marks the predicate macros
// (has/all/exists/exists_one) that can stand as a whole rule, versus the
// list-producing ones (map/filter).
type MacroRef struct {
	Name        string `json:"name"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	ReturnsBool bool   `json:"returnsBool"`
}

// toolFields are the members of a tool_calls element, reused as the Fields of
// the tool_calls variable so completion can offer t.name/.server/... inside
// `tool_calls.exists(t, ...)`. Each is itself a matchable field.
var toolFields = []VarRef{
	{Name: "name", Type: "field", Matchable: true, Description: "The tool call's name (e.g. shell, query)."},
	{Name: "server", Type: "field", Matchable: true, Description: "The MCP server the tool belongs to."},
	{Name: "function", Type: "field", Matchable: true, Description: "The function/operation the tool invokes."},
	{Name: "args", Type: "field", Matchable: true, Description: "The tool call's arguments; drill in with .get(path)."},
}

// Describe returns the editor-facing catalog. It is the one place the authorable
// surface is described for humans; the engine itself is declared in New().
func Describe() Reference {
	return Reference{
		Variables: []VarRef{
			{Name: "kind", Type: "string", Description: "Message type: user_message, assistant_message, tool_request, or tool_response. Usually unnecessary — body fields are auto-scoped."},
			{Name: "content", Type: "field", Matchable: true, Description: "The message's raw text body, any message type."},
			{Name: "prompt", Type: "field", Matchable: true, Description: "The body of a user_message (empty otherwise)."},
			{Name: "assistant", Type: "field", Matchable: true, Description: "The body of an assistant_message (empty otherwise)."},
			{Name: "tool_result", Type: "field", Matchable: true, Description: "The output of a tool_response message (empty otherwise). Singular: one response message carries one tool's output."},
			{Name: "tool_calls", Type: "list(tool)", Description: "The tool calls on a tool_request message. Plural: one request can fan out parallel calls. Iterate with tool_calls.exists(t, ...); each tool has .name, .server, .function, .args (correlated to the same call).", Fields: toolFields},
		},
		Matchers: []FuncRef{
			{Name: "matchRegex", Signature: "field.matchRegex(pattern: string) -> bool", Description: "RE2 regex match anywhere in the value; records a span per occurrence. Case-insensitive via the (?i) flag, e.g. matchRegex(\"(?i)secret\")."},
			{Name: "matchText", Signature: "field.matchText(substring: string) -> bool", Description: "Case-insensitive literal substring; records a span per occurrence."},
			{Name: "matchExact", Signature: "field.matchExact(value: string) -> bool", Description: "Whole-value equality; records the whole-value span on match."},
			{Name: "matchPrefix", Signature: "field.matchPrefix(prefix: string) -> bool", Description: "Prefix match; records the prefix span."},
			{Name: "matchSuffix", Signature: "field.matchSuffix(suffix: string) -> bool", Description: "Suffix match; records the suffix span."},
			{Name: "matchGlob", Signature: "field.matchGlob(pattern: string) -> bool", Description: "Glob match over the whole value (e.g. *_exec, shell:*); records the whole-value span."},
			{Name: "get", Signature: "field.get(path: string) -> field", ReturnsField: true, Description: "Drill into the field's JSON at a gjson path (command, payload.sql, rows.0.ssn); returns a sub-field the matchers compose over."},
			{Name: "present", Signature: "field.present() -> bool", Description: "True when the field has a non-empty value."},
		},
		Macros: []MacroRef{
			{Name: "exists", Signature: "list.exists(x, predicate) -> bool", ReturnsBool: true, Description: "True when the predicate holds for at least one element. The usual way to match tool calls: tool_calls.exists(t, t.function.matchExact(\"bash\"))."},
			{Name: "all", Signature: "list.all(x, predicate) -> bool", ReturnsBool: true, Description: "True when the predicate holds for every element (vacuously true when the list is empty)."},
			{Name: "exists_one", Signature: "list.exists_one(x, predicate) -> bool", ReturnsBool: true, Description: "True when the predicate holds for exactly one element."},
			{Name: "has", Signature: "has(field) -> bool", ReturnsBool: true, Description: "True when a field/path is present and set, e.g. has(t.args.command). Use field.present() to also require non-empty."},
			{Name: "map", Signature: "list.map(x, expr) -> list", Description: "Transforms each element to expr, yielding a new list; the 3-arg form list.map(x, predicate, expr) maps only matching elements. Returns a list, not a verdict — feed it to another macro."},
			{Name: "filter", Signature: "list.filter(x, predicate) -> list", Description: "Keeps the elements where the predicate holds, yielding a sublist. Returns a list, not a verdict — combine with another macro to reach a bool."},
		},
	}
}
