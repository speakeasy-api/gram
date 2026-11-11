package relay

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

const (
	schemaVersion   = "hook.ingest.v1"
	agentTurnPrefix = "agent-turn:v1:"
)

// adapterSlug maps an agenthooks provider onto the stable Speakeasy adapter slug the
// backend expects (it keys its provider-style telemetry vocabulary on these).
func adapterSlug(p agenthooks.Provider) string {
	switch p {
	case agenthooks.ProviderClaudeCode:
		return "claude"
	default:
		return string(p)
	}
}

// canonicalEventType resolves the Speakeasy canonical event.type from the unified
// event kind. Two cursor quirks the unified taxonomy cannot express yet are
// split inline: its stop hook reports a usage summary rather than a final
// message, and it is the only provider with a distinct thought stream on the
// model-response kind (ModelEvent carries no discriminator, so the split
// reads the native name).
func canonicalEventType(e *agenthooks.Event) components.Type {
	switch e.Kind {
	case agenthooks.KindSessionStart:
		return components.TypeSessionStarted
	case agenthooks.KindSessionEnd:
		return components.TypeSessionEnded
	case agenthooks.KindMCPInventory:
		return components.TypeMcpInventory
	case agenthooks.KindPromptSubmitted:
		return components.TypePromptSubmitted
	case agenthooks.KindToolPre, agenthooks.KindPermission:
		return components.TypeToolRequested
	case agenthooks.KindToolPost:
		return components.TypeToolCompleted
	case agenthooks.KindToolError:
		return components.TypeToolFailed
	case agenthooks.KindStop:
		if e.Provider == agenthooks.ProviderCursor {
			return components.TypeUsageReported
		}
		return components.TypeAssistantResponded
	case agenthooks.KindSubagentStop:
		return components.TypeAssistantResponded
	case agenthooks.KindModelResponse:
		if e.NativeName == "afterAgentThought" {
			return components.TypeAssistantThought
		}
		return components.TypeAssistantResponded
	case agenthooks.KindNotification:
		return components.TypeNotificationReported
	default:
		return components.TypeSessionUpdated
	}
}

// buildEnvelope projects a normalized agenthooks event onto the canonical Speakeasy
// ingest payload. Feature blocks are populated only for the event's kind; the
// verbatim provider payload rides under raw for debugging (the backend never
// reads it for behavior).
func buildEnvelope(typed any, hostname string) components.IngestRequestBody {
	base := agenthooks.EventOf(typed)
	eventType := canonicalEventType(base)
	data := &components.HookIngestData{
		Mcp:                   nil,
		McpAttribution:        nil,
		McpInventory:          nil,
		McpInventoryCollected: nil,
		Message:               nil,
		Notification:          nil,
		Prompt:                nil,
		PromptAttachments:     nil,
		Skill:                 nil,
		ToolCall:              nil,
		Usage:                 nil,
	}

	switch ev := typed.(type) {
	case *agenthooks.MCPInventoryEvent:
		attachMCPInventory(data, ev.Servers, ev.Complete)
	case *agenthooks.PromptEvent:
		if ev.Prompt != "" {
			data.Prompt = &components.HookPromptData{Text: new(ev.Prompt)}
		}
		if base.Provider == agenthooks.ProviderCodex {
			if name := codexPromptSkillName(ev.Prompt, base.Session.CWD); name != "" {
				data.Skill = &components.HookSkillData{Name: name, Source: nil}
			}
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
			data.Message = &components.HookMessageData{Text: new(ev.FinalMessage), Role: new("assistant"), DurationMs: nil}
		}
		if ev.Usage != nil {
			data.Usage = &components.HookUsageData{
				InputTokens:      int64Ptr(ev.Usage.InputTokens),
				OutputTokens:     int64Ptr(ev.Usage.OutputTokens),
				CacheReadTokens:  int64Ptr(ev.Usage.CacheReadTokens),
				CacheWriteTokens: int64Ptr(ev.Usage.CacheWriteTokens),
				Cost:             ev.Usage.Cost,
				LoopCount:        int64Ptr(ev.Usage.LoopCount),
				Status:           optStr(ev.Usage.Status),
			}
		}
	case *agenthooks.NotificationEvent:
		if ev.Message != "" {
			data.Notification = &components.HookNotificationData{Type: nil, Title: nil, Message: new(ev.Message)}
		}
	case *agenthooks.ModelEvent:
		applyModelResponse(data, base)
	}
	// The server records an activation for every event carrying data.skill, so
	// an implicit (inferred) activation is attached only to the completed
	// event: its tool output carries the manifest the content registry hashes,
	// and marking the pre event too would double-count. Explicit Skill tool
	// calls keep the provider's own event classification on both sides.
	if activation := agenthooks.SkillActivationOf(typed); activation != nil && (activation.Explicit || base.Kind == agenthooks.KindToolPost) {
		data.Skill = &components.HookSkillData{Name: activation.Name, Source: nil}
		if activation.Explicit {
			eventType = components.TypeSkillActivated
		}
	}

	payload := components.IngestRequestBody{
		SchemaVersion: schemaVersion,
		Source: components.HookIngestSource{
			Adapter: adapterSlug(base.Provider),
			// Doubles as the relay's capability marker: releases before this
			// one left the field unset, so the server reads its absence as
			// "this client cannot report MCP inventory" and degrades the
			// codex meta-tool guard rather than denying traffic that works
			// today (DNO-767).
			AdapterVersion: optStr(BinaryVersion),
			RawEventName:   optStr(base.NativeName),
			Hostname:       optStr(hostname),
			UserEmail:      nil,
		},
		Session: sessionOf(base),
		Event: components.HookIngestEvent{
			Type:       eventType,
			OccurredAt: occurredAt(base),
		},
		Data: data,
		Raw:  nil,
	}
	if len(base.Raw) > 0 {
		payload.Raw = scrubRawPayload(base.Raw, base.Provider)
	}
	if isEmptyData(data) {
		payload.Data = nil
	}
	return payload
}

