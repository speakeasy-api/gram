package relay

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/speakeasy-api/agenthooks"
)

const schemaVersion = "hook.ingest.v1"

type ingestSource struct {
	Adapter        string `json:"adapter"`
	AdapterVersion string `json:"adapter_version,omitempty"`
	RawEventName   string `json:"raw_event_name,omitempty"`
	Hostname       string `json:"hostname,omitempty"`
}

type ingestSession struct {
	ID     string `json:"id,omitempty"`
	TurnID string `json:"turn_id,omitempty"`
	CWD    string `json:"cwd,omitempty"`
	Model  string `json:"model,omitempty"`
}

type ingestEvent struct {
	Type       string `json:"type"`
	OccurredAt string `json:"occurred_at,omitempty"`
}

type promptData struct {
	Text string `json:"text"`
}

type toolCallData struct {
	ID             string          `json:"id,omitempty"`
	Name           string          `json:"name,omitempty"`
	Input          json.RawMessage `json:"input,omitempty"`
	Output         json.RawMessage `json:"output,omitempty"`
	Error          json.RawMessage `json:"error,omitempty"`
	IsInterrupt    *bool           `json:"is_interrupt,omitempty"`
	PermissionType string          `json:"permission_type,omitempty"`
	DurationMS     *float64        `json:"duration_ms,omitempty"`
	Status         string          `json:"status,omitempty"`
}

type mcpData struct {
	ServerName     string `json:"server_name,omitempty"`
	ServerIdentity string `json:"server_identity,omitempty"`
	URL            string `json:"url,omitempty"`
	Command        string `json:"command,omitempty"`
	ResultJSON     string `json:"result_json,omitempty"`
}

type usageData struct {
	InputTokens      *int     `json:"input_tokens,omitempty"`
	OutputTokens     *int     `json:"output_tokens,omitempty"`
	CacheReadTokens  *int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int     `json:"cache_write_tokens,omitempty"`
	Cost             *float64 `json:"cost,omitempty"`
	LoopCount        *int     `json:"loop_count,omitempty"`
	Status           string   `json:"status,omitempty"`
}

type messageData struct {
	Text       string   `json:"text"`
	Role       string   `json:"role,omitempty"`
	DurationMS *float64 `json:"duration_ms,omitempty"`
}

type skillData struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
}

type notificationData struct {
	Type    string `json:"type,omitempty"`
	Title   string `json:"title,omitempty"`
	Message string `json:"message,omitempty"`
}

type ingestData struct {
	Prompt       *promptData       `json:"prompt,omitempty"`
	ToolCall     *toolCallData     `json:"tool_call,omitempty"`
	MCP          *mcpData          `json:"mcp,omitempty"`
	Usage        *usageData        `json:"usage,omitempty"`
	Message      *messageData      `json:"message,omitempty"`
	Skill        *skillData        `json:"skill,omitempty"`
	Notification *notificationData `json:"notification,omitempty"`
}

