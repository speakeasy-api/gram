package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// catalogChanges compares the approved definitions with the captured review.
type catalogChanges struct {
	// Added and Removed include definitions on their existing side only.
	Added   []catalogToolChange
	Removed []catalogToolChange
	// Changed includes the differing fields of tools present in both snapshots.
	Changed []catalogToolChange
}

type catalogToolChange struct {
	// Name is the qualified tool name shown above its changed fields.
	Name string
	// Fields contains only differing top-level definition fields, in name order.
	Fields []catalogFieldChange
}

type catalogFieldChange struct {
	// Name is the MCP definition field, such as description or inputSchema.
	Name string
	// Before and After are display text, escaped by html/template.
	Before string
	After  string
	// Presence distinguishes missing fields from explicit null or empty strings.
	HasBefore bool
	HasAfter  bool
}

func compareCatalogInventory(previous *connectionCatalog, inventory consentToolInventory) (catalogChanges, error) {
	changes := catalogChanges{Added: nil, Removed: nil, Changed: nil}
	old := make(map[string]*toolListEntry, len(previous.Tools))
	for _, tool := range previous.Tools {
		old[tool.Name] = tool
	}
	for _, tool := range inventory.Tools {
		if tool.Definition == nil {
			return changes, fmt.Errorf("review tool has no definition")
		}
		before, found := old[tool.Name]
		if !found || consentEvidenceHash(before) != tool.DefinitionHash {
			fields, err := compareToolDefinition(before, tool.Definition)
			if err != nil {
				return changes, err
			}
			change := catalogToolChange{Name: tool.Name, Fields: fields}
			if !found {
				changes.Added = append(changes.Added, change)
			} else if len(fields) > 0 {
				changes.Changed = append(changes.Changed, change)
			}
		}
		delete(old, tool.Name)
	}
	for name, tool := range old {
		fields, err := compareToolDefinition(tool, nil)
		if err != nil {
			return changes, err
		}
		changes.Removed = append(changes.Removed, catalogToolChange{Name: name, Fields: fields})
	}
	byName := func(a, b catalogToolChange) int { return strings.Compare(a.Name, b.Name) }
	slices.SortFunc(changes.Added, byName)
	slices.SortFunc(changes.Removed, byName)
	slices.SortFunc(changes.Changed, byName)
	return changes, nil
}

// Match the audit trail's structured diff: show changed fields, preserving full
// schema JSON and distinguishing an absent value from null or an empty string.
func compareToolDefinition(before, after *toolListEntry) ([]catalogFieldChange, error) {
	decode := func(tool *toolListEntry) (map[string]json.RawMessage, error) {
		if tool == nil {
			return nil, nil
		}
		raw, err := json.Marshal(tool)
		if err != nil {
			return nil, fmt.Errorf("encode tool definition: %w", err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("decode tool fields: %w", err)
		}
		delete(fields, "name") // Already shown in the tool heading.
		// Canonicalize schema objects without rounding JSON numbers. Whitespace
		// and object key order are not changes to a tool's contract.
		for key, value := range fields {
			decoder := json.NewDecoder(bytes.NewReader(value))
			decoder.UseNumber()
			var decoded any
			if err := decoder.Decode(&decoded); err != nil {
				return nil, fmt.Errorf("decode definition field: %w", err)
			}
			var canonical bytes.Buffer
			encoder := json.NewEncoder(&canonical)
			encoder.SetEscapeHTML(false)
			err := encoder.Encode(decoded)
			if err != nil {
				return nil, fmt.Errorf("encode definition field: %w", err)
			}
			fields[key] = bytes.TrimSpace(canonical.Bytes())
		}
		return fields, nil
	}
	old, err := decode(before)
	if err != nil {
		return nil, err
	}
	next, err := decode(after)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]struct{}, len(old)+len(next))
	for key := range old {
		keys[key] = struct{}{}
	}
	for key := range next {
		keys[key] = struct{}{}
	}
	var changes []catalogFieldChange
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		previous, hasBefore := old[key]
		current, hasAfter := next[key]
		if bytes.Equal(previous, current) {
			continue
		}
		changes = append(changes, catalogFieldChange{
			Name: key, Before: formatCatalogField(previous), After: formatCatalogField(current),
			HasBefore: hasBefore, HasAfter: hasAfter,
		})
	}
	return changes, nil
}

func formatCatalogField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil && string(raw) != "null" && text != "" {
		return text
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		return string(raw)
	}
	return pretty.String()
}