// scrubRawPayload rewrites the secret-bearing top-level keys of the provider
// payload before it is echoed under raw (a debugging aid the backend never
// reads): url/mcp_server_url/command can carry credentials, and redacting
// data.mcp alone would leave the same values in the echo. It runs on every
// event like the bash senders' scrub did. Payloads that need no rewrite are
// returned verbatim; a rewritten payload is re-encoded, so its key order and
// whitespace may normalize while all other values stay intact. The OpenClaw
// shim's mcpServers list is dropped.
func scrubRawPayload(raw json.RawMessage, provider agenthooks.Provider) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	changed := false
	redactField := func(key string, redact func(string) string) {
		v, ok := obj[key]
		if !ok {
			return
		}
		var s string
		if json.Unmarshal(v, &s) != nil || s == "" {
			return
		}
		r := redact(s)
		if r == s {
			return
		}
		b, err := json.Marshal(r)
		if err != nil {
			return
		}
		obj[key] = b
		changed = true
	}
	for _, key := range []string{"url", "server_url", "mcp_server_url"} {
		redactField(key, redactURL)
	}
	redactField("command", redactCommand)
	// The OpenClaw shim lists every configured MCP server on each tool frame.
	// data.mcp already carries the one the call reached, so the echo does not
	// repeat the whole list. Other providers' payloads keep the field.
	if _, ok := obj[openclawMCPServersField]; ok && provider == agenthooks.ProviderOpenClaw {
		delete(obj, openclawMCPServersField)
		changed = true
	}
	if !changed {
		return raw
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return b
}

// applyToolCall fills the tool_call (and, for MCP tools, mcp) feature blocks and
// returns a possibly-reclassified event type: a Claude "Skill" tool call is
// promoted to skill.activated the way the backend expects.
func applyToolCall(data *components.HookIngestData, base *agenthooks.Event, tool *agenthooks.ToolCall, eventType components.Type, permissionType string, output, errOut json.RawMessage, interrupt *bool, duration *float64) components.Type {
	tc := &components.HookToolCallData{
		ID:             optStr(tool.ID),
		Name:           optStr(tool.Name),
		Input:          nil,
		Output:         nil,
		Error:          nil,
		IsInterrupt:    interrupt,
		PermissionType: optStr(permissionType),
		DurationMs:     duration,
		Status:         nil,
	}
	if in := normalizeInput(tool.Input); in != nil {
		tc.Input = in
	}
	if len(output) > 0 {
		tc.Output = output
	}
	if len(errOut) > 0 {
		tc.Error = errOut
	}
	data.ToolCall = tc

	mcp := tool.MCP
	if mcp == nil && base.Provider == agenthooks.ProviderOpenClaw {
		mcp = openclawResolveMCPCall(base, tool.Name, output, errOut)
	}
	if mcp != nil {
		m := &components.HookMCPData{
			ServerName:     optStr(mcp.Server),
			ServerIdentity: nil,
			URL:            optStr(redactURL(mcp.URL)),
			Command:        optStr(redactCommand(mcp.Command)),
			ResultJSON:     nil,
		}
		if m.URL == nil && m.Command != nil {
			m.ServerIdentity = m.Command
		} else {
			m.ServerIdentity = optStr(mcp.Server)
		}
		if len(output) > 0 {
			m.ResultJSON = new(string(output))
		}
		data.Mcp = m
	}

	return eventType
}

