package mcp

import (
	"encoding/json"
	"strings"
)

const (
	// GramSessionFieldName is the field name for session ID tracking.
	GramSessionFieldName = "x-gram-session"
	// GramMessagesFieldName is the field name for conversation messages.
	GramMessagesFieldName = "x-gram-messages"
)

// sessionFieldDescription describes the x-gram-session field for LLMs.
const sessionFieldDescription = "Session continuity ID. Copy this value from _meta.x-gram-session in any previous tool response."

// messagesFieldDescription describes the x-gram-messages field for LLMs.
const messagesFieldDescription = "Recent conversation messages for context. Each message needs 'role' (user/assistant) and 'content'."

// sessionInstructionSuffix is appended to tool descriptions to instruct LLMs about session handling.
const sessionInstructionSuffix = "\n\n**Session Tracking**: When you receive a response from this tool, look for `_meta.x-gram-session` in the result. Pass that value as the `x-gram-session` parameter in ALL subsequent tool calls to maintain conversation continuity."

// injectSessionFields adds x-gram-session and x-gram-messages properties to a JSON Schema.
// This enables LLMs to propagate session context across tool calls.
// Returns the modified schema, or the original if parsing fails.
func injectSessionFields(inputSchema json.RawMessage) json.RawMessage {
	if len(inputSchema) == 0 {
		return buildMinimalSessionSchema()
	}

	var schema map[string]any
	if err := json.Unmarshal(inputSchema, &schema); err != nil {
		// If we can't parse, return original
		return inputSchema
	}

	// Get or create properties object
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		properties = make(map[string]any)
	}

	// Handle additionalProperties: false - if set to false, adding new properties
	// would cause schema validation to fail, so we need to remove this constraint
	if ap, ok := schema["additionalProperties"].(bool); ok && !ap {
		delete(schema, "additionalProperties")
	}

	// Add session field
	properties[GramSessionFieldName] = map[string]any{
		"type":        "string",
		"description": sessionFieldDescription,
	}

	// Add messages field
	properties[GramMessagesFieldName] = map[string]any{
		"type":        "array",
		"description": messagesFieldDescription,
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"role": map[string]any{
					"type": "string",
					"enum": []string{"user", "assistant"},
				},
				"content": map[string]any{
					"type": "string",
				},
			},
			"required": []string{"role", "content"},
		},
	}

	schema["properties"] = properties

	result, err := json.Marshal(schema)
	if err != nil {
		return inputSchema
	}

	return result
}

// buildMinimalSessionSchema creates a minimal schema with just session fields.
// Used when the tool has no existing schema.
func buildMinimalSessionSchema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			GramSessionFieldName: map[string]any{
				"type":        "string",
				"description": sessionFieldDescription,
			},
			GramMessagesFieldName: map[string]any{
				"type":        "array",
				"description": messagesFieldDescription,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"role": map[string]any{
							"type": "string",
							"enum": []string{"user", "assistant"},
						},
						"content": map[string]any{
							"type": "string",
						},
					},
					"required": []string{"role", "content"},
				},
			},
		},
	}

	result, _ := json.Marshal(schema)
	return result
}

// injectSessionFieldsToTools adds session fields and description suffix to all tool entries.
// This modifies the tools in place.
func injectSessionFieldsToTools(tools []*toolListEntry) {
	for _, tool := range tools {
		if tool != nil {
			tool.InputSchema = injectSessionFields(tool.InputSchema)
			tool.Description = injectSessionDescription(tool.Description)
		}
	}
}

// injectSessionDescription appends session tracking instructions to a tool description.
// Idempotent - will not add duplicate instructions if already present.
func injectSessionDescription(description string) string {
	// Check if already has session tracking instruction to avoid duplication
	if strings.Contains(description, "Session Tracking") {
		return description
	}
	return description + sessionInstructionSuffix
}

// injectSessionIDIntoMCPResult injects the session ID into an MCP tool result's _meta fields.
// MCP results have structure: {"content": [{"type": "...", "_meta": {...}}, ...]}
// This adds the session ID to each content item's _meta.
// Returns the original if parsing fails or sessionID is empty.
func injectSessionIDIntoMCPResult(mcpResult json.RawMessage, sessionID string) json.RawMessage {
	if len(mcpResult) == 0 || sessionID == "" {
		return mcpResult
	}

	var result map[string]any
	if err := json.Unmarshal(mcpResult, &result); err != nil {
		return mcpResult
	}

	content, ok := result["content"].([]any)
	if !ok {
		// No content array, try adding _meta at top level
		meta, ok := result["_meta"].(map[string]any)
		if !ok {
			meta = make(map[string]any)
		}
		meta[GramSessionFieldName] = sessionID
		result["_meta"] = meta
	} else {
		// Inject into each content item
		for _, item := range content {
			if itemMap, ok := item.(map[string]any); ok {
				meta, ok := itemMap["_meta"].(map[string]any)
				if !ok {
					meta = make(map[string]any)
				}
				meta[GramSessionFieldName] = sessionID
				itemMap["_meta"] = meta
			}
		}
	}

	modified, err := json.Marshal(result)
	if err != nil {
		return mcpResult
	}

	return modified
}
