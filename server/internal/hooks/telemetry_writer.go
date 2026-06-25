package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/toolref"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type WriteOptions struct {
	BlockReason string
	SkipChat    bool
}

// TelemetryWriter is the hook-specific write boundary. It fans a canonical
// hook event into ClickHouse telemetry and chat storage from one type switch.
type TelemetryWriter struct {
	logger             *slog.Logger
	db                 *pgxpool.Pool
	cache              cache.Cache
	telemetryLogger    *telemetry.Logger
	chatWriter         *chat.ChatMessageWriter
	productFeatures    ProductFeaturesClient
	chatTitleGenerator ChatTitleGenerator
}

func NewTelemetryWriter(
	logger *slog.Logger,
	db *pgxpool.Pool,
	cacheAdapter cache.Cache,
	telemetryLogger *telemetry.Logger,
	chatWriter *chat.ChatMessageWriter,
	productFeatures ProductFeaturesClient,
	chatTitleGenerator ChatTitleGenerator,
) *TelemetryWriter {
	return &TelemetryWriter{
		logger:             logger.With(attr.SlogComponent("hook_telemetry_writer")),
		db:                 db,
		cache:              cacheAdapter,
		telemetryLogger:    telemetryLogger,
		chatWriter:         chatWriter,
		productFeatures:    productFeatures,
		chatTitleGenerator: chatTitleGenerator,
	}
}

func (w *TelemetryWriter) Write(ctx context.Context, ev any, metadata *SessionMetadata, opts WriteOptions) error {
	if ev == nil || metadata == nil {
		return nil
	}

	md := *metadata
	md.UserEmail = strings.TrimSpace(md.UserEmail)
	if md.UserID == "" && md.UserEmail != "" {
		md.UserID = w.resolveUserByEmail(ctx, md.UserEmail, md.GramOrgID)
	}

	var eg errgroup.Group
	eg.Go(func() error {
		if err := w.writeTelemetry(ctx, ev, &md, opts.BlockReason); err != nil {
			return fmt.Errorf("write hook telemetry: %w", err)
		}
		return nil
	})
	if !opts.SkipChat {
		eg.Go(func() error {
			if err := w.writeChatProjection(ctx, ev, &md); err != nil {
				return fmt.Errorf("write hook chat projection: %w", err)
			}
			return nil
		})
	}
	return eg.Wait()
}

func (w *TelemetryWriter) writeTelemetry(ctx context.Context, ev any, metadata *SessionMetadata, blockReason string) error {
	if w.telemetryLogger == nil {
		return nil
	}

	event, ok := canonicalEvent(ev)
	if !ok {
		return nil
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("parse project id: %w", err)
	}

	attrs, toolName := w.telemetryAttributes(ctx, ev, event, metadata, blockReason)
	timestamp := event.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	w.telemetryLogger.Log(ctx, telemetry.LogParams{
		Timestamp: timestamp,
		ToolInfo: telemetry.ToolInfo{
			Name:           toolName,
			OrganizationID: metadata.GramOrgID,
			ProjectID:      projectID.String(),
			ID:             "",
			URN:            "",
			DeploymentID:   "",
			FunctionID:     nil,
		},
		UserInfo:   telemetry.UserInfoByIDAndEmail(metadata.UserID, metadata.UserEmail),
		Attributes: attrs,
	})
	return nil
}

