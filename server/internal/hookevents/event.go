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

type User struct {
	ID    string
	Email string
}

type EventContext struct {
	OrganizationID string
	ProjectID      uuid.UUID
	User           User
}

type BaseEvent struct {
	Provider     Provider
	Type         EventType
	RawEventType string
	Timestamp    time.Time
	AuthContext  *contextvalues.AuthContext
	Context      EventContext
	Raw          any
}

type Event struct {
	BaseEvent
	ConversationID string
	TranscriptPath string
	CWD            string
	PermissionMode string
	Model          string
	HookHostname   string
	AdditionalData map[string]any
}

type Eventer interface {
	HookEvent() Event
}

func (e Event) HookEvent() Event {
	return e
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
	ToolCallID string
	ToolName   string
	ToolInput  any
}

type BeforeToolUseParams struct {
	ToolCallID string
	ToolName   string
	ToolInput  any
}

func NewBeforeToolUse(event Event, params BeforeToolUseParams) *BeforeToolUse {
	event.Type = EventTypeBeforeToolUse
	return &BeforeToolUse{
		Event:      event,
		ToolCallID: params.ToolCallID,
		ToolName:   params.ToolName,
		ToolInput:  params.ToolInput,
	}
}

type BeforeMCPExecution struct {
	Event
	ToolCallID string
	ToolName   string
	ToolInput  any
}

type BeforeMCPExecutionParams struct {
	ToolCallID string
	ToolName   string
	ToolInput  any
}

func NewBeforeMCPExecution(event Event, params BeforeMCPExecutionParams) *BeforeMCPExecution {
	event.Type = EventTypeBeforeMCPExecution
	return &BeforeMCPExecution{
		Event:      event,
		ToolCallID: params.ToolCallID,
		ToolName:   params.ToolName,
		ToolInput:  params.ToolInput,
	}
}

type AfterToolUse struct {
	Event
	ToolCallID string
	ToolName   string
	ToolOutput any
}

type AfterToolUseParams struct {
	ToolCallID string
	ToolName   string
	ToolOutput any
}

func NewAfterToolUse(event Event, params AfterToolUseParams) *AfterToolUse {
	event.Type = EventTypeAfterToolUse
	return &AfterToolUse{
		Event:      event,
		ToolCallID: params.ToolCallID,
		ToolName:   params.ToolName,
		ToolOutput: params.ToolOutput,
	}
}

type AfterToolUseFailure struct {
	Event
	ToolCallID  string
	ToolName    string
	Error       any
	IsInterrupt bool
}

type AfterToolUseFailureParams struct {
	ToolCallID  string
	ToolName    string
	Error       any
	IsInterrupt bool
}

func NewAfterToolUseFailure(event Event, params AfterToolUseFailureParams) *AfterToolUseFailure {
	event.Type = EventTypeAfterToolUseFailure
	return &AfterToolUseFailure{
		ToolCallID:  params.ToolCallID,
		Event:       event,
		ToolName:    params.ToolName,
		Error:       params.Error,
		IsInterrupt: params.IsInterrupt,
	}
}

type AfterMCPExecution struct {
	Event
	ToolCallID string
	ToolName   string
	ToolOutput any
}

type AfterMCPExecutionParams struct {
	ToolCallID string
	ToolName   string
	ToolOutput any
}

func NewAfterMCPExecution(event Event, params AfterMCPExecutionParams) *AfterMCPExecution {
	event.Type = EventTypeAfterMCPExecution
	return &AfterMCPExecution{
		Event:      event,
		ToolCallID: params.ToolCallID,
		ToolName:   params.ToolName,
		ToolOutput: params.ToolOutput,
	}
}

type PermissionRequest struct {
	Event
	ToolCallID     string
	ToolName       string
	ToolInput      any
	PermissionType string
}

type PermissionRequestParams struct {
	ToolCallID     string
	ToolName       string
	ToolInput      any
	PermissionType string
}

func NewPermissionRequest(event Event, params PermissionRequestParams) *PermissionRequest {
	event.Type = EventTypePermissionRequest
	return &PermissionRequest{
		Event:          event,
		ToolCallID:     params.ToolCallID,
		ToolName:       params.ToolName,
		ToolInput:      params.ToolInput,
		PermissionType: params.PermissionType,
	}
}

type UserPromptSubmit struct {
	Event
	Prompt string
}

type UserPromptSubmitParams struct {
	Prompt string
}

func NewUserPromptSubmit(event Event, params UserPromptSubmitParams) *UserPromptSubmit {
	event.Type = EventTypeUserPromptSubmit
	return &UserPromptSubmit{
		Event:  event,
		Prompt: params.Prompt,
	}
}

type AfterAgentResponse struct {
	Event
	Text string
}

type AfterAgentResponseParams struct {
	Text string
}

func NewAfterAgentResponse(event Event, params AfterAgentResponseParams) *AfterAgentResponse {
	event.Type = EventTypeAfterAgentResponse
	return &AfterAgentResponse{
		Event: event,
		Text:  params.Text,
	}
}

type AfterAgentThought struct {
	Event
	Text       string
	DurationMs int
}

type AfterAgentThoughtParams struct {
	Text       string
	DurationMs int
}

func NewAfterAgentThought(event Event, params AfterAgentThoughtParams) *AfterAgentThought {
	event.Type = EventTypeAfterAgentThought
	return &AfterAgentThought{
		Event:      event,
		Text:       params.Text,
		DurationMs: params.DurationMs,
	}
}

type Stop struct {
	Event
	LastAssistantMessage string
}

type StopParams struct {
	LastAssistantMessage string
}

func NewStop(event Event, params StopParams) *Stop {
	event.Type = EventTypeStop
	return &Stop{
		Event:                event,
		LastAssistantMessage: params.LastAssistantMessage,
	}
}

type SessionEnd struct {
	Event
	Reason string
}

type SessionEndParams struct {
	Reason string
}

func NewSessionEnd(event Event, params SessionEndParams) *SessionEnd {
	event.Type = EventTypeSessionEnd
	return &SessionEnd{
		Event:  event,
		Reason: params.Reason,
	}
}

type Notification struct {
	Event
	NotificationType string
	Message          string
	Title            string
}

type NotificationParams struct {
	NotificationType string
	Message          string
	Title            string
}

func NewNotification(event Event, params NotificationParams) *Notification {
	event.Type = EventTypeNotification
	return &Notification{
		Event:            event,
		NotificationType: params.NotificationType,
		Message:          params.Message,
		Title:            params.Title,
	}
}
