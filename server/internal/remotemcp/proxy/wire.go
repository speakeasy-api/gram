package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
)

// These wire types preserve members that Gram does not model, allowing newer MCP
// payloads to survive mutations. Duplicate and case-folded names are collapsed so
// Gram and downstream parsers cannot disagree about authorized values. Messages
// that are not mutated bypass these types and relay byte-for-byte.
func extrasOnly(members object, modeled ...string) object {
	extras := make(object, len(members))
	maps.Copy(extras, members)

	for _, name := range modeled {
		delete(extras, name)
		for carried := range extras {
			if strings.EqualFold(carried, name) {
				delete(extras, carried)
			}
		}
	}

	return extras
}

func mergeModeled(extras object, modeled object) (json.RawMessage, error) {
	names := make([]string, 0, len(modeled))
	for name := range modeled {
		names = append(names, name)
	}

	out := extrasOnly(extras, names...)
	maps.Copy(out, modeled)

	return out.encode()
}

// ToolAnnotations contains the hints Gram evaluates and preserves unknown members.
type ToolAnnotations struct {
	ReadOnlyHint    *bool
	DestructiveHint *bool
	IdempotentHint  *bool
	OpenWorldHint   *bool

	extras object
}

var annotationMembers = []string{"readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint"}

// UnmarshalJSON implements [json.Unmarshaler].
func (a *ToolAnnotations) UnmarshalJSON(data []byte) error {
	members, err := decodeObject(data)
	if err != nil {
		return fmt.Errorf("decode tool annotations: %w", err)
	}

	var next ToolAnnotations
	for name, target := range map[string]**bool{
		"readOnlyHint":    &next.ReadOnlyHint,
		"destructiveHint": &next.DestructiveHint,
		"idempotentHint":  &next.IdempotentHint,
		"openWorldHint":   &next.OpenWorldHint,
	} {
		raw, ok := members[name]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return fmt.Errorf("decode annotation %q: %w", name, err)
		}
	}
	next.extras = extrasOnly(members, annotationMembers...)

	*a = next
	return nil
}

// MarshalJSON implements [json.Marshaler].
func (a ToolAnnotations) MarshalJSON() ([]byte, error) {
	modeled := object{}
	for name, value := range map[string]*bool{
		"readOnlyHint":    a.ReadOnlyHint,
		"destructiveHint": a.DestructiveHint,
		"idempotentHint":  a.IdempotentHint,
		"openWorldHint":   a.OpenWorldHint,
	} {
		if value == nil {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode annotation %q: %w", name, err)
		}
		modeled[name] = encoded
	}

	return mergeModeled(a.extras, modeled)
}

// Tool is a tools/list entry that preserves members Gram does not model.
type Tool struct {
	// Name identifies the tool for authorization.
	Name string

	// InputSchema preserves the peer's raw schema encoding.
	InputSchema json.RawMessage

	// Annotations is nil when the tool declared none.
	Annotations *ToolAnnotations

	extras object
}

var toolMembers = []string{"name", "inputSchema", "annotations"}

// UnmarshalJSON implements [json.Unmarshaler].
func (t *Tool) UnmarshalJSON(data []byte) error {
	members, err := decodeObject(data)
	if err != nil {
		return fmt.Errorf("decode tool: %w", err)
	}

	var next Tool
	if raw, ok := members["name"]; ok {
		if err := json.Unmarshal(raw, &next.Name); err != nil {
			return fmt.Errorf("decode tool name: %w", err)
		}
	}
	if raw, ok := members["inputSchema"]; ok {
		next.InputSchema = raw
	}
	if raw, ok := members["annotations"]; ok {
		if err := json.Unmarshal(raw, &next.Annotations); err != nil {
			return fmt.Errorf("decode tool annotations: %w", err)
		}
	}
	next.extras = extrasOnly(members, toolMembers...)

	*t = next
	return nil
}

// MarshalJSON implements [json.Marshaler].
func (t Tool) MarshalJSON() ([]byte, error) {
	name, err := json.Marshal(t.Name)
	if err != nil {
		return nil, fmt.Errorf("encode tool name: %w", err)
	}

	modeled := object{"name": name}
	if len(t.InputSchema) > 0 {
		modeled["inputSchema"] = t.InputSchema
	}
	if t.Annotations != nil {
		encoded, err := json.Marshal(*t.Annotations)
		if err != nil {
			return nil, fmt.Errorf("encode tool annotations: %w", err)
		}
		modeled["annotations"] = encoded
	}

	return mergeModeled(t.extras, modeled)
}

// ToolsListResult preserves unmodeled members of a tools/list result.
type ToolsListResult struct {
	Tools      []*Tool
	NextCursor string

	extras object
}

// UnmarshalJSON implements [json.Unmarshaler].
func (r *ToolsListResult) UnmarshalJSON(data []byte) error {
	members, err := decodeObject(data)
	if err != nil {
		return fmt.Errorf("decode tools/list result: %w", err)
	}

	var next ToolsListResult
	if raw, ok := members["tools"]; ok {
		// Reject null so mutation cannot silently normalize it to [].
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("tools is null, not an array")
		}
		if err := json.Unmarshal(raw, &next.Tools); err != nil {
			return fmt.Errorf("decode tools: %w", err)
		}
	}
	// Reject null entries before authorization code dereferences them.
	for idx, tool := range next.Tools {
		if tool == nil {
			return fmt.Errorf("tool %d is null", idx)
		}
	}
	if raw, ok := members["nextCursor"]; ok {
		if err := json.Unmarshal(raw, &next.NextCursor); err != nil {
			return fmt.Errorf("decode nextCursor: %w", err)
		}
	}
	next.extras = extrasOnly(members, "tools", "nextCursor")

	*r = next
	return nil
}