func (w *TelemetryWriter) telemetryAttributes(ctx context.Context, ev any, event hookevents.Event, metadata *SessionMetadata, blockReason string) (map[attr.Key]any, string) {
	hookEventName := persistedHookEventName(event)
	toolName := hookToolName(ev)
	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.ToolNameKey:       toolName,
		attr.HookEventKey:      hookEventName,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        generateTraceID(),
		attr.LogBodyKey:        hookLogBody(hookEventName, toolName),
		attr.ProjectIDKey:      metadata.ProjectID,
		attr.OrganizationIDKey: metadata.GramOrgID,
		attr.HookSourceKey:     hookSource(event, metadata),
	}
	if blockReason != "" {
		attrs[attr.HookBlockReasonKey] = blockReason
	}
	if event.ConversationID != "" {
		attrs[attr.GenAIConversationIDKey] = event.ConversationID
	}
	if event.Model != "" {
		attrs[attr.GenAIResponseModelKey] = event.Model
	}
	if event.HookHostname != "" {
		attrs[attr.HookHostnameKey] = event.HookHostname
	}
	if correlationID := toolCorrelationID(ev, event); correlationID != "" {
		attrs[attr.TraceIDKey] = hashToolCallIDToTraceID(correlationID)
		attrs[attr.GenAIToolCallIDKey] = correlationID
	}

	w.applyTypedTelemetryAttrs(ctx, attrs, ev)
	if event.Provider == hookevents.ProviderClaude {
		w.applyClaudeToolAttrs(ctx, attrs, event, hookToolName(ev))
	}
	w.applyRawTelemetryAttrs(ctx, attrs, ev, event, metadata)

	if storedToolName, ok := attrs[attr.ToolNameKey].(string); ok {
		toolName = storedToolName
	}
	return attrs, toolName
}

func (w *TelemetryWriter) applyTypedTelemetryAttrs(ctx context.Context, attrs map[attr.Key]any, ev any) {
	switch ev := ev.(type) {
	case *hookevents.BeforeToolUse:
		setStringified(ctx, attrs, attr.GenAIToolCallArgumentsKey, ev.ToolInput, w.logger, "marshal hook tool input")
	case *hookevents.BeforeMCPExecution:
		setStringified(ctx, attrs, attr.GenAIToolCallArgumentsKey, ev.ToolInput, w.logger, "marshal hook mcp input")
	case *hookevents.PermissionRequest:
		setStringified(ctx, attrs, attr.GenAIToolCallArgumentsKey, ev.ToolInput, w.logger, "marshal hook permission input")
	case *hookevents.AfterToolUse:
		setStringified(ctx, attrs, attr.GenAIToolCallResultKey, ev.ToolOutput, w.logger, "marshal hook tool output")
	case *hookevents.AfterMCPExecution:
		setStringified(ctx, attrs, attr.GenAIToolCallResultKey, ev.ToolOutput, w.logger, "marshal hook mcp output")
	case *hookevents.AfterToolUseFailure:
		attrs[attr.HookErrorKey] = ev.Error
		attrs[attr.HookIsInterruptKey] = ev.IsInterrupt
		setStringified(ctx, attrs, attr.GenAIToolCallResultKey, ev.Error, w.logger, "marshal hook tool error")
	case *hookevents.UserPromptSubmit:
		if ev.Prompt != "" {
			attrs[attr.LogBodyKey] = ev.Prompt
		}
	case *hookevents.AfterAgentResponse:
		if ev.Text != "" {
			attrs[attr.LogBodyKey] = ev.Text
		}
	case *hookevents.Stop:
		if ev.LastAssistantMessage != "" {
			attrs[attr.LogBodyKey] = ev.LastAssistantMessage
		}
	}
}

func (w *TelemetryWriter) applyRawTelemetryAttrs(ctx context.Context, attrs map[attr.Key]any, ev any, event hookevents.Event, metadata *SessionMetadata) {
	switch payload := event.Raw.(type) {
	case *gen.CursorPayload:
		applyHookHostnameAttr(attrs, payload.HookHostname)
		if payload.Model != nil && *payload.Model != "" {
			attrs[attr.GenAIResponseModelKey] = *payload.Model
		}
		if payload.InputTokens != nil {
			attrs[attr.GenAIUsageInputTokensKey] = *payload.InputTokens
		}
		if payload.OutputTokens != nil {
			attrs[attr.GenAIUsageOutputTokensKey] = *payload.OutputTokens
		}
		if correlationID := cursorToolCorrelationID(payload); correlationID != "" {
			attrs[attr.TraceIDKey] = hashToolCallIDToTraceID(correlationID)
			attrs[attr.GenAIToolCallIDKey] = correlationID
		}
		w.applyCursorToolAttrs(attrs, payload)
	case *gen.CodexPayload:
		if payload.Model != nil && *payload.Model != "" {
			attrs[attr.GenAIResponseModelKey] = *payload.Model
		}
		if metadata.SessionID != "" {
			attrs[attr.GenAIConversationIDKey] = metadata.SessionID
			attrs[attr.TraceIDKey] = hashToolCallIDToTraceID(metadata.SessionID)
		}
		w.applyCodexToolAttrs(ctx, attrs, payload, metadata)
	default:
		_ = ev
	}
}

