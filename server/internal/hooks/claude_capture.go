package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// ClaudeMessages performs idempotent batch capture of Claude Code transcript
// messages emitted on Stop / SubagentStop. Each message carries its transcript
// UUID as external_id; persistence keys on external_message_id with
// ON CONFLICT DO NOTHING, so re-delivery from multiple plugin installations (or
// a re-sent Stop) stores each message exactly once.
func (s *Service) ClaudeMessages(ctx context.Context, payload *gen.ClaudeMessagesPayload) error {
	sessionID := strings.TrimSpace(payload.SessionID)
	logger := s.logger.With(
		attr.SlogHookSource("claude"),
		attr.SlogHookEvent("ClaudeMessages"),
		attr.SlogGenAIConversationID(sessionID),
	)

	if sessionID == "" || len(payload.Messages) == 0 {
		return nil
	}

	// Optional plugin auth, same posture as Method("claude"): on failure we fall
	// through unauthenticated rather than 401 (a 401 would block the client with
	// no way to recover). Without resolvable attribution we drop the batch — it
	// is idempotent telemetry, not a request that must succeed.
	if payload.ApikeyToken != nil && *payload.ApikeyToken != "" {
		if authedCtx, err := s.authorizePluginRequest(ctx, *payload.ApikeyToken, conv.PtrValOr(payload.ProjectSlugInput, "")); err != nil {
			logger.WarnContext(ctx, "plugin auth failed on claude messages; attempting session-metadata fallback",
				attr.SlogEvent("claude_messages_auth_failed"),
				attr.SlogError(err),
			)
		} else {
			ctx = authedCtx
		}
	}

	metadata, err := s.resolveClaudeSessionMetadata(ctx, sessionID, conv.PtrValOr(payload.UserEmail, ""))
	if err != nil {
		logger.WarnContext(ctx, "skipping claude message capture; no resolvable session attribution",
			attr.SlogEvent("claude_messages_unattributed"),
			attr.SlogError(err),
		)
		return nil
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("parse project id from session metadata: %w", err)
	}

	enabled, err := s.productFeatures.IsFeatureEnabled(ctx, metadata.GramOrgID, productfeatures.FeatureSessionCapture)
	if err != nil {
		return fmt.Errorf("check session_capture feature flag: %w", err)
	}
	if !enabled {
		logger.DebugContext(ctx, "session capture disabled; skipping claude message capture",
			attr.SlogEvent("claude_messages_session_capture_disabled"),
			attr.SlogOrganizationID(metadata.GramOrgID),
			attr.SlogProjectID(metadata.ProjectID),
		)
		return nil
	}

	// Persistence must outlive the request: Claude Code closes the connection the
	// instant the hook returns, which would otherwise cancel the in-flight writes.
	ctx = context.WithoutCancel(ctx)

	chatID := sessionIDToUUID(sessionID)
	if _, err := s.repo.UpsertClaudeCodeSession(ctx, repo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: metadata.GramOrgID,
		UserID:         conv.ToPGTextEmpty(metadata.UserID),
		ExternalUserID: conv.ToPGTextEmpty(metadata.UserEmail),
		Title:          conv.ToPGText(s.defaultChatTitleForSession(ctx, sessionID)),
	}); err != nil {
		return fmt.Errorf("ensure claude code chat exists: %w", err)
	}

	params := make([]chatRepo.CreateExternalChatMessageParams, 0, len(payload.Messages))
	hasAssistant := false
	for _, msg := range payload.Messages {
		if msg == nil || strings.TrimSpace(msg.ExternalID) == "" {
			continue
		}

		var toolCalls []byte
		finishReason := ""
		if msg.ToolCalls != nil {
			if b, mErr := json.Marshal(msg.ToolCalls); mErr == nil && len(b) > 0 && string(b) != "null" {
				toolCalls = b
				finishReason = "tool_calls"
			}
		}

		createdAt := time.Now()
		if msg.Timestamp != nil && *msg.Timestamp != "" {
			if t, pErr := time.Parse(time.RFC3339, *msg.Timestamp); pErr == nil {
				createdAt = t
			}
		}

		if msg.Role == "assistant" {
			hasAssistant = true
		}

		params = append(params, chatRepo.CreateExternalChatMessageParams{
			ChatID:            chatID,
			Role:              msg.Role,
			ProjectID:         projectID,
			Content:           conv.PtrValOr(msg.Content, ""),
			ContentRaw:        nil,
			ContentAssetUrl:   conv.ToPGTextEmpty(""),
			StorageError:      conv.ToPGTextEmpty(""),
			Model:             conv.ToPGTextEmpty(conv.PtrValOr(msg.Model, "")),
			MessageID:         conv.ToPGTextEmpty(""),
			ToolCallID:        conv.ToPGTextEmpty(conv.PtrValOr(msg.ToolCallID, "")),
			UserID:            conv.ToPGTextEmpty(metadata.UserID),
			ExternalUserID:    conv.ToPGTextEmpty(metadata.UserEmail),
			ExternalMessageID: conv.ToPGText(strings.TrimSpace(msg.ExternalID)),
			FinishReason:      conv.ToPGTextEmpty(finishReason),
			ToolCalls:         toolCalls,
			PromptTokens:      conv.PtrValOr(msg.PromptTokens, 0),
			CompletionTokens:  conv.PtrValOr(msg.CompletionTokens, 0),
			TotalTokens:       conv.PtrValOr(msg.TotalTokens, 0),
			Origin:            conv.ToPGTextEmpty(""),
			UserAgent:         conv.ToPGTextEmpty(""),
			IpAddress:         conv.ToPGTextEmpty(""),
			Source:            conv.ToPGTextEmpty(metadata.ServiceName),
			ContentHash:       nil,
			Generation:        0,
			CreatedAt:         pgtype.Timestamptz{Time: createdAt, Valid: true, InfinityModifier: pgtype.Finite},
		})
	}

	if len(params) == 0 {
		return nil
	}

	stored, err := s.writer.WriteExternal(ctx, projectID, params)
	if err != nil {
		return fmt.Errorf("write captured claude messages: %w", err)
	}

	logger.InfoContext(ctx, "captured claude transcript messages",
		attr.SlogEvent("claude_messages_captured"),
		attr.SlogOrganizationID(metadata.GramOrgID),
		attr.SlogProjectID(metadata.ProjectID),
		attr.SlogHookMessagesCaptured(int(stored)),
	)

	// New assistant content can change the inferred chat title; schedule a refresh
	// once per batch rather than per message.
	if hasAssistant && stored > 0 && s.chatTitleGenerator != nil {
		if err := s.chatTitleGenerator.ScheduleChatTitleGeneration(ctx, chatID.String(), metadata.GramOrgID, metadata.ProjectID); err != nil {
			logger.WarnContext(ctx, "failed to schedule chat title generation", attr.SlogError(err))
		}
	}

	return nil
}
