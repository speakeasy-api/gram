package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

var (
	// claudeSessionNamespace is the UUIDv5 namespace for Claude Code session IDs.
	// This ensures deterministic UUID generation from session ID strings.
	claudeSessionNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

	// ErrChatNotFound indicates the chat (conversation) does not exist.
	ErrChatNotFound = errors.New("chat not found")
)

// isForeignKeyViolation checks if the error is a PostgreSQL foreign key constraint violation.
// This indicates that the referenced chat does not exist.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 23503 is PostgreSQL's foreign_key_violation error code
		return pgErr.Code == "23503"
	}
	return false
}

// isConversationEvent returns true if the event is a conversation capture event (not a tool call).
func isConversationEvent(eventName string) bool {
	switch eventName {
	case "UserPromptSubmit", "Stop":
		return true
	default:
		return false
	}
}

// sessionIDToUUID converts a Claude Code session_id string to a UUID.
// The session_id is expected to already be a valid UUID string.
// If parsing fails, falls back to generating a deterministic UUIDv5 from the session_id.
func sessionIDToUUID(sessionID string) uuid.UUID {
	// Try to parse the session ID as a UUID directly
	parsedUUID, err := uuid.Parse(sessionID)
	if err == nil {
		return parsedUUID
	}

	// Fallback: generate a deterministic UUIDv5 from the session ID string
	return uuid.NewSHA1(claudeSessionNamespace, []byte(sessionID))
}

// makeHookResult creates a ClaudeHookResult, attaching HookSpecificOutput only
// for hook events whose Claude Code response schema permits it. Stop, SessionStart,
// SessionEnd, Notification, and PostToolUseFailure must NOT carry hookSpecificOutput
// — Claude Code rejects unknown variants with "Hook JSON output validation failed".
func makeHookResult(hookEventName string) *gen.ClaudeHookResult {
	result := &gen.ClaudeHookResult{
		HookSpecificOutput: nil,
		Continue:           nil,
		StopReason:         nil,
		SuppressOutput:     nil,
		SystemMessage:      nil,
	}
	if hookEventName == "PreToolUse" {
		result.HookSpecificOutput = &HookSpecificOutput{
			HookEventName:            &hookEventName,
			AdditionalContext:        nil,
			PermissionDecision:       nil,
			PermissionDecisionReason: nil,
		}
	}
	return result
}

// handleUserPromptSubmit captures the user's prompt text as a chat message.
// When a blocking risk policy matches, it denies the prompt with HTTP 403.
// The send_hook.sh script converts 4xx responses to exit code 2 (block).
func (s *Service) handleUserPromptSubmit(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	if s.riskScanner != nil && payload.Prompt != nil && payload.SessionID != nil {
		if scanResult := s.scanClaudeForEnforcement(ctx, payload); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			// ClickHouse always gets the technical reason; the user_message
			// override only changes what the agent / end user sees.
			if metadata, err := s.getSessionMetadata(ctx, *payload.SessionID); err == nil {
				s.writeClaudeBlockToClickHouse(ctx, payload, &metadata, auditReason)
			}
			return nil, oops.E(oops.CodeForbidden, nil, "%s", userReason)
		}
	}
	return makeHookResult(payload.HookEventName), nil
}

// handleStop captures the assistant's final response text.
// Note: If the Stop event includes tool calls, those are handled separately by PreToolUse events,
// so we skip creating duplicate messages here.
func (s *Service) handleStop(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	return makeHookResult(payload.HookEventName), nil
}

// handleSessionEnd finalizes the session by updating the timestamp.
func (s *Service) handleSessionEnd(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	return makeHookResult(payload.HookEventName), nil
}

// handleNotification handles notification events (permission_prompt, idle_prompt, etc.)
func (s *Service) handleNotification(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	return makeHookResult(payload.HookEventName), nil
}

