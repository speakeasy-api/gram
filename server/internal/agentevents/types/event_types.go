package types

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