func (w *TelemetryWriter) applyClaudeToolAttrs(ctx context.Context, attrs map[attr.Key]any, event hookevents.Event, toolName string) {
	if server, fn, ok := toolref.AttributeTool(toolName); ok {
		attrs[attr.ToolCallSourceKey] = server
		attrs[attr.ToolNameKey] = fn
	}
	if parsed := parseClaudeToolName(toolName); parsed.IsMCP && event.ConversationID != "" {
		if entries, err := w.getCachedMCPList(ctx, event.ConversationID); err == nil {
			matched := matchCachedMCPEntry(entries, parsed.Server)
			if v := resolvedMCPMatch(matched, parsed.Server); v != "" {
				attrs[attr.MCPMatchKey] = v
			}
			applyMCPInventoryAttrs(attrs, matched)
		}
	}
}

func (w *TelemetryWriter) applyCursorToolAttrs(attrs map[attr.Key]any, payload *gen.CursorPayload) {
	toolName := conv.PtrValOr(payload.ToolName, "")
	hookEvent, ok := parseCursorHookEvent(payload.HookEventName)
	if strings.HasPrefix(toolName, "mcp__") {
		if server, fn, ok := toolref.AttributeTool(toolName); ok {
			attrs[attr.ToolCallSourceKey] = server
			attrs[attr.ToolNameKey] = fn
		}
	}
	if ok && (hookEvent == HookEventBeforeMCPExecution || hookEvent == HookEventAfterMCPExecution) {
		if source := cursorMCPToolSource(payload); source != "" {
			attrs[attr.ToolCallSourceKey] = source
		}
		if stripped, ok := strings.CutPrefix(toolName, "MCP:"); ok {
			attrs[attr.ToolNameKey] = stripped
		}
		if payload.URL != nil && *payload.URL != "" {
			attrs[attr.MCPServerURLKey] = *payload.URL
		}
	}
	if payload.ResultJSON != nil && *payload.ResultJSON != "" {
		attrs[attr.GenAIToolCallResultKey] = *payload.ResultJSON
		var parsed struct {
			IsError bool `json:"isError"`
		}
		if err := json.Unmarshal([]byte(*payload.ResultJSON), &parsed); err == nil && parsed.IsError {
			attrs[attr.HookErrorKey] = *payload.ResultJSON
		}
	}
}

func (w *TelemetryWriter) applyCodexToolAttrs(ctx context.Context, attrs map[attr.Key]any, payload *gen.CodexPayload, metadata *SessionMetadata) {
	toolName := conv.PtrValOr(payload.ToolName, "")
	if server, fn, ok := toolref.AttributeTool(toolName); ok {
		attrs[attr.ToolCallSourceKey] = server
		attrs[attr.ToolNameKey] = fn
		if metadata.SessionID != "" {
			if entries, err := w.getCachedMCPList(ctx, metadata.SessionID); err == nil {
				matched := matchCodexCachedMCPEntry(entries, toolName)
				if matched != nil && matched.ToolPrefix != "" {
					if rest, ok := strings.CutPrefix(toolName, "mcp__"+matched.ToolPrefix+"__"); ok {
						attrs[attr.ToolCallSourceKey] = matched.ToolPrefix
						attrs[attr.ToolNameKey] = rest
					}
				}
				if v := resolvedMCPMatch(matched, server); v != "" {
					attrs[attr.MCPMatchKey] = v
				}
				applyMCPInventoryAttrs(attrs, matched)
			}
		}
	}
}

