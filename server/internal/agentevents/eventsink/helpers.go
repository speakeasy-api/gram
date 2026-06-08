package eventsink

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

func optionalString[T any](e agentevents.Event[T], field types.Field) string {
	value, ok, err := e.String(field)
	if err != nil || !ok {
		return ""
	}
	return value
}

func optionalIntPtr[T any](e agentevents.Event[T], field types.Field) *int {
	value, ok, err := e.Int(field)
	if err != nil || !ok || value <= 0 {
		return nil
	}
	return &value
}

// setStringAttr resolves a string field and records it on attrs only when the
// resolved value is non-empty.
func setStringAttr[T any](attrs map[attr.Key]any, e agentevents.Event[T], field types.Field, key attr.Key) {
	setStringValueAttr(attrs, optionalString(e, field), key)
}

func setStringValueAttr(attrs map[attr.Key]any, value string, key attr.Key) {
	if value != "" {
		attrs[key] = value
	}
}

// setIntAttr resolves an int field and records it on attrs only when the field
// is present.
func setIntAttr[T any](attrs map[attr.Key]any, e agentevents.Event[T], field types.Field, key attr.Key) {
	value := optionalIntPtr(e, field)
	if value != nil {
		attrs[key] = *value
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func marshalToJSON(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func buildToolCalls(toolCallID, toolName string, toolInput any) []map[string]any {
	return []map[string]any{{
		"id":   toolCallID,
		"type": "function",
		"function": map[string]any{
			"name":      toolName,
			"arguments": marshalToJSON(toolInput),
		},
	}}
}

// generateTraceID generates a W3C-compliant trace ID (32 hex characters).
func generateTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// generateSpanID generates a W3C-compliant span ID (16 hex characters).
func generateSpanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// hashToolCallIDToTraceID converts a tool call ID into a deterministic
// 32-hex-char trace ID so tool start/result events share a trace.
func hashToolCallIDToTraceID(toolCallID string) string {
	hash := sha256.Sum256([]byte(toolCallID))
	return hex.EncodeToString(hash[:16])
}
