package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
)

// Preserve the whole upstream declaration, including extension fields, for
// presentation and frozen fingerprints. A gateway changes only the tool name.
func (t *toolListEntry) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("tool definition must be an object")
	}
	type wire toolListEntry
	var decoded wire
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("decode tool: %w", err)
	}
	*t = toolListEntry(decoded)
	t.rawDefinition = append(json.RawMessage(nil), raw...)
	return nil
}
func (t toolListEntry) MarshalJSON() ([]byte, error) {
	type wire toolListEntry
	if len(t.rawDefinition) == 0 {
		raw, err := json.Marshal(wire(t))
		if err != nil {
			return nil, fmt.Errorf("encode tool: %w", err)
		}
		return raw, nil
	}
	raw, err := metamcp.MarshalToolDefinition(t.rawDefinition, t.Name)
	if err != nil {
		return nil, fmt.Errorf("encode full tool: %w", err)
	}
	return raw, nil
}
