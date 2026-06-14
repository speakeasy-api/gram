package types

// Field identifies a logical value that handlers and builders can resolve from
// a provider-native payload without knowing that provider's payload shape.
type Field string

const (
	// FieldEventType resolves the normalized EventType for the native payload.
	FieldEventType Field = "event.type"

	// Hook fields describe the original hook envelope.
	FieldHookHostname Field = "hook.hostname"
	FieldError        Field = "error"

	// Conversation fields are used to build storage-facing chat messages.
	FieldPrompt        Field = "prompt"
	FieldAssistantText Field = "assistant.text"
	FieldModel         Field = "model"

	// Scan fields are used by hook handlers for risk enforcement decisions.
	FieldScannableText   Field = "scan.text"
	FieldScanMessageType Field = "scan.message_type"

	// Tool fields describe tool call requests and responses.
	FieldToolName   Field = "tool.name"
	FieldToolSource Field = "tool.source"
	FieldToolInput  Field = "tool.input"
	FieldToolOutput Field = "tool.output"
	FieldToolCallID Field = "tool.call_id"

	// Usage fields describe token counters reported by agent runtimes.
	FieldUsageInputTokens      Field = "usage.input_tokens"
	FieldUsageOutputTokens     Field = "usage.output_tokens"
	FieldUsageCacheReadTokens  Field = "usage.cache_read_tokens"
	FieldUsageCacheWriteTokens Field = "usage.cache_write_tokens"
)
