package policies

import (
	"github.com/speakeasy-api/agenthooks"
)

// extKeyMCPToolRequest is the Event.Ext key carrying ingest's MCP predicate
// (canonicalIsMCPToolRequest). Stages read it through mcpToolRequest so the
// key never leaks past this file.
const extKeyMCPToolRequest = "gram.mcp_tool_request"

// StampMCPToolRequest records ingest's MCP predicate on the event envelope's
// extension carrier. The adapter that projects the canonical payload onto
// the typed event calls it on every tool-carrying dispatch path (tool.pre
// and permission.request), so each gate — including the deliberate
// duplicate-scan permission gate — reads the one value Ingest computed.
func StampMCPToolRequest(env *agenthooks.Event, isMCP bool) {
	if env.Ext == nil {
		env.Ext = map[string]any{}
	}
	env.Ext[extKeyMCPToolRequest] = isMCP
}

// mcpToolRequest reports the MCP predicate stamped by StampMCPToolRequest.
// An unstamped event (not dispatched through the ingest adapter) reads
// false, keeping the MCP-gated stages neutral.
func mcpToolRequest(env *agenthooks.Event) bool {
	isMCP, _ := env.Ext[extKeyMCPToolRequest].(bool)
	return isMCP
}

// toolInputOf converts the event's RawInput projection back into the `any`
// the enforcement primitives take. The adapter builds RawInput as the JSON
// encoding of the exact canonicalToolInput value the inline evaluation
// passed around, and every live consumer JSON-encodes that value again
// (marshalToJSON in the scan paths: nil -> "", else json.Marshal). Handing
// the encoded bytes on as a json.RawMessage is therefore byte-identical:
// json.Marshal of a json.RawMessage re-emits the same bytes, and an absent
// input maps back to nil so the "no input" guards keep firing.
func toolInputOf(tool agenthooks.ToolCall) any {
	if len(tool.RawInput) == 0 {
		return nil
	}
	return tool.RawInput
}
