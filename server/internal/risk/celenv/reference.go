package celenv

// Reference is the editor-facing catalog of the CEL environment, driving
// autocomplete and the reference panel. The engine (New) is authoritative; this
// carries no machine types and has no parity to maintain.
type Reference struct {
	Variables []VarRef   `json:"variables"`
	Matchers  []FuncRef  `json:"matchers"`
	Macros    []MacroRef `json:"macros"`
}

type VarRef struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Matchable   bool     `json:"matchable"` // field-typed: matchers apply
	Fields      []VarRef `json:"fields,omitempty"`
}

type FuncRef struct {
	Name         string `json:"name"`
	Signature    string `json:"signature"`
	Description  string `json:"description"`
	ReturnsField bool   `json:"returnsField"` // get() chains; the rest return bool
}

type MacroRef struct {
	Name        string `json:"name"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	ReturnsBool bool   `json:"returnsBool"`
}

// Describe returns a freshly-built catalog (no shared backing arrays).
func Describe() Reference {
	field := func(name, desc string) VarRef {
		return VarRef{Name: name, Type: "field", Description: desc, Matchable: true, Fields: nil}
	}
	matcher := func(name, sig, desc string) FuncRef {
		return FuncRef{Name: name, Signature: sig, Description: desc, ReturnsField: false}
	}
	predicate := func(name, sig, desc string) MacroRef {
		return MacroRef{Name: name, Signature: sig, Description: desc, ReturnsBool: true}
	}
	producer := func(name, sig, desc string) MacroRef {
		return MacroRef{Name: name, Signature: sig, Description: desc, ReturnsBool: false}
	}

	toolFields := []VarRef{
		field("name", "The tool call's name (e.g. shell, query)."),
		field("server", "The MCP server the tool belongs to."),
		field("function", "The function/operation the tool invokes."),
		field("args", "The tool call's arguments; drill in with .get(path)."),
	}

	return Reference{
		Variables: []VarRef{
			{Name: "kind", Type: "string", Description: "Message type: user_message, assistant_message, tool_request, tool_response, or prompt_attachment. Usually unnecessary — body fields are auto-scoped.", Matchable: false, Fields: nil},
			field("content", "The message's raw text body, any message type."),
			field("prompt", "The body of a user_message (empty otherwise)."),
			field("assistant", "The body of an assistant_message (empty otherwise)."),
			field("tool_result", "The output of a tool_response message (empty otherwise). Singular: one response message carries one tool's output."),
			{Name: "tool_calls", Type: "list(tool)", Description: "The tool calls on a tool_request message. Plural: one request can fan out parallel calls. Iterate with tool_calls.exists(t, ...); each tool has .name, .server, .function, .args (correlated to the same call).", Matchable: false, Fields: toolFields},
		},
		Matchers: []FuncRef{
			matcher("matchRegex", "field.matchRegex(pattern: string) -> bool", "RE2 regex match anywhere in the value; records a span per occurrence. Case-insensitive via the (?i) flag, e.g. matchRegex(\"(?i)secret\")."),
			matcher("matchText", "field.matchText(substring: string) -> bool", "Case-insensitive literal substring; records a span per occurrence."),
			matcher("matchExact", "field.matchExact(value: string) -> bool", "Whole-value equality; records the whole-value span on match."),
			matcher("matchPrefix", "field.matchPrefix(prefix: string) -> bool", "Prefix match; records the prefix span."),
			matcher("matchSuffix", "field.matchSuffix(suffix: string) -> bool", "Suffix match; records the suffix span."),
			matcher("matchGlob", "field.matchGlob(pattern: string) -> bool", "Glob match over the whole value (e.g. *_exec, shell:*); records the whole-value span."),
			{Name: "get", Signature: "field.get(path: string) -> field", Description: "Drill into the field's JSON at a gjson path (command, payload.sql, rows.0.ssn); returns a sub-field the matchers compose over.", ReturnsField: true},
			matcher("present", "field.present() -> bool", "True when the field has a non-empty value."),
		},
		Macros: []MacroRef{
			predicate("exists", "list.exists(x, predicate) -> bool", "True when the predicate holds for at least one element. The usual way to match tool calls: tool_calls.exists(t, t.function.matchExact(\"bash\"))."),
			predicate("all", "list.all(x, predicate) -> bool", "True when the predicate holds for every element (vacuously true when the list is empty)."),
			predicate("exists_one", "list.exists_one(x, predicate) -> bool", "True when the predicate holds for exactly one element."),
			predicate("has", "has(field) -> bool", "True when a field/path is present and set, e.g. has(t.args.command). Use field.present() to also require non-empty."),
			producer("map", "list.map(x, expr) -> list", "Transforms each element to expr, yielding a new list; the 3-arg form list.map(x, predicate, expr) maps only matching elements. Returns a list, not a verdict — feed it to another macro."),
			producer("filter", "list.filter(x, predicate) -> list", "Keeps the elements where the predicate holds, yielding a sublist. Returns a list, not a verdict — combine with another macro to reach a bool."),
		},
	}
}
