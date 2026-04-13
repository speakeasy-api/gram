package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// Cursor is the endpoint for Cursor hook events
func (s *Service) Cursor(ctx context.Context, payload *gen.CursorPayload) (*gen.CursorHookResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	s.logger.InfoContext(ctx, fmt.Sprintf("🪝 HOOK Cursor: %s", payload.HookEventName),
		attr.SlogEvent("cursor_hook"),
		attr.SlogValueAny(map[string]any{
			"hookEventName": payload.HookEventName,
			"toolName":      payload.ToolName,
		}),
	)

	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()

	// Record the hook (will route to ClickHouse for tool calls, PG for all events)
	s.recordCursorHook(ctx, payload, orgID, projectID)

	result := &gen.CursorHookResult{
		Permission:        nil,
		UserMessage:       nil,
		AdditionalContext: nil,
	}

	switch payload.HookEventName {
	case "preToolUse":
		result.Permission = new("allow")
	default:
		// nothing to do
	}

	return result, nil
}

func (s *Service) recordCursorHook(ctx context.Context, payload *gen.CursorPayload, orgID string, projectID string) {
	if payload.ConversationID == nil || *payload.ConversationID == "" {
		s.logger.WarnContext(ctx, "Cursor event called without conversation ID")
		return
	}

	metadata := &SessionMetadata{
		SessionID:   *payload.ConversationID,
		ServiceName: "Cursor",
		UserEmail:   conv.PtrValOr(payload.UserEmail, ""),
		ClaudeOrgID: "",
		GramOrgID:   orgID,
		ProjectID:   projectID,
	}

	s.persistCursorHook(ctx, payload, metadata)
}

func (s *Service) persistCursorHook(ctx context.Context, payload *gen.CursorPayload, metadata *SessionMetadata) {
	if isCursorConversationEvent(payload.HookEventName) {
		// Conversation events: PG only (user prompts and agent responses)
		var err error
		switch payload.HookEventName {
		case "beforeSubmitPrompt":
			err = s.persistCursorUserPrompt(ctx, payload, metadata)
		case "afterAgentResponse":
			err = s.persistCursorAgentResponse(ctx, payload, metadata)
		}
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to persist Cursor conversation event", attr.SlogError(err))
		}
	} else {
		// Tool call events: ClickHouse + PG
		if err := s.persistCursorToolCallEvent(ctx, payload, metadata); err != nil {
			s.logger.ErrorContext(ctx, "Failed to persist Cursor tool call event", attr.SlogError(err))
		}
	}
}

// persistCursorToolCallEvent writes tool call events to both ClickHouse and PostgreSQL
func (s *Service) persistCursorToolCallEvent(ctx context.Context, payload *gen.CursorPayload, metadata *SessionMetadata) error {
	// Write to ClickHouse for telemetry
	s.writeCursorHookToClickHouse(ctx, payload, metadata.GramOrgID, metadata.ProjectID)

	// Write to PostgreSQL for chat history
	switch payload.HookEventName {
	case "preToolUse":
		return s.writeCursorToolCallRequestToPG(ctx, payload, metadata)
	case "postToolUse", "postToolUseFailure":
		return s.writeCursorToolCallResultToPG(ctx, payload, metadata)
	}
	return nil
}

// isCursorConversationEvent returns true if the event is a conversation capture event (not a tool call).
func isCursorConversationEvent(eventName string) bool {
	switch eventName {
	case "beforeSubmitPrompt", "afterAgentResponse":
		return true
	default:
		return false
	}
}

// writeCursorHookToClickHouse writes a Cursor hook event directly to ClickHouse
// Unlike Claude hooks, Cursor payloads are already authenticated and include user_email,
// so no Redis buffering is needed.
func (s *Service) writeCursorHookToClickHouse(ctx context.Context, payload *gen.CursorPayload, orgID string, projectID string) {
	attrs := s.buildCursorTelemetryAttributes(ctx, payload, orgID, projectID)
	toolName, _ := attrs[attr.ToolNameKey].(string)

	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Invalid project ID for Cursor hook", attr.SlogError(err))
		return
	}

	toolInfo := telemetry.ToolInfo{
		Name:           toolName,
		OrganizationID: orgID,
		ProjectID:      parsedProjectID.String(),
		ID:             "",
		URN:            "",
		DeploymentID:   "",
		FunctionID:     nil,
	}

	if s.telemetryLogger != nil {
		s.telemetryLogger.Log(ctx, telemetry.LogParams{
			Timestamp:  time.Now(),
			ToolInfo:   toolInfo,
			Attributes: attrs,
		})

		s.logger.DebugContext(ctx, "Wrote Cursor hook to ClickHouse",
			attr.SlogEvent("cursor_hook_written"),
		)
	}
}