type ingestPayload struct {
	SchemaVersion  string          `json:"schema_version"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Source         ingestSource    `json:"source"`
	Session        *ingestSession  `json:"session,omitempty"`
	Event          ingestEvent     `json:"event"`
	Data           *ingestData     `json:"data,omitempty"`
	Raw            json.RawMessage `json:"raw,omitempty"`
}

// adapterSlug maps an agenthooks provider onto the stable Gram adapter slug the
// backend expects (it keys its provider-style telemetry vocabulary on these).
func adapterSlug(p agenthooks.Provider) string {
	switch p {
	case agenthooks.ProviderClaudeCode:
		return "claude"
	default:
		return string(p)
	}
}

// canonicalEventType resolves the Gram canonical event.type for a normalized
// event. It mirrors the per-provider native→canonical tables the bash senders
// used so the backend's raw-event-name round-trip keeps counting rows, then
// falls back to a kind-based mapping for providers/events those tables omit.
func canonicalEventType(e *agenthooks.Event) string {
	switch e.Provider {
	case agenthooks.ProviderClaudeCode:
		switch e.NativeName {
		case "SessionStart":
			return "session.started"
		case "ConfigChange":
			return "session.updated"
		case "PreToolUse":
			return "tool.requested"
		case "PostToolUse":
			return "tool.completed"
		case "PostToolUseFailure":
			return "tool.failed"
		case "UserPromptSubmit":
			return "prompt.submitted"
		case "Stop":
			return "assistant.responded"
		case "SessionEnd":
			return "session.ended"
		case "Notification":
			return "notification.reported"
		}
	case agenthooks.ProviderCursor:
		switch e.NativeName {
		case "sessionStart":
			return "session.started"
		case "beforeSubmitPrompt":
			return "prompt.submitted"
		case "afterAgentResponse":
			return "assistant.responded"
		case "afterAgentThought":
			return "assistant.thought"
		case "preToolUse", "beforeShellExecution", "beforeReadFile":
			return "tool.requested"
		case "postToolUse", "afterShellExecution":
			return "tool.completed"
		case "postToolUseFailure":
			return "tool.failed"
		case "beforeMCPExecution":
			return "tool.requested"
		case "afterMCPExecution":
			return "tool.completed"
		case "stop":
			return "usage.reported"
		}
	case agenthooks.ProviderCodex:
		switch e.NativeName {
		case "SessionStart":
			return "session.started"
		case "PreToolUse", "PermissionRequest":
			return "tool.requested"
		case "PostToolUse":
			return "tool.completed"
		case "UserPromptSubmit":
			return "prompt.submitted"
		case "Stop":
			return "assistant.responded"
		}
	}
	return kindEventType(e.Kind)
}

func kindEventType(k agenthooks.EventKind) string {
	switch k {
	case agenthooks.KindSessionStart:
		return "session.started"
	case agenthooks.KindSessionEnd:
		return "session.ended"
	case agenthooks.KindPromptSubmitted:
		return "prompt.submitted"
	case agenthooks.KindToolPre, agenthooks.KindPermission:
		return "tool.requested"
	case agenthooks.KindToolPost:
		return "tool.completed"
	case agenthooks.KindToolError:
		return "tool.failed"
	case agenthooks.KindStop, agenthooks.KindSubagentStop:
		return "assistant.responded"
	case agenthooks.KindNotification:
		return "notification.reported"
	default:
		return "session.updated"
	}
}

// buildEnvelope projects a normalized agenthooks event onto the canonical Gram
// ingest payload. Feature blocks are populated only for the event's kind; the
// verbatim provider payload rides under raw for debugging (the backend never
// reads it for behavior).
func buildEnvelope(typed any, hostname string) ingestPayload {
	base := agenthooks.EventOf(typed)
	eventType := canonicalEventType(base)
	data := &ingestData{
		Prompt:       nil,
		ToolCall:     nil,
		MCP:          nil,
		Usage:        nil,
		Message:      nil,
		Skill:        nil,
		Notification: nil,
	}

	switch ev := typed.(type) {
	case *agenthooks.PromptEvent:
		if ev.Prompt != "" {
			data.Prompt = &promptData{Text: ev.Prompt}
		}
	case *agenthooks.ToolPreEvent:
		eventType = applyToolCall(data, base, &ev.Tool, eventType, "", nil, nil, nil, nil)
	case *agenthooks.PermissionEvent:
		pt := permissionTypeOf(base)
		eventType = applyToolCall(data, base, &ev.Tool, eventType, pt, nil, nil, nil, nil)
	case *agenthooks.ToolPostEvent:
		var output, errOut json.RawMessage
		if ev.Failed {
			errOut = toolErrorPayload(ev)
		} else {
			output = normalizeOutput(ev.Output)
		}
		var interrupt *bool
		if ev.Failed {
			b := isInterrupt(base)
			if b {
				interrupt = &b
			}
		}
		eventType = applyToolCall(data, base, &ev.Tool, eventType, "", output, errOut, interrupt, ev.DurationMS)
	case *agenthooks.StopEvent:
		if ev.FinalMessage != "" {
			data.Message = &messageData{Text: ev.FinalMessage, Role: "assistant", DurationMS: nil}
		}
		if ev.Usage != nil {
			data.Usage = &usageData{
				InputTokens:      ev.Usage.InputTokens,
				OutputTokens:     ev.Usage.OutputTokens,
				CacheReadTokens:  ev.Usage.CacheReadTokens,
				CacheWriteTokens: ev.Usage.CacheWriteTokens,
				Cost:             ev.Usage.Cost,
				LoopCount:        ev.Usage.LoopCount,
				Status:           ev.Usage.Status,
			}
		}
	case *agenthooks.NotificationEvent:
		if ev.Message != "" {
			data.Notification = &notificationData{Type: "", Title: "", Message: ev.Message}
		}
	case *agenthooks.ModelEvent:
		applyModelResponse(data, base)
	}

	payload := ingestPayload{
		SchemaVersion:  schemaVersion,
		IdempotencyKey: "",
		Source: ingestSource{
			Adapter:        adapterSlug(base.Provider),
			AdapterVersion: "",
			RawEventName:   base.NativeName,
			Hostname:       hostname,
		},
		Session: sessionOf(base),
		Event: ingestEvent{
			Type:       eventType,
			OccurredAt: occurredAt(base),
		},
		Data: data,
		Raw:  base.Raw,
	}
	if isEmptyData(data) {
		payload.Data = nil
	}
	return payload
}

// applyToolCall fills the tool_call (and, for MCP tools, mcp) feature blocks and
// returns a possibly-reclassified event type: a Claude "Skill" tool call is
// promoted to skill.activated the way the backend expects.
func applyToolCall(data *ingestData, base *agenthooks.Event, tool *agenthooks.ToolCall, eventType, permissionType string, output, errOut json.RawMessage, interrupt *bool, duration *float64) string {
	tc := &toolCallData{
		ID:             tool.ID,
		Name:           tool.Name,
		Input:          normalizeInput(tool.Input),
		Output:         output,
		Error:          errOut,
		IsInterrupt:    interrupt,
		PermissionType: permissionType,
		DurationMS:     duration,
		Status:         "",
	}
	data.ToolCall = tc

	if tool.MCP != nil {
		m := &mcpData{
			ServerName:     tool.MCP.Server,
			ServerIdentity: "",
			URL:            redactURL(tool.MCP.URL),
			Command:        redactCommand(tool.MCP.Command),
			ResultJSON:     "",
		}
		if m.URL == "" && m.Command != "" {
			m.ServerIdentity = m.Command
		} else {
			m.ServerIdentity = tool.MCP.Server
		}
		if len(output) > 0 {
			m.ResultJSON = string(output)
		}
		data.MCP = m
	}

	if base.Provider == agenthooks.ProviderClaudeCode && strings.EqualFold(tool.Name, "Skill") {
		if name := skillNameOf(tool.Input); name != "" {
			data.Skill = &skillData{Name: name, Source: ""}
			return "skill.activated"
		}
	}
	return eventType
}

// applyModelResponse lifts the assistant message text and token usage a
// model-response event carries into the canonical feature blocks. The
// normalized ModelEvent exposes only the envelope, so both live in the raw
// provider payload (cursor afterAgentResponse/afterAgentThought).
func applyModelResponse(data *ingestData, base *agenthooks.Event) {
	var raw struct {
		Text             string   `json:"text"`
		InputTokens      *int     `json:"input_tokens"`
		OutputTokens     *int     `json:"output_tokens"`
		CacheReadTokens  *int     `json:"cache_read_tokens"`
		CacheWriteTokens *int     `json:"cache_write_tokens"`
		Cost             *float64 `json:"cost"`
	}
	if json.Unmarshal(base.Raw, &raw) != nil {
		return
	}
	if raw.Text != "" {
		data.Message = &messageData{Text: raw.Text, Role: "assistant", DurationMS: nil}
	}
	if raw.InputTokens != nil || raw.OutputTokens != nil || raw.CacheReadTokens != nil ||
		raw.CacheWriteTokens != nil || raw.Cost != nil {
		data.Usage = &usageData{
			InputTokens:      raw.InputTokens,
			OutputTokens:     raw.OutputTokens,
			CacheReadTokens:  raw.CacheReadTokens,
			CacheWriteTokens: raw.CacheWriteTokens,
			Cost:             raw.Cost,
			LoopCount:        nil,
			Status:           "",
		}
	}
}

func sessionOf(base *agenthooks.Event) *ingestSession {
	s := &ingestSession{
		ID:     base.Session.ID,
		TurnID: base.Session.TurnID,
		CWD:    base.Session.CWD,
		Model:  base.Session.Model,
	}
	if s.ID == "" && s.TurnID == "" && s.CWD == "" && s.Model == "" {
		return nil
	}
	return s
}

func occurredAt(base *agenthooks.Event) string {
	if base.Time.IsZero() {
		return ""
	}
	return base.Time.UTC().Format(time.RFC3339Nano)
}

func permissionTypeOf(base *agenthooks.Event) string {
	if raw := base.RawField("permission_type"); len(raw) > 0 {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	return ""
}

func isInterrupt(base *agenthooks.Event) bool {
	if raw := base.RawField("is_interrupt"); len(raw) > 0 {
		var b bool
		if json.Unmarshal(raw, &b) == nil {
			return b
		}
	}
	return false
}

// normalizeInput returns a JSON object for the tool input, or nil to omit it.
func normalizeInput(in json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(in))
	if trimmed == "" || trimmed == "{}" || trimmed == "null" {
		return nil
	}
	return in
}

func normalizeOutput(out json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	return out
}

// toolErrorPayload builds the error feature value for a failed tool call,
// preferring the provider's structured output and falling back to the error
// string.
func toolErrorPayload(ev *agenthooks.ToolPostEvent) json.RawMessage {
	if out := normalizeOutput(ev.Output); out != nil {
		return out
	}
	if ev.Error != "" {
		b, err := json.Marshal(ev.Error)
		if err == nil {
			return b
		}
	}
	return nil
}

func skillNameOf(input json.RawMessage) string {
	var obj struct {
		Skill string `json:"skill"`
		Name  string `json:"name"`
	}
	if json.Unmarshal(input, &obj) == nil {
		if obj.Skill != "" {
			return obj.Skill
		}
		return obj.Name
	}
	return ""
}

func isEmptyData(d *ingestData) bool {
	return d.Prompt == nil && d.ToolCall == nil && d.MCP == nil && d.Usage == nil &&
		d.Message == nil && d.Skill == nil && d.Notification == nil
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}
