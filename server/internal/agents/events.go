package agents

import (
	"encoding/json"
	"fmt"
)

// SubAgentEventType represents the type of sub-agent SSE event
type SubAgentEventType string

const (
	// SubAgentEventSpawn is sent when a new sub-agent is spawned
	SubAgentEventSpawn SubAgentEventType = "sub_agent.spawn"
	// SubAgentEventDelta is sent for streaming text content from a sub-agent
	SubAgentEventDelta SubAgentEventType = "sub_agent.delta"
	// SubAgentEventToolCall is sent when a sub-agent makes a tool call
	SubAgentEventToolCall SubAgentEventType = "sub_agent.tool_call"
	// SubAgentEventToolResult is sent when a tool call returns a result
	SubAgentEventToolResult SubAgentEventType = "sub_agent.tool_result"
	// SubAgentEventComplete is sent when a sub-agent completes execution
	SubAgentEventComplete SubAgentEventType = "sub_agent.complete"
)

// SubAgentEvent is the base interface for all sub-agent events
type SubAgentEvent interface {
	EventType() SubAgentEventType
	AgentID() string
	ToJSON() ([]byte, error)
}

// SubAgentSpawnEvent is sent when a new sub-agent is spawned
type SubAgentSpawnEvent struct {
	Type        SubAgentEventType `json:"type"`
	ID          string            `json:"agent_id"`
	ParentID    *string           `json:"parent_id,omitempty"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Task        string            `json:"task"`
}

func (e SubAgentSpawnEvent) EventType() SubAgentEventType { return SubAgentEventSpawn }
func (e SubAgentSpawnEvent) AgentID() string              { return e.ID }
func (e SubAgentSpawnEvent) ToJSON() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("marshal spawn event: %w", err)
	}
	return data, nil
}

// SubAgentDeltaEvent is sent for streaming text content from a sub-agent
type SubAgentDeltaEvent struct {
	Type    SubAgentEventType `json:"type"`
	ID      string            `json:"agent_id"`
	Content string            `json:"content"`
}

func (e SubAgentDeltaEvent) EventType() SubAgentEventType { return SubAgentEventDelta }
func (e SubAgentDeltaEvent) AgentID() string              { return e.ID }
func (e SubAgentDeltaEvent) ToJSON() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("marshal delta event: %w", err)
	}
	return data, nil
}

// SubAgentToolCallEvent is sent when a sub-agent makes a tool call
type SubAgentToolCallEvent struct {
	Type       SubAgentEventType `json:"type"`
	ID         string            `json:"agent_id"`
	ToolCallID string            `json:"tool_call_id"`
	ToolName   string            `json:"tool_name"`
	Args       map[string]any    `json:"args"`
}

func (e SubAgentToolCallEvent) EventType() SubAgentEventType { return SubAgentEventToolCall }
func (e SubAgentToolCallEvent) AgentID() string              { return e.ID }
func (e SubAgentToolCallEvent) ToJSON() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("marshal tool call event: %w", err)
	}
	return data, nil
}

// SubAgentToolResultEvent is sent when a tool call returns a result
type SubAgentToolResultEvent struct {
	Type       SubAgentEventType `json:"type"`
	ID         string            `json:"agent_id"`
	ToolCallID string            `json:"tool_call_id"`
	ToolName   string            `json:"tool_name"`
	Result     string            `json:"result"`
	IsError    bool              `json:"is_error,omitempty"`
}

func (e SubAgentToolResultEvent) EventType() SubAgentEventType { return SubAgentEventToolResult }
func (e SubAgentToolResultEvent) AgentID() string              { return e.ID }
func (e SubAgentToolResultEvent) ToJSON() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("marshal tool result event: %w", err)
	}
	return data, nil
}

// SubAgentCompleteEvent is sent when a sub-agent completes execution
type SubAgentCompleteEvent struct {
	Type   SubAgentEventType `json:"type"`
	ID     string            `json:"agent_id"`
	Status string            `json:"status"` // "completed" or "failed"
	Result *string           `json:"result,omitempty"`
	Error  *string           `json:"error,omitempty"`
}

func (e SubAgentCompleteEvent) EventType() SubAgentEventType { return SubAgentEventComplete }
func (e SubAgentCompleteEvent) AgentID() string              { return e.ID }
func (e SubAgentCompleteEvent) ToJSON() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("marshal complete event: %w", err)
	}
	return data, nil
}
