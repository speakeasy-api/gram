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
	elements, count := toolCallElements(toolCalls)
	ids := make([]string, 0, len(elements))
	for _, element := range elements {
		if id := toolCallID(element); id != "" {
			ids = append(ids, id)
		}
	}
	return metadata{
		toolCallCount:   conv.SafeInt32(count),
		contentBytes:    conv.SafeInt32(len(content)),
		contentRawBytes: conv.SafeInt32(len(contentRaw)),
		toolCallsBytes:  conv.SafeInt32(len(toolCalls)),
		toolCallIDs:     ids,
	}
}

// toolCallElements reads a tool_calls value and counts its calls the way the
// dashboard and the SQL totals do. An array counts its elements whatever their
// shape. Some rows hold the array double-encoded as a JSON string: a string
// that is empty, "[]" or "null" counts as none, one that holds an array counts
// its elements, and any other string counts as one call. Anything else counts
// as none. It never fails: metadata must not fail a message write.
func toolCallElements(raw []byte) ([]json.RawMessage, int) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, 0
	}
	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err == nil {
		return elements, len(elements)
	}
	var inner string
	if err := json.Unmarshal(raw, &inner); err != nil {
		return nil, 0
	}
	switch string(bytes.TrimSpace([]byte(inner))) {
	case "", "[]", "null":
		return nil, 0
	}
	if err := json.Unmarshal([]byte(inner), &elements); err == nil {
		return elements, len(elements)
	}
	return nil, 1
}

// toolCallID is the string ID of one tool call, or "" when the element is not
// an object with a string id. One malformed element never hides the IDs of its
// siblings.
func toolCallID(element json.RawMessage) string {
	var call struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(element, &call); err != nil {
		return ""
	}
	return call.ID
}
