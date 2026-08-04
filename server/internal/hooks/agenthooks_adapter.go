package hooks

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/speakeasy-api/agenthooks"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/policies"
)

// This file is the envelope -> typed-event adapter: it projects a validated
// hook.ingest.v1 payload onto the agenthooks typed events the policy router
// consumes. It is the exact reverse of the relay's buildEnvelope
// (hooks/relay/envelope.go) for the decision-relevant event types, so a
// policy written against the library sees the same normalized view on the
// server as it would at the edge.

// agenthooksTypedEvent maps the canonical payload to the agenthooks typed
// event for the decision-relevant canonical types only:
//
//   - prompt.submitted -> *agenthooks.PromptEvent
//   - tool.requested   -> *agenthooks.ToolPreEvent, or *agenthooks.PermissionEvent
//     when the tool call carries a permission_type (mirroring the relay, which
//     folds KindPermission into tool.requested + permission_type)
//
// Every other event type returns nil: those events are observe/persist-only
// today and never gate. Missing envelope fields stay zero-valued; policies
// needing more than the normalized projection read Event.Raw (the verbatim
// provider payload) or hold the payload itself.
//
// timestamp is the clamped canonicalEventTime the caller already computed, so
// the decision layer agrees with persistence on one time per event.
func agenthooksTypedEvent(payload *gen.IngestPayload, timestamp time.Time) any {
	switch strings.TrimSpace(payload.Event.Type) {
	case "prompt.submitted":
		return &agenthooks.PromptEvent{
			Event:  agenthooksBaseEvent(payload, agenthooks.KindPromptSubmitted, timestamp),
			Prompt: canonicalPromptText(payload),
		}
	case "tool.requested":
		tool := agenthooksToolCall(payload)
		kind := agenthooks.KindToolPre
		if canonicalPermissionType(payload) != "" {
			kind = agenthooks.KindPermission
		}
		base := agenthooksBaseEvent(payload, kind, timestamp)
		// Stamp ingest's MCP predicate on the event so the gates route
		// MCP-vs-plain exactly as the inline evaluation did. Deliberately
		// not Tool.MCP != nil: the library's matcher parses Gemini-style
		// mcp_server_tool names and server-only mcp__ prefixes the ingest
		// path has never treated as MCP, while Tool.MCP also absorbs the
		// envelope's mcp-block overlay — both serve other consumers, so the
		// ingest predicate rides as extension data instead.
		policies.StampMCPToolRequest(&base, canonicalIsMCPToolRequest(payload))
		if kind == agenthooks.KindPermission {
			return &agenthooks.PermissionEvent{Event: base, Tool: tool}
		}
		return &agenthooks.ToolPreEvent{Event: base, Tool: tool}
	default:
		return nil
	}
}

// agenthooksProvider maps the stable Gram adapter slug back onto the
// agenthooks Provider the relay derived it from — the reverse of the relay's
// adapterSlug. "claude" is the one slug that differs from its Provider value;
// every other slug ("cursor", "codex", "gemini", "opencode", "kimi-code", or
// a customer hook name) is the Provider string itself.
func agenthooksProvider(adapter string) agenthooks.Provider {
	slug := strings.TrimSpace(adapter)
	if slug == "claude" {
		return agenthooks.ProviderClaudeCode
	}
	return agenthooks.Provider(slug)
}

// agenthooksBaseEvent fills the shared envelope: adapter slug -> Provider,
// source.raw_event_name -> NativeName, session identity, the sender's
// self-reported user email, and the verbatim provider payload under Raw.
// Fields the envelope does not carry (Variant, Agent, DetectionConfidence,
// transcript path, permission mode, workspace roots) stay zero-valued.
func agenthooksBaseEvent(payload *gen.IngestPayload, kind agenthooks.EventKind, timestamp time.Time) agenthooks.Event {
	session := agenthooks.SessionInfo{
		ID:             "",
		TurnID:         "",
		CWD:            "",
		WorkspaceRoots: nil,
		TranscriptPath: "",
		Model:          "",
		PermissionMode: "",
		UserEmail:      canonicalSourceUserEmail(payload),
	}
	if payload.Session != nil {
		session.ID = strings.TrimSpace(conv.PtrValOr(payload.Session.ID, ""))
		session.TurnID = strings.TrimSpace(conv.PtrValOr(payload.Session.TurnID, ""))
		session.CWD = strings.TrimSpace(conv.PtrValOr(payload.Session.Cwd, ""))
		session.Model = strings.TrimSpace(conv.PtrValOr(payload.Session.Model, ""))
	}
	return agenthooks.Event{
		Provider:            agenthooksProvider(payload.Source.Adapter),
		Variant:             agenthooks.VariantUnknown,
		NativeName:          strings.TrimSpace(conv.PtrValOr(payload.Source.RawEventName, "")),
		Kind:                kind,
		Time:                timestamp,
		Session:             session,
		Agent:               nil,
		DetectionConfidence: "",
		Backfilled:          false,
		Raw:                 agenthooksRaw(payload),
		Ext:                 nil,
	}
}

