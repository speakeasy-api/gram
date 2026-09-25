package mcp

import (
	"encoding/json"
	"fmt"
)

// Preserve the whole upstream declaration, including extension fields, for
// presentation and frozen fingerprints. A gateway changes only the tool name.
func (t *toolListEntry) UnmarshalJSON(raw []byte) error {
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
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(t.rawDefinition, &fields); err != nil {
		return nil, fmt.Errorf("decode full tool: %w", err)
	}
	name, err := json.Marshal(t.Name)
	if err != nil {
		return nil, fmt.Errorf("encode tool name: %w", err)
	}
	fields["name"] = name
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode full tool: %w", err)
	}
	return raw, nil
}