func (w *TelemetryWriter) writeChatProjection(ctx context.Context, ev any, metadata *SessionMetadata) error {
	event, ok := canonicalEvent(ev)
	if !ok {
		return nil
	}
	sessionID := conv.Default(event.ConversationID, metadata.SessionID)
	if sessionID == "" {
		return nil
	}
	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("parse project id: %w", err)
	}
	chatID := sessionIDToUUID(sessionID)

	switch ev := ev.(type) {
	case *hookevents.BeforeToolUse:
		return w.writeToolCallRequest(ctx, event, metadata, chatID, projectID, toolCorrelationID(ev, event), ev.ToolName, ev.ToolInput)
	case *hookevents.BeforeMCPExecution:
		return w.writeToolCallRequest(ctx, event, metadata, chatID, projectID, toolCorrelationID(ev, event), ev.ToolName, ev.ToolInput)
	case *hookevents.AfterToolUse:
		return w.writeToolResult(ctx, event, metadata, chatID, projectID, toolCorrelationID(ev, event), ev.ToolOutput)
	case *hookevents.AfterMCPExecution:
		return w.writeToolResult(ctx, event, metadata, chatID, projectID, toolCorrelationID(ev, event), afterMCPExecutionOutput(ev))
	case *hookevents.AfterToolUseFailure:
		return w.writeToolResult(ctx, event, metadata, chatID, projectID, toolCorrelationID(ev, event), ev.Error)
	case *hookevents.UserPromptSubmit:
		return w.writeUserMessage(ctx, event, metadata, chatID, projectID, ev.Prompt)
	case *hookevents.AfterAgentResponse:
		return w.writeAssistantMessage(ctx, event, metadata, chatID, projectID, ev.Text)
	case *hookevents.Stop:
		if event.Provider == hookevents.ProviderClaude {
			if err := w.backfillLastUserPromptID(ctx, chatID, projectID, event.AdditionalData); err != nil {
				w.logger.WarnContext(ctx, "failed to backfill Claude user prompt ID",
					attr.SlogError(err),
					attr.SlogGenAIConversationID(event.ConversationID),
					attr.SlogProjectID(metadata.ProjectID),
				)
			}
		}
		return w.writeAssistantMessage(ctx, event, metadata, chatID, projectID, ev.LastAssistantMessage)
	default:
		return nil
	}
}

func (w *TelemetryWriter) writeToolCallRequest(ctx context.Context, event hookevents.Event, metadata *SessionMetadata, chatID, projectID uuid.UUID, toolCallID, toolName string, toolInput any) error {
	toolCalls := []map[string]any{{
		"id":   toolCallID,
		"type": "function",
		"function": map[string]any{
			"name":      toolName,
			"arguments": marshalToJSON(toolInput),
		},
	}}
	toolCallsJSON, err := json.Marshal(toolCalls)
	if err != nil {
		return fmt.Errorf("marshal tool calls: %w", err)
	}
	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "assistant",
		Content:          "",
		Model:            conv.ToPGTextEmpty(event.Model),
		UserID:           conv.ToPGTextEmpty(metadata.UserID),
		Source:           conv.ToPGText(chatSource(event, metadata)),
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		ContentRaw:       nil,
		ContentAssetUrl:  conv.ToPGTextEmpty(""),
		StorageError:     conv.ToPGTextEmpty(""),
		MessageID:        conv.ToPGTextEmpty(""),
		ToolCallID:       conv.ToPGTextEmpty(""),
		ExternalUserID:   conv.ToPGTextEmpty(metadata.UserEmail),
		FinishReason:     conv.ToPGText("tool_calls"),
		ToolCalls:        toolCallsJSON,
		Origin:           conv.ToPGTextEmpty(""),
		UserAgent:        conv.ToPGTextEmpty(""),
		IpAddress:        conv.ToPGTextEmpty(""),
		ContentHash:      nil,
		Generation:       0,
	}
	return w.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, w.defaultChatTitleForEvent(ctx, event))
}

