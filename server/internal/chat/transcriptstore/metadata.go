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
		if call.ID != "" {
			ids = append(ids, call.ID)
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

type toolCall struct {
	ID string `json:"id"`
}

// toolCallsOf reads a tool_calls value. Some rows hold the array
// double-encoded as a JSON string; both forms are read. Anything else yields
// no calls, never an error: metadata must not fail a message write.
func toolCallsOf(raw []byte) []toolCall {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var calls []toolCall
	if err := json.Unmarshal(raw, &calls); err == nil {
		return calls
	}
	var inner string
	if err := json.Unmarshal(raw, &inner); err != nil {
		return nil
	}
	if err := json.Unmarshal([]byte(inner), &calls); err != nil {
		return nil
	}
	return calls
}

// toolCallCount matches how the dashboard decides a message carries tool
// calls: a non-empty array counts its elements, and a double-encoded string
// that is not empty, "[]" or "null" but cannot be parsed counts as one.
func toolCallCount(raw []byte, calls []toolCall) int {
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
	var parsed []json.RawMessage
	if err := json.Unmarshal([]byte(inner), &parsed); err == nil {
		return len(parsed)
	}
	return 1
}
