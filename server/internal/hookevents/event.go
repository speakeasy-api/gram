package hookevents

import (
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

type Provider string

const (
	ProviderCodex  Provider = "codex"
	ProviderClaude Provider = "claude"
	ProviderCursor Provider = "cursor"
)

type EventType string

const (
	EventTypeConfigChange        EventType = "config_change"
	EventTypeSessionStart        EventType = "session_start"
	EventTypeBeforeToolUse       EventType = "before_tool_use"
	EventTypeAfterToolUse        EventType = "after_tool_use"
	EventTypeAfterToolUseFailure EventType = "after_tool_use_failure"
	EventTypeBeforeMCPExecution  EventType = "before_mcp_execution"
	EventTypeAfterMCPExecution   EventType = "after_mcp_execution"
	EventTypePermissionRequest   EventType = "permission_request"
	EventTypeUserPromptSubmit    EventType = "user_prompt_submit"
	EventTypeAfterAgentResponse  EventType = "after_agent_response"
	EventTypeAfterAgentThought   EventType = "after_agent_thought"
	EventTypeStop                EventType = "stop"
	EventTypeSessionEnd          EventType = "session_end"
	EventTypeNotification        EventType = "notification"
)

type Identity struct {
	OrganizationID string
	ProjectID      uuid.UUID
	UserID         string
	UserEmail      string
}

type Event struct {
	Provider       Provider
	Type           EventType
	RawEventType   string
	Timestamp      time.Time
	AuthContext    *contextvalues.AuthContext
	OrganizationID string
	ProjectID      uuid.UUID
	UserID         string
	UserEmail      string
	ConversationID string
	Raw            any
}

type SessionStart struct {
	Event
}

func NewSessionStart(event Event) *SessionStart {
	event.Type = EventTypeSessionStart
	return &SessionStart{Event: event}
}

type ConfigChange struct {
	Event
}

func NewConfigChange(event Event) *ConfigChange {
	event.Type = EventTypeConfigChange
	return &ConfigChange{Event: event}
}

type BeforeToolUse struct {
	Event
	ToolName  string
	ToolInput any
}

func NewBeforeToolUse(event Event, toolName string, toolInput any) *BeforeToolUse {
	event.Type = EventTypeBeforeToolUse
	return &BeforeToolUse{
		Event:     event,
		ToolName:  toolName,
		ToolInput: toolInput,
	}
}

type BeforeMCPExecution struct {
	Event
	ToolName  string
	ToolInput any
}

func NewBeforeMCPExecution(event Event, toolName string, toolInput any) *BeforeMCPExecution {
	event.Type = EventTypeBeforeMCPExecution
	return &BeforeMCPExecution{
		Event:     event,
		ToolName:  toolName,
		ToolInput: toolInput,
	}
}

type AfterToolUse struct {
	Event
	ToolName   string
	ToolOutput any
}

func NewAfterToolUse(event Event, toolName string, toolOutput any) *AfterToolUse {
	event.Type = EventTypeAfterToolUse
	return &AfterToolUse{
		Event:      event,
		ToolName:   toolName,
		ToolOutput: toolOutput,
	}
}

type AfterToolUseFailure struct {
	Event
	ToolName    string
	Error       any
	IsInterrupt bool
}

func NewAfterToolUseFailure(event Event, toolName string, err any, isInterrupt bool) *AfterToolUseFailure {
	event.Type = EventTypeAfterToolUseFailure
	return &AfterToolUseFailure{
		Event:       event,
		ToolName:    toolName,
		Error:       err,
		IsInterrupt: isInterrupt,
	}
}

type AfterMCPExecution struct {
	Event
	ToolName   string
	ToolOutput any
}

func NewAfterMCPExecution(event Event, toolName string, toolOutput any) *AfterMCPExecution {
	event.Type = EventTypeAfterMCPExecution
	return &AfterMCPExecution{
		Event:      event,
		ToolName:   toolName,
		ToolOutput: toolOutput,
	}
}

type PermissionRequest struct {
	Event
	ToolName       string
	ToolInput      any
	PermissionType string
}

func NewPermissionRequest(event Event, toolName string, toolInput any, permissionType string) *PermissionRequest {
	event.Type = EventTypePermissionRequest
	return &PermissionRequest{
		Event:          event,
		ToolName:       toolName,
		ToolInput:      toolInput,
		PermissionType: permissionType,
	}
}

type UserPromptSubmit struct {
	Event
	Prompt string
}

func NewUserPromptSubmit(event Event, prompt string) *UserPromptSubmit {
	event.Type = EventTypeUserPromptSubmit
	return &UserPromptSubmit{
		Event:  event,
		Prompt: prompt,
	}
}

type AfterAgentResponse struct {
	Event
	Text string
}

func NewAfterAgentResponse(event Event, text string) *AfterAgentResponse {
	event.Type = EventTypeAfterAgentResponse
	return &AfterAgentResponse{
		Event: event,
		Text:  text,
	}
}

type AfterAgentThought struct {
	Event
	Text       string
	DurationMs int
}

func NewAfterAgentThought(event Event, text string, durationMs int) *AfterAgentThought {
	event.Type = EventTypeAfterAgentThought
	return &AfterAgentThought{
		Event:      event,
		Text:       text,
		DurationMs: durationMs,
	}
}

type Stop struct {
	Event
	LastAssistantMessage string
}

func NewStop(event Event, lastAssistantMessage string) *Stop {
	event.Type = EventTypeStop
	return &Stop{
		Event:                event,
		LastAssistantMessage: lastAssistantMessage,
	}
}

type SessionEnd struct {
	Event
	Reason string
}

func NewSessionEnd(event Event, reason string) *SessionEnd {
	event.Type = EventTypeSessionEnd
	return &SessionEnd{
		Event:  event,
		Reason: reason,
	}
}

type Notification struct {
	Event
	NotificationType string
	Message          string
	Title            string
}

func NewNotification(event Event, notificationType, message, title string) *Notification {
	event.Type = EventTypeNotification
	return &Notification{
		Event:            event,
		NotificationType: notificationType,
		Message:          message,
		Title:            title,
	}
}