// MarshalJSON implements [json.Marshaler].
func (r ToolsListResult) MarshalJSON() ([]byte, error) {
	tools := r.Tools
	if tools == nil {
		// MCP requires an array.
		tools = []*Tool{}
	}
	encodedTools, err := json.Marshal(tools)
	if err != nil {
		return nil, fmt.Errorf("encode tools: %w", err)
	}

	modeled := object{"tools": encodedTools}
	if r.NextCursor != "" {
		cursor, err := json.Marshal(r.NextCursor)
		if err != nil {
			return nil, fmt.Errorf("encode nextCursor: %w", err)
		}
		modeled["nextCursor"] = cursor
	}

	return mergeModeled(r.extras, modeled)
}

// Resource is a resources/list entry that preserves members Gram does not model.
type Resource struct {
	URI  string
	Name string

	extras object
}

// UnmarshalJSON implements [json.Unmarshaler].
func (r *Resource) UnmarshalJSON(data []byte) error {
	members, err := decodeObject(data)
	if err != nil {
		return fmt.Errorf("decode resource: %w", err)
	}

	var next Resource
	if raw, ok := members["uri"]; ok {
		if err := json.Unmarshal(raw, &next.URI); err != nil {
			return fmt.Errorf("decode resource uri: %w", err)
		}
	}
	if raw, ok := members["name"]; ok {
		if err := json.Unmarshal(raw, &next.Name); err != nil {
			return fmt.Errorf("decode resource name: %w", err)
		}
	}
	next.extras = extrasOnly(members, "uri", "name")

	*r = next
	return nil
}

// MarshalJSON implements [json.Marshaler].
func (r Resource) MarshalJSON() ([]byte, error) {
	uri, err := json.Marshal(r.URI)
	if err != nil {
		return nil, fmt.Errorf("encode resource uri: %w", err)
	}
	name, err := json.Marshal(r.Name)
	if err != nil {
		return nil, fmt.Errorf("encode resource name: %w", err)
	}

	return mergeModeled(r.extras, object{"uri": uri, "name": name})
}

// ResourcesListResult preserves unmodeled members of a resources/list result.
type ResourcesListResult struct {
	Resources  []*Resource
	NextCursor string

	extras object
}

// UnmarshalJSON implements [json.Unmarshaler].
func (r *ResourcesListResult) UnmarshalJSON(data []byte) error {
	members, err := decodeObject(data)
	if err != nil {
		return fmt.Errorf("decode resources/list result: %w", err)
	}

	var next ResourcesListResult
	if raw, ok := members["resources"]; ok {
		// Reject null so mutation cannot silently normalize it to [].
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("resources is null, not an array")
		}
		if err := json.Unmarshal(raw, &next.Resources); err != nil {
			return fmt.Errorf("decode resources: %w", err)
		}
	}
	for idx, resource := range next.Resources {
		if resource == nil {
			return fmt.Errorf("resource %d is null", idx)
		}
	}
	if raw, ok := members["nextCursor"]; ok {
		if err := json.Unmarshal(raw, &next.NextCursor); err != nil {
			return fmt.Errorf("decode nextCursor: %w", err)
		}
	}
	next.extras = extrasOnly(members, "resources", "nextCursor")

	*r = next
	return nil
}

// MarshalJSON implements [json.Marshaler].
func (r ResourcesListResult) MarshalJSON() ([]byte, error) {
	resources := r.Resources
	if resources == nil {
		resources = []*Resource{}
	}
	encodedResources, err := json.Marshal(resources)
	if err != nil {
		return nil, fmt.Errorf("encode resources: %w", err)
	}

	modeled := object{"resources": encodedResources}
	if r.NextCursor != "" {
		cursor, err := json.Marshal(r.NextCursor)
		if err != nil {
			return nil, fmt.Errorf("encode nextCursor: %w", err)
		}
		modeled["nextCursor"] = cursor
	}

	return mergeModeled(r.extras, modeled)
}

func (r *ToolsListResult) confineToCaller() { r.extras.confineToCaller() }

func (r *ResourcesListResult) confineToCaller() { r.extras.confineToCaller() }

func (r ToolsListResult) clone() ToolsListResult {
	r.extras = maps.Clone(r.extras)
	if r.extras == nil {
		r.extras = object{}
	}

	return r
}

func (r ResourcesListResult) clone() ResourcesListResult {
	r.extras = maps.Clone(r.extras)
	if r.extras == nil {
		r.extras = object{}
	}

	return r
}

// requireUnambiguousInvocation rejects aliases that could make upstream invoke a
// different tool than Gram authorized.
func requireUnambiguousInvocation(members object, modeled ...string) error {
	for _, name := range modeled {
		if _, ok := members[name]; !ok {
			continue
		}
		for carried := range members {
			if carried != name && strings.EqualFold(carried, name) {
				return fmt.Errorf("members %q and %q differ only by case", name, carried)
			}
		}
	}

	return nil
}