// buildCursorTelemetryAttributes creates attributes for a Cursor hook event
func (s *Service) buildCursorTelemetryAttributes(ctx context.Context, payload *gen.CursorPayload, orgID string, projectID string) map[attr.Key]any {
	toolName := ""
	if payload.ToolName != nil {
		toolName = *payload.ToolName
	}

	userEmail := ""
	if payload.UserEmail != nil {
		userEmail = *payload.UserEmail
	}

	// Normalize to PascalCase to match Claude convention for consistent ClickHouse queries
	hookEvent := payload.HookEventName
	switch hookEvent {
	case "preToolUse":
		hookEvent = "PreToolUse"
	case "postToolUse":
		hookEvent = "PostToolUse"
	case "postToolUseFailure":
		hookEvent = "PostToolUseFailure"
	case "beforeSubmitPrompt":
		hookEvent = "BeforeSubmitPrompt"
	case "afterAgentResponse":
		hookEvent = "AfterAgentResponse"
	case "stop":
		hookEvent = "Stop"
	}

	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.ToolNameKey:       toolName,
		attr.HookEventKey:      hookEvent,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        generateTraceID(),
		attr.LogBodyKey:        fmt.Sprintf("Hook: %s", hookEvent),
		attr.UserEmailKey:      userEmail,
		attr.ProjectIDKey:      projectID,
		attr.OrganizationIDKey: orgID,
		attr.HookSourceKey:     "cursor",
	}

	if payload.Error != nil {
		attrs[attr.HookErrorKey] = payload.Error
	}

	if payload.IsInterrupt != nil {
		attrs[attr.HookIsInterruptKey] = *payload.IsInterrupt
	}

	// Parse MCP tool names (same mcp__ prefix convention)
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.SplitN(toolName, "__", 3)
		if len(parts) == 3 {
			attrs[attr.ToolCallSourceKey] = parts[1]
			attrs[attr.ToolNameKey] = parts[2]
		}
	}

	if payload.ToolUseID != nil && *payload.ToolUseID != "" {
		attrs[attr.TraceIDKey] = hashToolCallIDToTraceID(*payload.ToolUseID)
	}
	if payload.ConversationID != nil {
		attrs[attr.GenAIConversationIDKey] = *payload.ConversationID
	}
	if payload.ToolUseID != nil {
		attrs[attr.GenAIToolCallIDKey] = *payload.ToolUseID
	}

	// Store prompt text as log body for beforeSubmitPrompt events only
	if payload.HookEventName == "beforeSubmitPrompt" && payload.Prompt != nil && *payload.Prompt != "" {
		attrs[attr.LogBodyKey] = *payload.Prompt
	}

	// Store token usage from stop events
	if payload.InputTokens != nil {
		attrs[attr.GenAIUsageInputTokensKey] = *payload.InputTokens
	}
	if payload.OutputTokens != nil {
		attrs[attr.GenAIUsageOutputTokensKey] = *payload.OutputTokens
	}

	// Stringify ToolInput and ToolResponse to prevent JSON path explosion in ClickHouse
	if payload.ToolInput != nil {
		if jsonBytes, err := json.Marshal(payload.ToolInput); err == nil {
			attrs[attr.GenAIToolCallArgumentsKey] = string(jsonBytes)
		} else {
			s.logger.WarnContext(ctx, "Failed to marshal Cursor ToolInput", attr.SlogError(err))
		}
	}
	if payload.ToolResponse != nil {
		if jsonBytes, err := json.Marshal(payload.ToolResponse); err == nil {
			attrs[attr.GenAIToolCallResultKey] = string(jsonBytes)
		} else {
			s.logger.WarnContext(ctx, "Failed to marshal Cursor ToolResponse", attr.SlogError(err))
		}
	}

	return attrs
}

