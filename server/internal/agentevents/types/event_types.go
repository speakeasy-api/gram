package types

// EventType is the provider-neutral lifecycle event resolved from a hook
// payload before hooks decide how to handle it.
type EventType string

const (
	AnyEventType              EventType = "*"
	UserPromptSubmit          EventType = "user.prompt.submit"
	AssistantResponseComplete EventType = "assistant.response.complete"
	ToolCallStarted           EventType = "tool_call.started"
	ToolCallCompleted         EventType = "tool_call.completed"
	ToolCallFailed            EventType = "tool_call.failed"
	MCPToolCallStarted        EventType = "mcp_tool_call.started"
	MCPToolCallCompleted      EventType = "mcp_tool_call.completed"
	SessionEnded              EventType = "session.ended"
)

type HookEventType string
