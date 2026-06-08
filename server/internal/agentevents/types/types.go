package types

// Provider identifies the agent runtime that emitted a native hook payload.
type Provider string

// EventType is the normalized lifecycle event resolved from a provider payload.
type EventType string

const (
	// AnyEventType registers provider-wide resolvers such as FieldEventType.
	AnyEventType              EventType = "*"
	UserPromptSubmit          EventType = "user.prompt.submit"
	AssistantResponseComplete EventType = "assistant.response.complete"
	ToolCallStarted           EventType = "tool_call.started"
	ToolCallCompleted         EventType = "tool_call.completed"
	ToolCallFailed            EventType = "tool_call.failed"
	MCPToolCallStarted        EventType = "mcp_tool_call.started"
	MCPToolCallCompleted      EventType = "mcp_tool_call.completed"
	SessionStarted            EventType = "session.started"
	SessionEnded              EventType = "session.ended"
)

// Field identifies a logical value that handlers and builders can resolve from
// a provider-native payload without knowing that provider's payload shape.
type Field string

const (
	// FieldEventType resolves the normalized EventType for the native payload.
	FieldEventType Field = "event.type"

	// Hook fields describe the original hook envelope and framework decisions.
	FieldHookName     Field = "hook.name"
	FieldHookSource   Field = "hook.source"
	FieldHookHostname Field = "hook.hostname"
	FieldBlockReason  Field = "block.reason"
	FieldError        Field = "error"

	// Conversation fields are used to build storage-facing chat messages.
	FieldConversationID     Field = "conversation.id"
	FieldConversationChatID Field = "conversation.chat_id"
	FieldPrompt             Field = "prompt"
	FieldAssistantText      Field = "assistant.text"
	FieldModel              Field = "model"
	FieldFinishReason       Field = "finish_reason"

	// Scan fields are used by hook handlers for risk enforcement decisions.
	FieldScannableText   Field = "scan.text"
	FieldScanMessageType Field = "scan.message_type"

	// Tool fields describe tool call requests and responses.
	FieldToolName        Field = "tool.name"
	FieldToolDisplayName Field = "tool.display_name"
	FieldToolSource      Field = "tool.source"
	FieldToolInput       Field = "tool.input"
	FieldToolOutput      Field = "tool.output"
	FieldToolCallID      Field = "tool.call_id"

	// Usage fields describe token counters reported by agent runtimes.
	FieldUsageInputTokens      Field = "usage.input_tokens"
	FieldUsageOutputTokens     Field = "usage.output_tokens"
	FieldUsageCacheReadTokens  Field = "usage.cache_read_tokens"
	FieldUsageCacheWriteTokens Field = "usage.cache_write_tokens"
)