// writeCursorToolCallRequestToPG writes a Cursor tool call request (preToolUse) to PostgreSQL.
func (s *Service) writeCursorToolCallRequestToPG(ctx context.Context, payload *gen.CursorPayload, metadata *SessionMetadata) error {
	if payload.ConversationID == nil || *payload.ConversationID == "" {
		return nil
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	chatID := sessionIDToUUID(*payload.ConversationID)

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
		Content:          "",
		Model:            conv.ToPGTextEmpty(conv.PtrValOr(payload.Model, "")),
		UserID:           conv.ToPGTextEmpty(""),
		Source:           conv.ToPGText("Cursor"),
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
	}

	return s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, activities.DefaultCursorChatTitle)
}

// writeCursorToolCallResultToPG writes a Cursor tool call result (postToolUse/postToolUseFailure) to PostgreSQL.
func (s *Service) writeCursorToolCallResultToPG(ctx context.Context, payload *gen.CursorPayload, metadata *SessionMetadata) error {
	if payload.ConversationID == nil || *payload.ConversationID == "" {
		return nil
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	chatID := sessionIDToUUID(*payload.ConversationID)

	var content string
	if payload.HookEventName == "postToolUse" && payload.ToolResponse != nil {
		content = marshalToJSON(payload.ToolResponse)
	} else if payload.HookEventName == "postToolUseFailure" && payload.Error != nil {
		content = marshalToJSON(payload.Error)
	} else {
		return nil
	}

	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "tool",
		Content:          content,
		UserID:           conv.ToPGTextEmpty(""),
		Source:           conv.ToPGText("Cursor"),
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
	}

	return s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, activities.DefaultCursorChatTitle)
}

// persistCursorAgentResponse writes the assistant's response text to PostgreSQL as a chat message.
func (s *Service) persistCursorAgentResponse(ctx context.Context, payload *gen.CursorPayload, metadata *SessionMetadata) error {
	if payload.ConversationID == nil || *payload.ConversationID == "" {
		return nil
	}

	content := conv.PtrValOr(payload.Text, "")
	if content == "" {
		return nil
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	chatID := sessionIDToUUID(*payload.ConversationID)

	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "assistant",
		Content:          content,
		Model:            conv.ToPGTextEmpty(conv.PtrValOr(payload.Model, "")),
		UserID:           conv.ToPGTextEmpty(""),
		Source:           conv.ToPGText("Cursor"),
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
	}

	if err := s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, activities.DefaultCursorChatTitle); err != nil {
		return err
	}

	if s.chatTitleGenerator != nil {
		if err := s.chatTitleGenerator.ScheduleChatTitleGeneration(
			context.WithoutCancel(ctx),
			chatID.String(),
			metadata.GramOrgID,
			metadata.ProjectID,
		); err != nil {
			s.logger.WarnContext(ctx, "failed to schedule chat title generation for Cursor", attr.SlogError(err))
		}
	}

	return nil
}

// persistCursorUserPrompt writes a Cursor user prompt to PostgreSQL.
func (s *Service) persistCursorUserPrompt(ctx context.Context, payload *gen.CursorPayload, metadata *SessionMetadata) error {
	if payload.ConversationID == nil || *payload.ConversationID == "" {
		s.logger.WarnContext(ctx, "Cursor user prompt missing conversation_id")
		return nil
	}

	parsedProjectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID for Cursor user prompt: %w", err)
	}

	chatID := sessionIDToUUID(*payload.ConversationID)

	content := conv.PtrValOr(payload.Prompt, "")
	if content == "" {
		return nil
	}

	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        parsedProjectID,
		Role:             "user",
		Content:          content,
		Model:            conv.ToPGTextEmpty(""),
		UserID:           conv.ToPGTextEmpty(""),
		Source:           conv.ToPGText("Cursor"),
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
	}

	return s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, parsedProjectID, msgParams, activities.DefaultCursorChatTitle)
}
