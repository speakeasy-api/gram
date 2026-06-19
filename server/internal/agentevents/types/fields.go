package types

// Field identifies a logical value that handlers can resolve from a
// provider-native payload without knowing that provider's payload shape.
type Field string

const (
	FieldEventType Field = "event.type"

	FieldPrompt     Field = "prompt"
	FieldToolName   Field = "tool.name"
	FieldToolInput  Field = "tool.input"
	FieldToolOutput Field = "tool.output"
)