// insertMessageWithFallbackUpsert inserts a chat message, creating the chat if needed.
// This helper ensures the feature flag check is applied consistently.
func (s *Service) insertMessageWithFallbackUpsert(
	ctx context.Context,
	metadata *SessionMetadata,
	chatID uuid.UUID,
	projectID uuid.UUID,
	msgParams chatRepo.CreateChatMessageParams,
	defaultTitle string,
) error {
	if s.productFeatures == nil {
		return nil
	}

	// Check if session capture is enabled for this org
	enabled, err := s.productFeatures.IsFeatureEnabled(ctx, metadata.GramOrgID, productfeatures.FeatureSessionCapture)
	if err != nil {
		return fmt.Errorf("check session_capture feature flag: %w", err)
	}
	if !enabled {
		return nil
	}

	// Try to insert the message (Write handles notification on success).
	_, err = s.writer.Write(ctx, projectID, []chatRepo.CreateChatMessageParams{msgParams})
	if err == nil {
		return nil
	}

	// If this is not a foreign key violation (chat doesn't exist), fail.
	if !isForeignKeyViolation(err) {
		return fmt.Errorf("insert chat message: %w", err)
	}

	// Create the chat and retry.
	_, upsertErr := s.repo.UpsertClaudeCodeSession(ctx, repo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: metadata.GramOrgID,
		UserID:         conv.ToPGTextEmpty(""),
		ExternalUserID: conv.ToPGTextEmpty(metadata.UserEmail),
		Title:          conv.ToPGText(defaultTitle),
	})
	if upsertErr != nil {
		return fmt.Errorf("upsert claude code session after FK violation: %w", upsertErr)
	}

	if _, err = s.writer.Write(ctx, projectID, []chatRepo.CreateChatMessageParams{msgParams}); err != nil {
		return fmt.Errorf("insert chat message after creating chat: %w", err)
	}
	return nil
}

// persistConversationEvent writes a conversation event (user prompt or assistant response) to PostgreSQL.
func (s *Service) persistConversationEvent(ctx context.Context, payload *gen.ClaudePayload, metadata *SessionMetadata) error {
	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID in session metadata: %w", err)
	}

	chatID := sessionIDToUUID(*payload.SessionID)

	// Determine role and content based on event type
	var role, content string
	var model pgtype.Text

	switch payload.HookEventName {
	case "UserPromptSubmit":
		role = "user"
		content = conv.PtrValOr(payload.Prompt, "")
	case "Stop":
		role = "assistant"
		content = conv.PtrValOr(payload.LastAssistantMessage, "")
		model = conv.ToPGTextEmpty(conv.PtrValOr(payload.Model, ""))
	default:
		return nil
	}

	if content == "" {
		return nil
	}

	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             role,
		Content:          content,
		Model:            model,
		UserID:           conv.ToPGTextEmpty(""),
		Source:           conv.ToPGText(metadata.ServiceName),
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		ContentRaw:       nil,
		ContentAssetUrl:  conv.ToPGTextEmpty(""),
		StorageError:     conv.ToPGTextEmpty(""),
		MessageID:        conv.ToPGTextEmpty(""),
		ToolCallID:       conv.ToPGTextEmpty(""),
		ExternalUserID:   conv.ToPGTextEmpty(metadata.UserEmail),
		FinishReason:     conv.ToPGTextEmpty(""),
		ToolCalls:        nil,
		Origin:           conv.ToPGTextEmpty(""),
		UserAgent:        conv.ToPGTextEmpty(""),
		IpAddress:        conv.ToPGTextEmpty(""),
		ContentHash:      nil,
		Generation:       0,
	}

	if err := s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, activities.DefaultClaudeChatTitle); err != nil {
		return err
	}

	// Schedule chat title generation for assistant messages
	if role == "assistant" && s.chatTitleGenerator != nil {
		if err := s.chatTitleGenerator.ScheduleChatTitleGeneration(
			context.WithoutCancel(ctx),
			chatID.String(),
			metadata.GramOrgID,
			projectID.String(),
		); err != nil {
			s.logger.WarnContext(ctx, "failed to schedule chat title generation", attr.SlogError(err))
		}
	}

	return nil
}

