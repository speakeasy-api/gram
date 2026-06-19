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
			{Name: "type", Type: "string", Description: "Message type: user_message, assistant_message, tool_request, or tool_response. Usually unnecessary — body fields are auto-scoped."},
			{Name: "content", Type: "field", Description: "The message's raw text body, any message type."},
			{Name: "prompt", Type: "field", Description: "The body of a user_message (empty otherwise)."},
			{Name: "assistant", Type: "field", Description: "The body of an assistant_message (empty otherwise)."},
			{Name: "output", Type: "field", Description: "The body of a tool_response — the tool's output (empty otherwise)."},
			{Name: "tools", Type: "list(tool)", Description: "Tool calls on a tool_request. Iterate with tools.exists(t, ...); each tool has .name, .server, .function, .args fields (correlated to the same call)."},
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
