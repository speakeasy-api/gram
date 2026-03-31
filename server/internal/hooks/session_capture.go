package hooks

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// sessionIDToUUID converts a Claude Code session_id string to a deterministic UUID.
// Uses UUIDv5 with a fixed namespace so the same session_id always maps to the same UUID.
func sessionIDToUUID(sessionID string) uuid.UUID {
	// Use SHA256 of session ID to create a deterministic UUID
	hash := sha256.Sum256([]byte(sessionID))
	// Use the first 16 bytes as a UUIDv5-like deterministic ID
	id, _ := uuid.FromBytes(hash[:16])
	// Set version 5 and variant bits
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}

// handleUserPromptSubmit captures the user's prompt text as a chat message.
func (s *Service) handleUserPromptSubmit(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	s.recordConversationEvent(ctx, payload)

	return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
		HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
			HookEventName: &payload.HookEventName,
		},
	}, nil
}

// handleStop captures the assistant's final response text.
func (s *Service) handleStop(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	s.recordConversationEvent(ctx, payload)

	return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
		HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
			HookEventName: &payload.HookEventName,
		},
	}, nil
}

// handleSessionEnd finalizes the session by updating the timestamp.
func (s *Service) handleSessionEnd(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	if payload.SessionID != nil && *payload.SessionID != "" {
		sessionID := *payload.SessionID
		metadata, err := s.getSessionMetadata(ctx, sessionID)
		if err == nil {
			s.finalizeSession(ctx, &metadata)
		}
	}

	return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
		HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
			HookEventName: &payload.HookEventName,
		},
	}, nil
}

// handleNotification handles notification events (permission_prompt, idle_prompt, etc.)
func (s *Service) handleNotification(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
		HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
			HookEventName: &payload.HookEventName,
		},
	}, nil
}

// recordConversationEvent buffers or directly writes a conversation event (user prompt or assistant response).
func (s *Service) recordConversationEvent(ctx context.Context, payload *gen.ClaudeHookPayload) {
	if payload.SessionID == nil || *payload.SessionID == "" {
		return
	}

	sessionID := *payload.SessionID
	metadata, err := s.getSessionMetadata(ctx, sessionID)
	if err == nil {
		s.writeConversationToPG(ctx, payload, &metadata)
	} else {
		// Session not validated yet — buffer alongside tool calls
		if err := s.bufferHook(ctx, sessionID, payload); err != nil {
			s.logger.ErrorContext(ctx, "buffer conversation event", attr.SlogError(err))
		}
	}
}

// writeConversationToPG writes a conversation event (user prompt or assistant response) to PostgreSQL.
func (s *Service) writeConversationToPG(ctx context.Context, payload *gen.ClaudeHookPayload, metadata *SessionMetadata) {
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

	// Ensure the session (chat) exists
	_, err = s.repo.UpsertClaudeCodeSession(ctx, repo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: metadata.GramOrgID,
		UserID:         conv.ToPGTextEmpty(metadata.UserEmail),
		Title:          conv.ToPGText("Claude Code Session"),
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "upsert claude code session", attr.SlogError(err))
		return
	}

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
	case "SessionStart":
		role = "system"
		source := conv.PtrValOr(payload.Source, "startup")
		content = fmt.Sprintf("Session started (%s)", source)
		model = conv.ToPGTextEmpty(conv.PtrValOr(payload.Model, ""))
	default:
		return
	}

	if content == "" {
		return
	}

	err = s.repo.InsertClaudeCodeMessage(ctx, repo.InsertClaudeCodeMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		Role:      role,
		Content:   content,
		Model:     model,
		UserID:    conv.ToPGTextEmpty(metadata.UserEmail),
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "insert claude code message", attr.SlogError(err))
	}
}

// ensureSessionInPG creates the session (chat) in PG if session capture is enabled.
// Called at flush time when OTEL metadata arrives.
func (s *Service) ensureSessionInPG(ctx context.Context, sessionID string, metadata *SessionMetadata) {
	if s.productFeatures == nil {
		return
	}

	enabled, err := s.productFeatures.IsFeatureEnabled(ctx, metadata.GramOrgID, productfeatures.FeatureSessionCapture)
	if err != nil || !enabled {
		return
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return
	}

	chatID := sessionIDToUUID(sessionID)

	_, err = s.repo.UpsertClaudeCodeSession(ctx, repo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: metadata.GramOrgID,
		UserID:         conv.ToPGTextEmpty(metadata.UserEmail),
		Title:          conv.ToPGText("Claude Code Session"),
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "ensure session in PG", attr.SlogError(err))
	}
}

// finalizeSession updates the session timestamp on SessionEnd.
func (s *Service) finalizeSession(ctx context.Context, metadata *SessionMetadata) {
	if s.productFeatures == nil {
		return
	}

	enabled, err := s.productFeatures.IsFeatureEnabled(ctx, metadata.GramOrgID, productfeatures.FeatureSessionCapture)
	if err != nil || !enabled {
		return
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return
	}

	chatID := sessionIDToUUID(metadata.SessionID)

	err = s.repo.UpdateClaudeCodeSessionTimestamp(ctx, repo.UpdateClaudeCodeSessionTimestampParams{
		ID:        chatID,
		ProjectID: projectID,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "finalize session", attr.SlogError(err))
	}
}
