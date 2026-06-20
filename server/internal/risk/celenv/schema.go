package celenv

// Schema describes the CEL environment for the dashboard expression editor:
// the variables an author may reference and the helper functions available. It
// is derived from the same declarations buildEnv uses, so the editor cannot
// drift from what the backend actually accepts.
type Schema struct {
	Variables []VariableDescriptor `json:"variables"`
	Functions []FunctionDescriptor `json:"functions"`
}

// VariableDescriptor is one author-visible variable.
type VariableDescriptor struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	// Fields lists the member fields available on each element when this
	// variable is an object or a list of objects — e.g. an element of `tools`
	// is a `tool` exposing name/server/function. Empty for scalar/field
	// variables. The editor uses these to complete the macro bind variable in
	// `tools.exists(t, t.name...)`.
	Fields []MemberDescriptor `json:"fields,omitempty"`
}

// MemberDescriptor is one member field on an object-typed variable's element
// (e.g. `name` on a `tool`).
type MemberDescriptor struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// FunctionDescriptor is one author-visible helper function signature.
type FunctionDescriptor struct {
	Name        string `json:"name"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
}

// Describe returns the editor-facing schema for the environment.
func Describe() Schema {
	return Schema{
		Variables: []VariableDescriptor{
			{Name: "type", Type: "string", Description: "Message type: user_message, assistant_message, tool_request, or tool_response. Usually unnecessary — body fields are auto-scoped.", Fields: nil},
			{Name: "content", Type: "field", Description: "The message's raw text body, any message type.", Fields: nil},
			{Name: "prompt", Type: "field", Description: "The body of a user_message (empty otherwise).", Fields: nil},
			{Name: "assistant", Type: "field", Description: "The body of an assistant_message (empty otherwise).", Fields: nil},
			{Name: "output", Type: "field", Description: "The body of a tool_response — the tool's output (empty otherwise).", Fields: nil},
			{Name: "tools", Type: "list(tool)", Description: "Tool calls on a tool_request. Iterate with tools.exists(t, ...); each tool has .name, .server, .function, .args fields (correlated to the same call).", Fields: []MemberDescriptor{
				{Name: "name", Type: "field", Description: "The tool call's name (e.g. shell, query)."},
				{Name: "server", Type: "field", Description: "The MCP server the tool belongs to."},
				{Name: "function", Type: "field", Description: "The function/operation the tool invokes."},
				{Name: "args", Type: "field", Description: "The tool call's arguments; drill in with .get(path)."},
			}},
		},
		Functions: []FunctionDescriptor{
			{Name: "match", Signature: "field.match(pattern: string) -> bool", Description: "RE2 regex match; records a span per occurrence."},
			{Name: "includes", Signature: "field.includes(substr: string) -> bool", Description: "Case-insensitive substring; records a span per occurrence."},
			{Name: "eq", Signature: "field.eq(value: string) -> bool", Description: "Exact equality; records the whole-value span on match."},
			{Name: "prefix", Signature: "field.prefix(s: string) -> bool", Description: "Prefix match; records the prefix span."},
			{Name: "suffix", Signature: "field.suffix(s: string) -> bool", Description: "Suffix match; records the suffix span."},
			{Name: "glob", Signature: "field.glob(pattern: string) -> bool", Description: "Glob match over the whole value (e.g. *_exec, shell:*); records the whole-value span."},
			{Name: "present", Signature: "field.present() -> bool", Description: "True when the field has a non-empty value."},
			{Name: "get", Signature: "field.get(path: string) -> field", Description: "Drill into the field's JSON at a gjson path (command, payload.sql, rows.0.ssn); returns a sub-field the matchers compose over."},
		},
	}
}
