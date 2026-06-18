package types

// EventType is the provider-neutral hook processing event resolved from a hook
// payload before handlers decide how to respond.
type EventType string

const (
	AnyEventType        EventType = "*"
	UserPromptSubmit    EventType = "user_prompt_submit"
	AfterAgentResponse  EventType = "after_agent_response"
	BeforeToolUse       EventType = "before_tool_use"
	AfterToolUse        EventType = "after_tool_use"
	AfterToolUseFailure EventType = "after_tool_use_failure"
	BeforeMCPExecution  EventType = "before_mcp_execution"
	AfterMCPExecution   EventType = "after_mcp_execution"
	Stop                EventType = "stop"
)

type HookEventType string
