package codemode

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// NormalizeResult preserves structured and content results without exposing _meta.
func NormalizeResult(raw json.RawMessage) (*ToolResult, error) {
	if len(raw) > MaxResultBytes {
		return nil, fmt.Errorf("callback result exceeds limit")
	}
	var result struct {
		StructuredContent json.RawMessage              `json:"structuredContent"`
		Content           []map[string]json.RawMessage `json:"content"`
		IsError           bool                         `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("invalid MCP tool result")
	}
	for _, block := range result.Content {
		delete(block, "_meta")
		if rawResource, ok := block["resource"]; ok {
			var resource map[string]json.RawMessage
			if err := json.Unmarshal(rawResource, &resource); err != nil || resource == nil {
				return nil, fmt.Errorf("invalid embedded MCP resource")
			}
			delete(resource, "_meta")
			publicResource, err := json.Marshal(resource)
			if err != nil {
				return nil, fmt.Errorf("encode embedded resource: %w", err)
			}
			block["resource"] = publicResource
		}
	}
	if result.Content == nil {
		result.Content = []map[string]json.RawMessage{}
	}
	content, err := json.Marshal(result.Content)
	if err != nil {
		return nil, fmt.Errorf("encode MCP content: %w", err)
	}
	data := result.StructuredContent
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	message := ""
	if result.IsError {
		message = "upstream tool returned an error; inspect content"
	}
	return &ToolResult{Outcome: "completed", Warnings: nil, OK: !result.IsError, Data: data, Content: content, Error: message}, nil
}

// DispatchError preserves uncertainty when a transport fails after admission.
// BeforeInvoke is set only when the backend knows that dispatch never began.
type DispatchError struct {
	// BeforeInvoke distinguishes a guard refusal from an unknown external outcome.
	BeforeInvoke bool
	// Code is a public error code; private causes stay in Gram logs.
	Code string
}

func (e *DispatchError) Error() string { return e.Code }

func failedResult(outcome, code string) *ToolResult {
	return &ToolResult{Outcome: outcome, Warnings: nil, OK: false, Data: json.RawMessage("null"), Content: json.RawMessage("[]"), Error: code}
}

// ValidateArguments checks original input schemas without loading remote refs.
func ValidateArguments(definition, arguments json.RawMessage) error {
	var object map[string]json.RawMessage
	if len(arguments) > MaxResultBytes || json.Unmarshal(arguments, &object) != nil || object == nil {
		return fmt.Errorf("arguments must be a bounded JSON object")
	}
	return validateSchemaField(definition, "inputSchema", arguments, true)
}

func validateSchemaField(definition json.RawMessage, field string, value json.RawMessage, required bool) error {
	// Schema compilation is bounded independently of the aggregate catalog.
	const maxSchemaBytes = 128 << 10 // 128 KiB
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(definition, &fields); err != nil {
		return fmt.Errorf("invalid tool definition")
	}
	raw := fields[field]
	if len(raw) == 0 && !required {
		return nil
	}
	if len(raw) == 0 || len(raw) > maxSchemaBytes {
		return fmt.Errorf("missing or oversized %s", field)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("parse %s: %w", field, err)
	}
	compiler := jsonschema.NewCompiler()
	// No file or network loader: only definitions in this schema are resolvable.
	compiler.UseLoader(nil)
	const schemaURL = "urn:gram:code-mode:schema"
	if err := compiler.AddResource(schemaURL, document); err != nil {
		return fmt.Errorf("load %s: %w", field, err)
	}
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		return fmt.Errorf("compile %s: %w", field, err)
	}
	data, err := jsonschema.UnmarshalJSON(bytes.NewReader(value))
	if err != nil {
		return fmt.Errorf("parse value: %w", err)
	}
	if err := schema.Validate(data); err != nil {
		return fmt.Errorf("invalid %s value: %w", field, err)
	}
	return nil
}
