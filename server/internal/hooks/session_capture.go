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

// sessionIDToUUID converts a Claude Code session_id string to a deterministic UUIDv5.
// Uses RFC 4122 compliant UUIDv5 generation so the same session_id always maps to the same UUID.
func sessionIDToUUID(sessionID string) uuid.UUID {
	return uuid.NewSHA1(claudeSessionNamespace, []byte(sessionID))
}

// makeHookResult creates a ClaudeHookResult with the HookSpecificOutput populated.
func makeHookResult(hookEventName string) *gen.ClaudeHookResult {
	return &gen.ClaudeHookResult{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:            &hookEventName,
			AdditionalContext:        nil,
			PermissionDecision:       nil,
			PermissionDecisionReason: nil,
		},
		Continue:       nil,
		StopReason:     nil,
		SuppressOutput: nil,
	}
}

// handleUserPromptSubmit captures the user's prompt text as a chat message.
func (s *Service) handleUserPromptSubmit(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	return makeHookResult(payload.HookEventName), nil
}

// handleStop captures the assistant's final response text.
// Note: If the Stop event includes tool calls, those are handled separately by PreToolUse events,
// so we skip creating duplicate messages here.
func (s *Service) handleStop(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	return makeHookResult(payload.HookEventName), nil
}

// handleSessionEnd finalizes the session by updating the timestamp.
func (s *Service) handleSessionEnd(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	return makeHookResult(payload.HookEventName), nil
}

// handleNotification handles notification events (permission_prompt, idle_prompt, etc.)
func (s *Service) handleNotification(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	return makeHookResult(payload.HookEventName), nil
}

// persistConversationEvent writes a conversation event (user prompt or assistant response) to PostgreSQL.
func (s *Service) persistConversationEvent(ctx context.Context, payload *gen.ClaudeHookPayload, metadata *SessionMetadata) {
	if s.productFeatures == nil {
		return
	}

	// Check if session capture is enabled for this org
	enabled, err := s.productFeatures.IsFeatureEnabled(ctx, metadata.GramOrgID, productfeatures.FeatureSessionCapture)
	if err != nil {
		s.logger.WarnContext(ctx, "check session_capture feature flag", attr.SlogError(err))
		return
	}
	if !enabled {
		return
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid project ID in session metadata", attr.SlogError(err))
		return
	}

	chatID := sessionIDToUUID(*payload.SessionID)
	chatRepoQueries := chatRepo.New(s.db)

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
		return
	}

	if content == "" {
		return
	}

	insertMessage := func() error {
		_, err = chatRepoQueries.CreateChatMessage(ctx, []chatRepo.CreateChatMessageParams{{
			ChatID:           chatID,
			ProjectID:        projectID,
			Role:             role,
			Content:          content,
			Model:            model,
			UserID:           conv.ToPGTextEmpty(metadata.UserEmail),
			Source:           conv.ToPGText(metadata.ServiceName),
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			ContentRaw:       nil,
			ContentAssetUrl:  conv.ToPGTextEmpty(""),
			StorageError:     conv.ToPGTextEmpty(""),
			MessageID:        conv.ToPGTextEmpty(""),
			ToolCallID:       conv.ToPGTextEmpty(""),
			ExternalUserID:   conv.ToPGTextEmpty(""),
			FinishReason:     conv.ToPGTextEmpty(""),
			ToolCalls:        nil,
			Origin:           conv.ToPGTextEmpty(""),
			UserAgent:        conv.ToPGTextEmpty(""),
			IpAddress:        conv.ToPGTextEmpty(""),
		}})
		return err
	}

	// Only the first message needs to create a chat record, so we save database writes by only
	// upserting the chat record if inserting the message fails.
	if err := insertMessage(); err != nil {
		// Check if this is a foreign key violation (chat doesn't exist)
		if isForeignKeyViolation(err) {
			// Create the chat and retry
			_, upsertErr := s.repo.UpsertClaudeCodeSession(ctx, repo.UpsertClaudeCodeSessionParams{
				ID:             chatID,
				ProjectID:      projectID,
				OrganizationID: metadata.GramOrgID,
				UserID:         conv.ToPGTextEmpty(metadata.UserEmail),
				Title:          conv.ToPGText("Claude Code Session"),
			})
			if upsertErr != nil {
				s.logger.ErrorContext(ctx, "upsert claude code session after FK violation", attr.SlogError(upsertErr))
				return
			}

			// Retry message creation
			if err := insertMessage(); err != nil {
				s.logger.ErrorContext(ctx, "insert claude code message after creating chat", attr.SlogError(err))
				return
			}
		} else {
			s.logger.ErrorContext(ctx, "insert claude code message", attr.SlogError(err))
			return
		}
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
}