func (w *TelemetryWriter) writeToolResult(ctx context.Context, event hookevents.Event, metadata *SessionMetadata, chatID, projectID uuid.UUID, toolCallID string, output any) error {
	content := marshalToJSON(output)
	if content == "" {
		return nil
	}
	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "tool",
		Content:          content,
		Model:            conv.ToPGTextEmpty(""),
		UserID:           conv.ToPGTextEmpty(metadata.UserID),
		Source:           conv.ToPGText(chatSource(event, metadata)),
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		ContentRaw:       nil,
		ContentAssetUrl:  conv.ToPGTextEmpty(""),
		StorageError:     conv.ToPGTextEmpty(""),
		MessageID:        conv.ToPGTextEmpty(""),
		ToolCallID:       conv.ToPGTextEmpty(toolCallID),
		ExternalUserID:   conv.ToPGTextEmpty(metadata.UserEmail),
		FinishReason:     conv.ToPGTextEmpty(""),
		ToolCalls:        nil,
		Origin:           conv.ToPGTextEmpty(""),
		UserAgent:        conv.ToPGTextEmpty(""),
		IpAddress:        conv.ToPGTextEmpty(""),
		ContentHash:      nil,
		Generation:       0,
	}
	return w.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, w.defaultChatTitleForEvent(ctx, event))
}

func (w *TelemetryWriter) writeUserMessage(ctx context.Context, event hookevents.Event, metadata *SessionMetadata, chatID, projectID uuid.UUID, content string) error {
	if content == "" {
		return nil
	}
	msgParams := w.baseChatMessageParams(event, metadata, chatID, projectID, "user", content)
	return w.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, w.defaultChatTitleForEvent(ctx, event))
}

func (w *TelemetryWriter) writeAssistantMessage(ctx context.Context, event hookevents.Event, metadata *SessionMetadata, chatID, projectID uuid.UUID, content string) error {
	if content == "" {
		return nil
	}
	msgParams := w.baseChatMessageParams(event, metadata, chatID, projectID, "assistant", content)
	if err := w.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, w.defaultChatTitleForEvent(ctx, event)); err != nil {
		return err
	}
	if w.chatTitleGenerator != nil {
		if err := w.chatTitleGenerator.ScheduleChatTitleGeneration(
			context.WithoutCancel(ctx),
			chatID.String(),
			metadata.GramOrgID,
			projectID.String(),
		); err != nil {
			w.logger.WarnContext(ctx, "failed to schedule chat title generation", attr.SlogError(err))
		}
	}
	return nil
}

func (w *TelemetryWriter) baseChatMessageParams(event hookevents.Event, metadata *SessionMetadata, chatID, projectID uuid.UUID, role, content string) chatRepo.CreateChatMessageParams {
	return chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             role,
		Content:          content,
		Model:            conv.ToPGTextEmpty(event.Model),
		UserID:           conv.ToPGTextEmpty(metadata.UserID),
		Source:           conv.ToPGText(chatSource(event, metadata)),
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
}

func (w *TelemetryWriter) insertMessageWithFallbackUpsert(
	ctx context.Context,
	metadata *SessionMetadata,
	chatID uuid.UUID,
	projectID uuid.UUID,
	msgParams chatRepo.CreateChatMessageParams,
	defaultTitle string,
) error {
	if w.productFeatures == nil || w.chatWriter == nil {
		return nil
	}

	enabled, err := w.productFeatures.IsFeatureEnabled(ctx, metadata.GramOrgID, productfeatures.FeatureSessionCapture)
	if err != nil {
		return fmt.Errorf("check session_capture feature flag: %w", err)
	}
	if !enabled {
		w.logger.DebugContext(ctx, "session capture disabled; skipping hook chat persistence",
			attr.SlogEvent("hook_session_capture_disabled"),
			attr.SlogOrganizationID(metadata.GramOrgID),
			attr.SlogProjectID(projectID.String()),
			attr.SlogGenAIConversationID(metadata.SessionID),
		)
		return nil
	}

	_, err = w.chatWriter.Write(ctx, projectID, []chatRepo.CreateChatMessageParams{msgParams})
	if err == nil {
		return nil
	}
	if !isForeignKeyViolation(err) {
		return fmt.Errorf("insert chat message: %w", err)
	}

	_, upsertErr := repo.New(w.db).UpsertClaudeCodeSession(ctx, repo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: metadata.GramOrgID,
		UserID:         conv.ToPGTextEmpty(metadata.UserID),
		ExternalUserID: conv.ToPGTextEmpty(metadata.UserEmail),
		Title:          conv.ToPGText(defaultTitle),
	})
	if upsertErr != nil {
		return fmt.Errorf("upsert hook session after FK violation: %w", upsertErr)
	}

	if _, err = w.chatWriter.Write(ctx, projectID, []chatRepo.CreateChatMessageParams{msgParams}); err != nil {
		return fmt.Errorf("insert chat message after creating chat: %w", err)
	}
	return nil
}