// agenthooksRaw re-encodes the envelope's raw block — the verbatim provider
// payload, decoded into any by the transport — back to JSON for Event.Raw.
// Key order may normalize during the round-trip; all values stay intact. A
// missing raw block yields nil, matching a library event whose payload was
// never captured.
func agenthooksRaw(payload *gen.IngestPayload) json.RawMessage {
	if payload.Raw == nil {
		return nil
	}
	switch raw := payload.Raw.(type) {
	case json.RawMessage:
		return raw
	case []byte:
		return json.RawMessage(raw)
	default:
		b, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		return b
	}
}

// agenthooksToolCall builds the normalized ToolCall from the envelope's
// tool_call and mcp feature blocks. It replicates the population of the
// library's unexported makeToolCall — Input always a JSON object, MCP decoded
// from the tool-name dialects, Canonical via CanonicalToolFor, missing ids
// synthesized — then overlays the mcp block's transport identity (server
// name, url, command), which the relay resolved client-side and which name
// parsing alone cannot recover.
func agenthooksToolCall(payload *gen.IngestPayload) agenthooks.ToolCall {
	name := canonicalToolName(payload)
	rawInput := agenthooksJSONValue(canonicalToolInput(payload))
	tc := agenthooks.ToolCall{
		ID:          canonicalToolCallID(payload),
		Synthesized: false,
		Name:        name,
		Canonical:   agenthooks.CanonicalToolFor(name),
		MCP:         agenthooks.ParseMCPName(name),
		Input:       agenthooksObjectInput(rawInput),
		RawInput:    rawInput,
	}
	if tc.ID == "" {
		session := agenthooks.SessionInfo{
			ID:             "",
			TurnID:         "",
			CWD:            "",
			WorkspaceRoots: nil,
			TranscriptPath: "",
			Model:          "",
			PermissionMode: "",
			UserEmail:      "",
		}
		if payload.Session != nil {
			session.ID = strings.TrimSpace(conv.PtrValOr(payload.Session.ID, ""))
			session.TurnID = strings.TrimSpace(conv.PtrValOr(payload.Session.TurnID, ""))
		}
		tc.ID = agenthooks.SynthesizeToolID(session.ID, session.TurnID, name, tc.Input)
		tc.Synthesized = true
	}
	if mcp := canonicalMCPData(payload); mcp != nil {
		if tc.MCP == nil {
			// The name is not MCP-shaped but the envelope says the call
			// targets an MCP tool (e.g. a custom adapter sending bare tool
			// names): the bare name is the tool as the server knows it.
			tc.MCP = &agenthooks.MCPCall{
				Server:     "",
				Tool:       name,
				URL:        "",
				Command:    "",
				FromConfig: false,
			}
		}
		if server := strings.TrimSpace(conv.PtrValOr(mcp.ServerName, "")); server != "" {
			tc.MCP.Server = server
		}
		tc.MCP.URL = strings.TrimSpace(conv.PtrValOr(mcp.URL, ""))
		tc.MCP.Command = strings.TrimSpace(conv.PtrValOr(mcp.Command, ""))
	}
	if tc.MCP != nil {
		tc.Canonical = agenthooks.ToolMCP
	}
	return tc
}

// agenthooksJSONValue re-encodes a transport-decoded JSON value to raw JSON.
// A JSON-string input (Cursor's stringified form) round-trips back to its
// JSON-string encoding, so agenthooksObjectInput un-stringifies it exactly
// like the library does at the edge. nil (field absent) stays nil so
// RawInput keeps its "provider sent none" meaning.
func agenthooksJSONValue(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case json.RawMessage:
		return t
	case []byte:
		return json.RawMessage(t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return nil
		}
		return b
	}
}

// agenthooksObjectInput replicates the library's unexported normalizeInput so
// ToolCall.Input keeps its invariant of always being a JSON object: empty and
// null map to {}, a JSON string whose content is an object un-stringifies,
// and any other non-object value wraps as {"value": ...}.
func agenthooksObjectInput(in json.RawMessage) json.RawMessage {
	trim := bytes.TrimSpace(in)
	if len(trim) == 0 || bytes.Equal(trim, []byte("null")) {
		return json.RawMessage("{}")
	}
	if trim[0] == '"' {
		var s string
		if json.Unmarshal(trim, &s) == nil {
			inner := bytes.TrimSpace([]byte(s))
			if len(inner) > 0 && inner[0] == '{' && json.Valid(inner) {
				return json.RawMessage(inner)
			}
		}
	}
	if trim[0] == '{' && json.Valid(trim) {
		return trim
	}
	wrapped, err := json.Marshal(map[string]json.RawMessage{"value": trim})
	if err != nil {
		return json.RawMessage("{}")
	}
	return wrapped
}