// openClawMCPCall recovers the MCP identity of an OpenClaw tool call from its
// after_tool_call result. OpenClaw names MCP tools <server>__<tool> rather
// than mcp__<server>__<tool>, so the tool name cannot be decoded without
// guessing, but OpenClaw's MCP bridge stamps details.mcpServer and
// details.mcpTool on every MCP tool result and native tools never carry them.
// The pre-call event has only the tool name and params, so it carries no
// marker. Results are tried in order and the first one naming a server wins;
// nil means the result names no MCP server.
func openClawMCPCall(results ...json.RawMessage) *agenthooks.MCPCall {
	for _, raw := range results {
		var result struct {
			Details struct {
				MCPServer string `json:"mcpServer"`
				MCPTool   string `json:"mcpTool"`
			} `json:"details"`
		}
		if json.Unmarshal(raw, &result) != nil {
			continue
		}
		server := strings.TrimSpace(result.Details.MCPServer)
		if server == "" {
			continue
		}
		return &agenthooks.MCPCall{
			Server:     server,
			Tool:       strings.TrimSpace(result.Details.MCPTool),
			URL:        "",
			Command:    "",
			FromConfig: false,
		}
	}
	return nil
}

// applyModelResponse lifts the assistant message text and token usage a
// model-response event carries into the canonical feature blocks. The
// normalized ModelEvent exposes only the envelope, so both live in the raw
// provider payload (cursor afterAgentResponse/afterAgentThought).
func applyModelResponse(data *components.HookIngestData, base *agenthooks.Event) {
	var raw struct {
		Text             string   `json:"text"`
		InputTokens      *int64   `json:"input_tokens"`
		OutputTokens     *int64   `json:"output_tokens"`
		CacheReadTokens  *int64   `json:"cache_read_tokens"`
		CacheWriteTokens *int64   `json:"cache_write_tokens"`
		Cost             *float64 `json:"cost"`
	}
	if json.Unmarshal(base.Raw, &raw) != nil {
		return
	}
	if raw.Text != "" {
		data.Message = &components.HookMessageData{Text: new(raw.Text), Role: new("assistant"), DurationMs: nil}
	}
	if raw.InputTokens != nil || raw.OutputTokens != nil || raw.CacheReadTokens != nil ||
		raw.CacheWriteTokens != nil || raw.Cost != nil {
		data.Usage = &components.HookUsageData{
			InputTokens:      raw.InputTokens,
			OutputTokens:     raw.OutputTokens,
			CacheReadTokens:  raw.CacheReadTokens,
			CacheWriteTokens: raw.CacheWriteTokens,
			Cost:             raw.Cost,
			LoopCount:        nil,
			Status:           nil,
		}
	}
}

func sessionOf(base *agenthooks.Event) *components.HookIngestSession {
	turnID := base.Session.TurnID
	if turnID == "" && base.Provider == agenthooks.ProviderOpenCode && base.Kind == agenthooks.KindPromptSubmitted {
		for _, path := range []string{"output.message.id", "input.messageID"} {
			var messageID string
			if raw := base.RawField(path); len(raw) > 0 && json.Unmarshal(raw, &messageID) == nil && strings.TrimSpace(messageID) != "" {
				turnID = strings.TrimSpace(messageID)
				break
			}
		}
	}
	if turnID != "" && (base.Provider == agenthooks.ProviderCodex || base.Provider == agenthooks.ProviderOpenCode) {
		turnID = agentTurnPrefix + adapterSlug(base.Provider) + ":" + turnID
	}
	s := &components.HookIngestSession{
		ID:                optStr(base.Session.ID),
		TurnID:            optStr(turnID),
		Cwd:               optStr(base.Session.CWD),
		Model:             optStr(base.Session.Model),
		TranscriptCapture: nil,
	}
	// Inside the Cowork VM the Stop hook also ships the session transcript.
	// The flag tells the server to key this event's row by the id the
	// transcript shares, so the two never store a message twice.
	if coworkTranscriptMode(base) {
		s.TranscriptCapture = new(true)
	}
	if s.ID == nil && s.TurnID == nil && s.Cwd == nil && s.Model == nil && s.TranscriptCapture == nil {
		return nil
	}
	return s
}

func occurredAt(base *agenthooks.Event) *time.Time {
	if base.Time.IsZero() {
		return nil
	}
	t := base.Time.UTC()
	return &t
}

func permissionTypeOf(base *agenthooks.Event) string {
	if raw := base.RawField("permission_type"); len(raw) > 0 {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	// OpenCode's permission frame carries the kind under "type".
	if base.Provider == agenthooks.ProviderOpenCode {
		if raw := base.RawField("input.type"); len(raw) > 0 {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				return s
			}
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

func isEmptyData(d *components.HookIngestData) bool {
	return d.Prompt == nil && d.ToolCall == nil && d.Mcp == nil && d.Usage == nil &&
		d.Message == nil && d.Skill == nil && d.Notification == nil &&
		(d.McpInventoryCollected == nil || !*d.McpInventoryCollected) &&
		len(d.McpAttribution) == 0 && d.McpInventory == nil &&
		len(d.PromptAttachments) == 0
}

// optStr returns a pointer to s, or nil when s is empty so the field is
// omitted from the wire payload like the legacy senders omitted empty strings.
func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return new(s)
}

func int64Ptr(v *int) *int64 {
	if v == nil {
		return nil
	}
	return new(int64(*v))
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}
