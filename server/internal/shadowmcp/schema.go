package shadowmcp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// InjectToolsetIDConstant injects a required [XGramToolsetIDField] string
// property into a tool's input JSON Schema, fixed via "const" to the given
// scopeID. Tool callers must echo the value back so downstream
// validators can recover which Gram-managed scope (a toolset for `/mcp`
// servers, a remote MCP server for `/x/mcp`) authored the call.
//
// The schema is mutated as a structural map operation: the function
// unmarshals the schema into [map[string]any], adds the property to
// "properties", appends it to "required" if not already present, and
// re-marshals. Existing fields are preserved; "type" is defaulted to
// "object" when absent. An existing entry with the same property name
// is replaced — this defends against upstream schemas that declare a
// colliding [XGramToolsetIDField] property whose value the caller could
// otherwise control. Returns the original schema bytes unchanged on
// any marshaling failure.
func InjectToolsetIDConstant(schema json.RawMessage, scopeID string) (json.RawMessage, error) {
	schemaMap := map[string]any{}
	if len(schema) > 0 {
		if err := json.Unmarshal(schema, &schemaMap); err != nil {
			return schema, fmt.Errorf("parse tool input schema: %w", err)
		}
	}

	if _, ok := schemaMap["type"]; !ok {
		schemaMap["type"] = "object"
	}

	props, _ := schemaMap["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	props[XGramToolsetIDField] = map[string]any{
		"type":        "string",
		"const":       scopeID,
		"description": "Internal Gram toolset identifier. Must be passed through unchanged.",
	}
	schemaMap["properties"] = props

	required, _ := schemaMap["required"].([]any)
	hasField := false
	for _, r := range required {
		if s, ok := r.(string); ok && s == XGramToolsetIDField {
			hasField = true
			break
		}
	}
	if !hasField {
		required = append(required, XGramToolsetIDField)
	}
	schemaMap["required"] = required

	out, err := json.Marshal(schemaMap)
	if err != nil {
		return schema, fmt.Errorf("marshal mutated tool input schema: %w", err)
	}
	return out, nil
}

// StripToolsetIDProperty removes the [XGramToolsetIDField] property from a
// tool-call arguments JSON object, returning the input unchanged when the
// arguments are empty, not a JSON object, or don't contain the property.
// The property is injected by [InjectToolsetIDConstant] into the tool's
// input schema and echoed back by the caller; strip it before forwarding
// the arguments to the underlying tool so the tool sees its declared
// shape, not the proxy's envelope.
func StripToolsetIDProperty(args json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(args)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return args, nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return args, fmt.Errorf("unmarshal tool arguments: %w", err)
	}
	if _, ok := obj[XGramToolsetIDField]; !ok {
		return args, nil
	}
	delete(obj, XGramToolsetIDField)

	out, err := json.Marshal(obj)
	if err != nil {
		return args, fmt.Errorf("marshal scrubbed tool arguments: %w", err)
	}
	return out, nil
}