// writeToolCallRequestToPG writes an assistant message with tool_calls to PostgreSQL.
func (s *Service) writeToolCallRequestToPG(ctx context.Context, payload *gen.ClaudePayload, metadata *SessionMetadata) error {
	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	chatID := sessionIDToUUID(*payload.SessionID)

	// Build tool_calls JSONB array from the PreToolUse payload
	toolCalls := []map[string]any{{
		"id":   conv.PtrValOr(payload.ToolUseID, ""),
		"type": "function",
		"function": map[string]any{
			"name":      conv.PtrValOr(payload.ToolName, ""),
			"arguments": marshalToJSON(payload.ToolInput),
		},
	}}

	toolCallsJSON, err := json.Marshal(toolCalls)
	if err != nil {
		return fmt.Errorf("marshal tool_calls: %w", err)
	}

	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "assistant",
		Content:          "", // Tool call requests typically have empty content
		Model:            conv.ToPGTextEmpty(conv.PtrValOr(payload.Model, "")),
		UserID:           conv.ToPGTextEmpty(""),
		Source:           conv.ToPGText(metadata.ServiceName),
		ToolCalls:        toolCallsJSON,
		FinishReason:     conv.ToPGText("tool_calls"),
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		ContentRaw:       nil,
		ContentAssetUrl:  conv.ToPGTextEmpty(""),
		StorageError:     conv.ToPGTextEmpty(""),
		MessageID:        conv.ToPGTextEmpty(""),
		ToolCallID:       conv.ToPGTextEmpty(""),
		ExternalUserID:   conv.ToPGTextEmpty(metadata.UserEmail),
		Origin:           conv.ToPGTextEmpty(""),
		UserAgent:        conv.ToPGTextEmpty(""),
		IpAddress:        conv.ToPGTextEmpty(""),
		ContentHash:      nil,
		Generation:       0,
	}

	return s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, activities.DefaultClaudeChatTitle)
}

// writeToolCallResultToPG writes a tool result message to PostgreSQL.
func (s *Service) writeToolCallResultToPG(ctx context.Context, payload *gen.ClaudePayload, metadata *SessionMetadata) error {
	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	chatID := sessionIDToUUID(*payload.SessionID)

	// Build content from tool response or error
	var content string
	var isError bool
	if payload.HookEventName == "PostToolUse" && payload.ToolResponse != nil {
		content = marshalToJSON(payload.ToolResponse)
		isError = false
	} else if payload.HookEventName == "PostToolUseFailure" && payload.Error != nil {
		content = marshalToJSON(payload.Error)
		isError = true
	} else {
		return nil // No content to store
	}

	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "tool",
		Content:          content,
		UserID:           conv.ToPGTextEmpty(""),
		Source:           conv.ToPGText(metadata.ServiceName),
		ToolCallID:       conv.ToPGTextEmpty(conv.PtrValOr(payload.ToolUseID, "")),
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		ContentRaw:       nil,
		ContentAssetUrl:  conv.ToPGTextEmpty(""),
		StorageError:     conv.ToPGTextEmpty(""),
		Model:            conv.ToPGTextEmpty(""),
		MessageID:        conv.ToPGTextEmpty(""),
		ExternalUserID:   conv.ToPGTextEmpty(metadata.UserEmail),
		FinishReason:     conv.ToPGTextEmpty(""),
		ToolCalls:        nil,
		Origin:           conv.ToPGTextEmpty(""),
		UserAgent:        conv.ToPGTextEmpty(""),
		IpAddress:        conv.ToPGTextEmpty(""),
		ContentHash:      nil,
		Generation:       0,
	}

	// If this was an error, we could optionally set tool_outcome based on isError
	_ = isError

	return s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, activities.DefaultClaudeChatTitle)
}

// marshalToJSON converts any value to a JSON string.
func marshalToJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