func (w *TelemetryWriter) defaultChatTitleForEvent(ctx context.Context, event hookevents.Event) string {
	switch event.Provider {
	case hookevents.ProviderCursor:
		return activities.DefaultCursorChatTitle
	case hookevents.ProviderCodex:
		return activities.DefaultCodexChatTitle
	case hookevents.ProviderClaude:
		return w.defaultClaudeChatTitleForSession(ctx, event.ConversationID)
	default:
		return activities.DefaultClaudeAmbiguous
	}
}

func (w *TelemetryWriter) defaultClaudeChatTitleForSession(ctx context.Context, sessionID string) string {
	if sessionID == "" || w.cache == nil {
		return activities.DefaultClaudeAmbiguous
	}
	var variant string
	if err := w.cache.Get(ctx, sessionAgentVariantCacheKey(sessionID), &variant); err != nil {
		return activities.DefaultClaudeAmbiguous
	}
	switch variant {
	case agentVariantCowork:
		return activities.DefaultCoworkChatTitle
	case agentVariantClaudeCode:
		return activities.DefaultClaudeChatTitle
	default:
		return activities.DefaultClaudeAmbiguous
	}
}

func (w *TelemetryWriter) backfillLastUserPromptID(ctx context.Context, chatID uuid.UUID, projectID uuid.UUID, additionalData map[string]any) error {
	lastUserPromptID := claudeLastUserPromptIDFromAdditionalData(additionalData)
	if lastUserPromptID == "" {
		return nil
	}
	_, err := repo.New(w.db).BackfillLatestClaudeUserMessagePromptID(ctx, repo.BackfillLatestClaudeUserMessagePromptIDParams{
		ChatID:    chatID,
		ProjectID: projectID,
		MessageID: conv.ToPGText(lastUserPromptID),
	})
	if err != nil {
		return fmt.Errorf("backfill latest Claude user message prompt ID: %w", err)
	}
	return nil
}

func (w *TelemetryWriter) resolveUserByEmail(ctx context.Context, email, orgID string) string {
	lookup := conv.NormalizeEmail(email)
	if lookup == "" {
		return ""
	}
	user, err := usersrepo.New(w.db).GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
		Email:          lookup,
		OrganizationID: orgID,
	})
	if err == nil {
		return user.ID
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		w.logger.WarnContext(ctx, "failed to resolve hook user by email",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
			attr.SlogAuthUserEmail(email),
		)
	}
	return ""
}

func (w *TelemetryWriter) getCachedMCPList(ctx context.Context, sessionID string) ([]MCPServerEntry, error) {
	var entries []MCPServerEntry
	if w.cache == nil {
		return entries, errors.New("cache is not configured")
	}
	if err := w.cache.Get(ctx, sessionMCPListCacheKey(sessionID), &entries); err != nil {
		return entries, fmt.Errorf("get cached MCP list: %w", err)
	}
	return entries, nil
}

func canonicalEvent(ev any) (hookevents.Event, bool) {
	switch ev := ev.(type) {
	case *hookevents.SessionStart:
		return ev.Event, true
	case *hookevents.ConfigChange:
		return ev.Event, true
	case *hookevents.BeforeToolUse:
		return ev.Event, true
	case *hookevents.BeforeMCPExecution:
		return ev.Event, true
	case *hookevents.AfterToolUse:
		return ev.Event, true
	case *hookevents.AfterToolUseFailure:
		return ev.Event, true
	case *hookevents.AfterMCPExecution:
		return ev.Event, true
	case *hookevents.PermissionRequest:
		return ev.Event, true
	case *hookevents.UserPromptSubmit:
		return ev.Event, true
	case *hookevents.AfterAgentResponse:
		return ev.Event, true
	case *hookevents.AfterAgentThought:
		return ev.Event, true
	case *hookevents.Stop:
		return ev.Event, true
	case *hookevents.SessionEnd:
		return ev.Event, true
	case *hookevents.Notification:
		return ev.Event, true
	default:
		return hookevents.Event{
			BaseEvent: hookevents.BaseEvent{
				Provider:     "",
				Type:         "",
				RawEventType: "",
				Timestamp:    time.Time{},
				AuthContext:  nil,
				Context:      hookevents.EventContext{OrganizationID: "", ProjectID: uuid.Nil, User: hookevents.User{ID: "", Email: ""}},
				Raw:          nil,
			},
			ConversationID: "",
			TranscriptPath: "",
			CWD:            "",
			PermissionMode: "",
			Model:          "",
			HookHostname:   "",
			AdditionalData: nil,
		}, false
	}
}