// writeToolCallRequestToPG writes an assistant message with tool_calls to PostgreSQL.
func (s *Service) writeToolCallRequestToPG(ctx context.Context, payload *gen.ClaudeHookPayload, metadata *SessionMetadata) {
	if s.productFeatures == nil {
		return
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid project ID", attr.SlogError(err))
		return
	}

	chatID := sessionIDToUUID(*payload.SessionID)
	chatRepoQueries := chatRepo.New(s.db)

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
		s.logger.ErrorContext(ctx, "marshal tool_calls", attr.SlogError(err))
		return
	}

	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "assistant",
		Content:          "", // Tool call requests typically have empty content
		Model:            conv.ToPGTextEmpty(conv.PtrValOr(payload.Model, "")),
		UserID:           conv.ToPGTextEmpty(metadata.UserEmail),
		Source:           conv.ToPGText("ClaudeCode"),
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
		ExternalUserID:   conv.ToPGTextEmpty(""),
		Origin:           conv.ToPGTextEmpty(""),
		UserAgent:        conv.ToPGTextEmpty(""),
		IpAddress:        conv.ToPGTextEmpty(""),
	}

	// Try to insert the message
	_, err = chatRepoQueries.CreateChatMessage(ctx, []chatRepo.CreateChatMessageParams{msgParams})
	if err != nil {
		// Check if this is a foreign key violation (chat doesn't exist)
		if isForeignKeyViolation(err) {
			// Create the chat and retry
			_, upsertErr := s.repo.UpsertClaudeCodeSession(ctx, repo.UpsertClaudeCodeSessionParams{
				ID:             chatID,
				ProjectID:      projectID,
				OrganizationID: metadata.GramOrgID,
				UserID:         conv.ToPGTextEmpty(metadata.UserEmail),
				Title:          conv.ToPGText(activities.DefaultClaudeChatTitle),
			})
			if upsertErr != nil {
				s.logger.ErrorContext(ctx, "upsert claude code session after FK violation", attr.SlogError(upsertErr))
				return
			}

			// Retry message creation
			_, err = chatRepoQueries.CreateChatMessage(ctx, []chatRepo.CreateChatMessageParams{msgParams})
			if err != nil {
				s.logger.ErrorContext(ctx, "insert tool call request message after creating chat", attr.SlogError(err))
			}
		} else {
			s.logger.ErrorContext(ctx, "insert tool call request message", attr.SlogError(err))
		}
	}
}

// writeToolCallResultToPG writes a tool result message to PostgreSQL.
func (s *Service) writeToolCallResultToPG(ctx context.Context, payload *gen.ClaudeHookPayload, metadata *SessionMetadata) {
	if s.productFeatures == nil {
		return
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid project ID", attr.SlogError(err))
		return
	}

	chatID := sessionIDToUUID(*payload.SessionID)
	chatRepoQueries := chatRepo.New(s.db)

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
		return // No content to store
	}

	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "tool",
		Content:          content,
		UserID:           conv.ToPGTextEmpty(metadata.UserEmail),
		Source:           conv.ToPGText("ClaudeCode"),
		ToolCallID:       conv.ToPGTextEmpty(conv.PtrValOr(payload.ToolUseID, "")),
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		ContentRaw:       nil,
		ContentAssetUrl:  conv.ToPGTextEmpty(""),
		StorageError:     conv.ToPGTextEmpty(""),
		Model:            conv.ToPGTextEmpty(""),
		MessageID:        conv.ToPGTextEmpty(""),
		ExternalUserID:   conv.ToPGTextEmpty(""),
		FinishReason:     conv.ToPGTextEmpty(""),
		ToolCalls:        nil,
		Origin:           conv.ToPGTextEmpty(""),
		UserAgent:        conv.ToPGTextEmpty(""),
		IpAddress:        conv.ToPGTextEmpty(""),
	}

	// Try to insert the message
	_, err = chatRepoQueries.CreateChatMessage(ctx, []chatRepo.CreateChatMessageParams{msgParams})
	if err != nil {
		// Check if this is a foreign key violation (chat doesn't exist)
		if isForeignKeyViolation(err) {
			// Create the chat and retry
			_, upsertErr := s.repo.UpsertClaudeCodeSession(ctx, repo.UpsertClaudeCodeSessionParams{
				ID:             chatID,
				ProjectID:      projectID,
				OrganizationID: metadata.GramOrgID,
				UserID:         conv.ToPGTextEmpty(metadata.UserEmail),
				Title:          conv.ToPGText(activities.DefaultClaudeChatTitle),
			})
			if upsertErr != nil {
				s.logger.ErrorContext(ctx, "upsert claude code session after FK violation", attr.SlogError(upsertErr))
				return
			}

			// Retry message creation
			_, err = chatRepoQueries.CreateChatMessage(ctx, []chatRepo.CreateChatMessageParams{msgParams})
			if err != nil {
				s.logger.ErrorContext(ctx, "insert tool result message after creating chat", attr.SlogError(err))
			}
		} else {
			s.logger.ErrorContext(ctx, "insert tool result message", attr.SlogError(err))
		}
	}

	// If this was an error, we could optionally set tool_outcome based on isError
	_ = isError
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
