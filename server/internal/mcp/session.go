package mcp

import (
	"encoding/json"

	"github.com/google/uuid"
)

// MCPMessage represents a conversation message from x-gram-messages.
type MCPMessage struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"` // message content
}

// MCPSessionContext holds session-related data extracted from tool call arguments.
type MCPSessionContext struct {
	SessionID    uuid.UUID    // Session UUID (generated if not provided)
	Messages     []MCPMessage // Messages from x-gram-messages
	IsNewSession bool         // True if session ID was generated (not provided by client)
}

// extractSessionContext extracts x-gram-session and x-gram-messages from tool call arguments.
// It returns the session context and cleaned arguments (with session fields removed).
// If session ID is empty or invalid, a new UUID is generated.
func extractSessionContext(args json.RawMessage) (MCPSessionContext, json.RawMessage, error) {
	ctx := MCPSessionContext{
		SessionID:    uuid.Nil,
		Messages:     nil,
		IsNewSession: false,
	}

	if len(args) == 0 {
		ctx.SessionID = uuid.New()
		ctx.IsNewSession = true
		return ctx, args, nil
	}

	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		// Non-object args (e.g., array or primitive), generate new session
		ctx.SessionID = uuid.New()
		ctx.IsNewSession = true
		return ctx, args, nil
	}

	// Extract session ID (check both hyphen and underscore variants for LLM compatibility)
	sessionIDStr, ok := argsMap[GramSessionFieldName].(string)
	if !ok || sessionIDStr == "" {
		// Try underscore variant (some LLMs like Gemini normalize hyphens to underscores)
		sessionIDStr, ok = argsMap["x_gram_session"].(string)
	}

	if ok && sessionIDStr != "" {
		parsedID, err := uuid.Parse(sessionIDStr)
		if err != nil {
			// Invalid UUID, generate new one
			ctx.SessionID = uuid.New()
			ctx.IsNewSession = true
		} else {
			ctx.SessionID = parsedID
			ctx.IsNewSession = false
		}
	} else {
		// No session ID provided, generate new one
		ctx.SessionID = uuid.New()
		ctx.IsNewSession = true
	}

	// Extract messages (check both hyphen and underscore variants)
	messagesRaw, ok := argsMap[GramMessagesFieldName]
	if !ok {
		messagesRaw, ok = argsMap["x_gram_messages"]
	}
	if ok {
		if messagesSlice, ok := messagesRaw.([]any); ok {
			for _, msgRaw := range messagesSlice {
				if msgMap, ok := msgRaw.(map[string]any); ok {
					msg := MCPMessage{
						Role:    "",
						Content: "",
					}
					if role, ok := msgMap["role"].(string); ok {
						msg.Role = role
					}
					if content, ok := msgMap["content"].(string); ok {
						msg.Content = content
					}
					// Only add valid messages
					if msg.Role != "" && msg.Content != "" {
						ctx.Messages = append(ctx.Messages, msg)
					}
				}
			}
		}
	}

	// Remove session fields from args before passing to tool execution
	// Remove both hyphen and underscore variants for LLM compatibility
	delete(argsMap, GramSessionFieldName)
	delete(argsMap, "x_gram_session") // underscore variant
	delete(argsMap, GramMessagesFieldName)
	delete(argsMap, "x_gram_messages") // underscore variant

	cleanedArgs, err := json.Marshal(argsMap)
	if err != nil {
		// Return empty object to avoid leaking session fields to downstream tools
		return ctx, []byte("{}"), nil
	}

	return ctx, cleanedArgs, nil
}
