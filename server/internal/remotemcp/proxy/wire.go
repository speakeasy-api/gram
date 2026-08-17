package proxy

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
)

// The types here model the MCP payloads the proxy mutates. They exist instead of
// the vendored modelcontextprotocol/go-sdk structs because a struct commits its
// author's opinion about which members exist, and committing a mutation through
// one publishes that opinion as Gram's: every member the SDK version does not
// model is deleted in transit. MCP 2026-07-28 requires `resultType` on every
// result and clients validate it, so a member Gram drops makes Gram the
// non-compliant party for a payload the upstream got right — and the proxy is a
// pass-through on protocol version, so the revision being validated against is
// the one the client and the upstream settled between themselves, not one Gram
// picked.
//
// Each type models the members Gram itself reads and carries every other member
// of the same object, so a decode/encode round trip reproduces the payload.
// Carried members live on the object that owns them, which is what makes
// filtering a list preserve each surviving item's own members with no bookkeeping
// on the side, and a nested object that Gram reads into is modeled by a type of
// its own so the same guarantee holds one level down.
//
// Adding a field is how Gram takes a dependency on a member. Anything not listed
// travels through untouched, which is the point.
//
// Two things are deliberately not preserved, because preserving them would make
// the payload mean different things to different readers:
//
//   - A repeated member name collapses to the single value Go's decoder kept.
//     Parsers disagree here — Go keeps the last, first-wins parsers are common —
//     so relaying both onward would let Gram authorize one tool while a peer
//     downstream reads another.
//   - A member whose name differs from a modeled one only by case is dropped.
//     Go matches a struct field to a member case-insensitively while the protocol
//     matches keys exactly, so carrying both `name` and `Name` would leave Gram
//     having authorized one value and a case-insensitive peer reading the other.
//
// Member order is not preserved either, but that carries no meaning. Byte-for-byte
// relay remains the behaviour whenever nothing mutates, which never decodes at all.

// extrasOnly returns the members of a decoded object that the modeled names do
// not claim, dropping each modeled name and any case-fold alias of it.
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

// mergeModeled writes the carried extras and then the modeled members into one
// object, dropping any extra that case-fold collides with a modeled name so the
// emitted object cannot read differently to a case-insensitive parser.
func mergeModeled(extras object, modeled object) (json.RawMessage, error) {
	names := make([]string, 0, len(modeled))
	for name := range modeled {
		names = append(names, name)
	}

	out := extrasOnly(extras, names...)
	maps.Copy(out, modeled)

	return out.encode()
}

// ToolAnnotations is a tool's annotation hints. Gram matches annotation-scoped
// grants against the four the spec defines; anything else an upstream attaches is
// carried. Absent and explicitly-false hints are both nil, so a caller cannot
// mistake "not declared" for "declared false".
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

// Tool is a tools/list entry. Gram reads a tool's name to authorize it, its
// annotation hints to match annotation-scoped grants, and carries its input
// schema so a caller can construct a usable replacement; everything else —
// description, output schema, title, icons, `_meta`, vendor extensions — is
// carried opaquely.
type Tool struct {
	// Name identifies the tool and is what Gram authorizes against, so it is
	// read exact-key rather than through a case-insensitive struct match.
	Name string

	// InputSchema is the tool's declared parameter schema, kept as the bytes the
	// peer sent so no schema dialect or numeric formatting is reinterpreted.
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

// ToolsListResult is a tools/list result. Gram reads the tool array and the
// pagination cursor; every other member — `resultType`, the caching hints,
// `_meta`, vendor extensions — is carried.
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
		if err := json.Unmarshal(raw, &next.Tools); err != nil {
			return fmt.Errorf("decode tools: %w", err)
		}
	}
	// A null entry would reach every reader of the list as a tool with no name,
	// so it is refused here rather than guarded at each of them.
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
		// MCP list results carry an array, never null.
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

// Resource is a resources/list entry. Gram reads a resource's uri and name;
// everything else is carried.
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

// ResourcesListResult is a resources/list result, modeled like
// [ToolsListResult].
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

// confineToCaller marks a list result the proxy has filtered for one caller as
// cacheable only within that caller's authorization context. See the identically
// named method on [object] for why.
func (r *ToolsListResult) confineToCaller() { r.extras.confineToCaller() }

// confineToCaller mirrors [ToolsListResult.confineToCaller].
func (r *ResourcesListResult) confineToCaller() { r.extras.confineToCaller() }

// clone returns a copy whose carried members can be mutated without touching the
// original's, so a setter can stage a mutation and abandon it on failure.
func (r ToolsListResult) clone() ToolsListResult {
	r.extras = maps.Clone(r.extras)
	if r.extras == nil {
		r.extras = object{}
	}

	return r
}

// clone mirrors [ToolsListResult.clone].
func (r ResourcesListResult) clone() ResourcesListResult {
	r.extras = maps.Clone(r.extras)
	if r.extras == nil {
		r.extras = object{}
	}

	return r
}

// requireUnambiguousInvocation rejects a request payload whose members would
// identify a different operation to a peer than the one Gram authorized.
//
// Gram authorizes a tools/call against the decoded `name` and then forwards the
// payload, so a body carrying both `name` and a case-fold alias of it can be
// authorized as one tool and executed as another by an exact-key upstream.
// Dropping the alias is the right answer for a response Gram is filtering, but
// not for a request: silently changing which tool is invoked is worse than
// refusing to invoke one. Callers surface this as a mutation failure.
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