func hookToolName(ev any) string {
	switch ev := ev.(type) {
	case *hookevents.BeforeToolUse:
		return ev.ToolName
	case *hookevents.BeforeMCPExecution:
		return ev.ToolName
	case *hookevents.AfterToolUse:
		return ev.ToolName
	case *hookevents.AfterToolUseFailure:
		return ev.ToolName
	case *hookevents.AfterMCPExecution:
		return ev.ToolName
	case *hookevents.PermissionRequest:
		return ev.ToolName
	default:
		return ""
	}
}

func persistedHookEventName(event hookevents.Event) string {
	if event.Provider == hookevents.ProviderCursor {
		if hookEvent, ok := parseCursorHookEvent(event.RawEventType); ok {
			return string(hookEvent)
		}
	}
	return event.RawEventType
}

func hookSource(event hookevents.Event, metadata *SessionMetadata) string {
	switch event.Provider {
	case hookevents.ProviderClaude:
		return conv.Default(metadata.ServiceName, "claude")
	case hookevents.ProviderCursor:
		return "cursor"
	case hookevents.ProviderCodex:
		return "codex"
	default:
		return string(event.Provider)
	}
}

func chatSource(event hookevents.Event, metadata *SessionMetadata) string {
	if metadata.ServiceName != "" {
		return metadata.ServiceName
	}
	switch event.Provider {
	case hookevents.ProviderCursor:
		return "Cursor"
	case hookevents.ProviderCodex:
		return "Codex"
	case hookevents.ProviderClaude:
		return "Claude"
	default:
		return string(event.Provider)
	}
}

func hookLogBody(hookEventName, toolName string) string {
	if toolName == "" {
		return fmt.Sprintf("Hook: %s", hookEventName)
	}
	return fmt.Sprintf("Tool: %s, Hook: %s", toolName, hookEventName)
}

func setStringified(ctx context.Context, attrs map[attr.Key]any, key attr.Key, value any, logger *slog.Logger, message string) {
	if value == nil {
		return
	}
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		logger.WarnContext(ctx, message, attr.SlogError(err))
		return
	}
	attrs[key] = string(jsonBytes)
}

func toolCorrelationID(ev any, event hookevents.Event) string {
	switch ev := ev.(type) {
	case *hookevents.BeforeToolUse:
		if ev.ToolCallID != "" {
			return ev.ToolCallID
		}
	case *hookevents.BeforeMCPExecution:
		if ev.ToolCallID != "" {
			return ev.ToolCallID
		}
	case *hookevents.AfterToolUse:
		if ev.ToolCallID != "" {
			return ev.ToolCallID
		}
	case *hookevents.AfterToolUseFailure:
		if ev.ToolCallID != "" {
			return ev.ToolCallID
		}
	case *hookevents.AfterMCPExecution:
		if ev.ToolCallID != "" {
			return ev.ToolCallID
		}
	case *hookevents.PermissionRequest:
		if ev.ToolCallID != "" {
			return ev.ToolCallID
		}
	}
	switch payload := event.Raw.(type) {
	case *gen.CursorPayload:
		return cursorToolCorrelationID(payload)
	case *gen.CodexPayload:
		return conv.PtrValOr(payload.ToolName, "")
	default:
		return ""
	}
}

func afterMCPExecutionOutput(ev *hookevents.AfterMCPExecution) any {
	if payload, ok := ev.Raw.(*gen.CursorPayload); ok {
		if payload.ResultJSON != nil && *payload.ResultJSON != "" {
			return *payload.ResultJSON
		}
	}
	return ev.ToolOutput
}
