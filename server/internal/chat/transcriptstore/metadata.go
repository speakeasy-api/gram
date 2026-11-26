package transcriptstore

import (
	"bytes"
	"encoding/json"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// metadata is the plaintext-free description of a message that capture stores
// next to its shadow, so readers can count, budget and look up tool calls
// without decrypting. It is derived from the same snapshot that is sealed, and
// the capture compare-and-set on source_hash keeps the two in step.
type metadata struct {
	toolCallCount   int32
	contentBytes    int32
	contentRawBytes int32
	toolCallsBytes  int32
	toolCallIDs     []string
}

// metadataOf measures a snapshot. JSON columns arrive in their jsonb text
// form, so their lengths match octet_length(column::text) in SQL.
func metadataOf(content string, contentRaw, toolCalls []byte) metadata {
	calls := toolCallsOf(toolCalls)
	ids := make([]string, 0, len(calls))
	for _, call := range calls {
		if id := toolCallID(call); id != "" {
			ids = append(ids, id)
		}
	}
	return metadata{
		toolCallCount:   conv.SafeInt32(toolCallCount(toolCalls, calls)),
		contentBytes:    conv.SafeInt32(len(content)),
		contentRawBytes: conv.SafeInt32(len(contentRaw)),
		toolCallsBytes:  conv.SafeInt32(len(toolCalls)),
		toolCallIDs:     ids,
	}
}

// toolCallsOf reads a tool_calls value as its array elements. Some rows hold
// the array double-encoded as a JSON string; both forms are read. Anything
// else yields no elements, never an error: metadata must not fail a message
// write.
func toolCallsOf(raw []byte) []json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err == nil {
		return elements
	}
	var inner string
	if err := json.Unmarshal(raw, &inner); err != nil {
		return nil
	}
	if err := json.Unmarshal([]byte(inner), &elements); err != nil {
		return nil
	}
	return elements
}

// toolCallID returns an element's "id", or "" when it is not an object with
// a string id. The key must be exactly "id", as the dashboard and SQL read it;
// encoding/json would also match "ID" or "Id" when decoding into a struct.
func toolCallID(element json.RawMessage) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(element, &fields); err != nil {
		return ""
	}
	var id string
	if err := json.Unmarshal(fields["id"], &id); err != nil {
		return ""
	}
	return id
}

// toolCallCount matches how the dashboard decides a message carries tool
// calls: an array counts its elements, whatever they are, and a
// double-encoded string that is not empty, "[]" or "null" but cannot be
// parsed counts as one.
func toolCallCount(raw []byte, calls []json.RawMessage) int {
	if len(calls) > 0 {
		return len(calls)
	}
	var inner string
	if err := json.Unmarshal(raw, &inner); err != nil {
		return 0
	}
	switch string(bytes.TrimSpace([]byte(inner))) {
	case "", "[]", "null":
		return 0
	}
	return 1
}
