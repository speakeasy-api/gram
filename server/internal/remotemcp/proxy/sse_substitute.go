package proxy

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// notificationsMessageMethod is the JSON-RPC method name for MCP's logging
// utility — see MCP 2025-06-18 § utilities/logging. The proxy uses it to
// surface rejected upstream notifications to the user as level=error log
// notifications, since notifications themselves carry no id and cannot be
// substituted with a JSON-RPC error response.
const notificationsMessageMethod = "notifications/message"

// substituteRejectedSSEEvent builds the SSE-framed wire bytes the proxy
// should emit in place of an upstream message that an interceptor rejected.
// The substitute shape depends on the rejected message's role in the
// JSON-RPC protocol:
//
//   - A response (carries an id correlating to a prior request) is replaced
//     by a JSON-RPC error response carrying the same id, so the user's MCP
//     runtime can match it against its outstanding call and surface the
//     error cleanly.
//
//   - A server-initiated request (also has an id) is replaced by a JSON-RPC
//     error response carrying the request's id. The user's MCP runtime
//     receives an error in place of the original request. Note the
//     upstream is not informed that the proxy ate its request; that's an
//     acknowledged limitation of one-way relay.
//
//   - A notification (no id) is replaced by a "notifications/message" log
//     notification at level "error", carrying the rejection reason. This
//     is spec-aligned per MCP § utilities/logging and surfaces in clients
//     that wire log notifications through.
//
// Returns an error only if marshaling the substitute fails — a soft "we
// couldn't even build a substitute" signal the caller can log and fall
// back to silently dropping the event.
func substituteRejectedSSEEvent(msg jsonrpc.Message, reject *RejectError) ([]byte, error) {
	payload, err := substituteRejectedJSONRPCPayload(msg, reject)
	if err != nil {
		return nil, err
	}
	return formatSSEDataEvent(payload), nil
}

// substituteRejectedJSONRPCPayload builds the JSON-RPC payload bytes that
// will become the substitute event's "data:" field. Exposed as its own
// helper to keep the SSE-framing concern separate from the message-shape
// concern; tested independently.
func substituteRejectedJSONRPCPayload(msg jsonrpc.Message, reject *RejectError) ([]byte, error) {
	if reject == nil {
		reject = &RejectError{Code: RejectCodeInternalError, Message: "rejected", Data: nil}
	}

	switch m := msg.(type) {
	case *jsonrpc.Request:
		// jsonrpc.Request covers both server-initiated requests (id set)
		// and notifications (id unset). MCP's spec-aligned substitution
		// for notifications uses the logging notification rather than a
		// JSON-RPC error response — notifications carry no id and have
		// no correlated reply slot.
		if m.ID.IsValid() {
			return marshalErrorResponse(m.ID, reject)
		}
		return marshalLogNotification(m.Method, reject)
	case *jsonrpc.Response:
		return marshalErrorResponse(m.ID, reject)
	default:
		// jsonrpc.DecodeMessage only ever produces *jsonrpc.Request or
		// *jsonrpc.Response, so this branch is unreachable today. If the
		// SDK adds a new Message implementation in the future we'd
		// rather fail loudly than silently invent a substitute.
		return nil, fmt.Errorf("substitute event: unexpected message type %T", msg)
	}
}

// marshalErrorResponse produces a JSON-RPC 2.0 error response with the
// given id and error payload. When id is invalid (e.g. rejecting an
// inbound notification, which carries no id), the "id" field is omitted
// per the MCP Streamable HTTP spec for rejected notifications.
func marshalErrorResponse(id jsonrpc.ID, reject *RejectError) ([]byte, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    reject.Code,
			"message": reject.Message,
			"data":    reject.Data,
		},
	}
	if id.IsValid() {
		payload["id"] = id.Raw()
	}
	bs, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonrpc error response: %w", err)
	}
	return bs, nil
}

// marshalLogNotification produces a "notifications/message" log
// notification carrying the rejection reason. originalMethod is included
// in the data payload so observers can see which message was suppressed.
func marshalLogNotification(originalMethod string, reject *RejectError) ([]byte, error) {
	data := map[string]any{
		"reason": reject.Message,
	}
	if originalMethod != "" {
		data["rejected_method"] = originalMethod
	}
	if reject.Data != nil {
		data["details"] = reject.Data
	}

	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  notificationsMessageMethod,
		"params": map[string]any{
			"level":  "error",
			"logger": "gram.remotemcp.proxy",
			"data":   data,
		},
	}
	bs, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonrpc log notification: %w", err)
	}
	return bs, nil
}

// formatSSEDataEvent wraps a JSON-RPC payload in the minimal SSE event
// framing — a single "data:" field followed by the mandatory blank-line
// separator. Multi-line payloads are emitted as a single "data:" line
// because JSON encodes newlines as "\n" escape sequences within strings,
// so the marshaled bytes never contain literal newlines that would
// require multiple "data:" lines.
func formatSSEDataEvent(payload []byte) []byte {
	out := make([]byte, 0, len(payload)+8)
	out = append(out, "data: "...)
	out = append(out, payload...)
	out = append(out, '\n', '\n')
	return out
}
