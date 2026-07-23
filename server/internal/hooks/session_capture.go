package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// ErrChatNotFound indicates the chat (conversation) does not exist.
var ErrChatNotFound = errors.New("chat not found")

// isForeignKeyViolation checks if the error is a PostgreSQL foreign key constraint violation.
// This indicates that the referenced chat does not exist.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgerrcode.ForeignKeyViolation
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

// defaultChatTitleForSession picks the default chat title based on the
// session's resolved product surface. If the surface is unknown (no OTEL
// service.name or SessionStart variant on file yet) we fall back to the
// ambiguous "Claude Session" title rather than assuming claude-code — the
// title generator will replace it with a real one once enough conversation
// is on file.
func (s *Service) defaultChatTitleForSession(ctx context.Context, metadata *SessionMetadata) string {
	switch s.claudeSessionSurface(ctx, metadata) {
	case agentVariantCowork:
		return activities.DefaultCoworkChatTitle
	case agentVariantClaudeCode, surfaceClaudeCodeDesktop:
		return activities.DefaultClaudeChatTitle
	default:
		return activities.DefaultClaudeAmbiguous
	}
}

// claudeSurfaceFromServiceName maps a reported service name or hook adapter
// slug to the canonical Claude product surface: "cowork", "claude-code-desktop"
// (CCD), or "claude-code" (the CLI). The OTEL resource service.name is the
// source of truth where it disambiguates: cowork self-identifies on it, while
// the CLI and CCD both report "claude-code" — CCD is identified by the desktop
// hook client's adapter slug instead. Returns "" when the value identifies no
// Claude surface (Cursor, Codex, unknown adapters).
func claudeSurfaceFromServiceName(name string) string {
	switch n := strings.ToLower(strings.TrimSpace(name)); {
	case strings.Contains(n, "cowork"):
		return agentVariantCowork
	case n == surfaceClaudeCodeDesktop:
		return surfaceClaudeCodeDesktop
	case n == "claude-code" || n == "claudecode":
		return agentVariantClaudeCode
	default:
		return ""
	}
}

// claudeServiceNameSpecificity ranks how precisely a service name or adapter
// slug identifies the product surface. "cowork" is unambiguous; the desktop
// adapter slug narrows to CCD; "claude-code" is the OTEL name shared by the
// CLI and CCD, so it is the least specific Claude value. Non-Claude values
// rank zero.
func claudeServiceNameSpecificity(name string) int {
	switch claudeSurfaceFromServiceName(name) {
	case agentVariantCowork:
		return 3
	case surfaceClaudeCodeDesktop:
		return 2
	case agentVariantClaudeCode:
		return 1
	default:
		return 0
	}
}

// preferClaudeServiceName merges a freshly reported service name (or adapter
// slug) with the session's previously cached one, keeping whichever identifies
// the Claude product surface more precisely. This is what lets the two signals
// compose: the OTEL stream's "cowork" upgrades a cached desktop adapter slug,
// while a cached "claude-code-desktop" survives OTEL batches that only report
// the ambiguous "claude-code". Ties keep the fresh value. A non-empty incoming
// value that identifies no Claude surface (Cursor, Codex, unknown adapters)
// always wins: non-Claude senders keep their reported name instead of being
// overwritten by a Claude value cached under the same session id.
func preferClaudeServiceName(incoming, cached string) string {
	if incoming == "" {
		return cached
	}
	if claudeSurfaceFromServiceName(incoming) == "" {
		return incoming
	}
	if claudeServiceNameSpecificity(cached) > claudeServiceNameSpecificity(incoming) {
		return cached
	}
	return incoming
}

// claudeSessionSurface resolves the product surface for a session from the
// service name carried on SessionMetadata (the OTEL service.name once the
// session's log stream has been seen, the hook adapter slug before then) with
// the inventory-shape variant stamped at SessionStart as fallback — it covers
// cowork builds that predate the cowork service.name and sessions whose OTEL
// stream has not arrived yet. Values that identify no Claude surface pass
// through unchanged so non-Claude senders keep their reported name.
func (s *Service) claudeSessionSurface(ctx context.Context, metadata *SessionMetadata) string {
	surface := claudeSurfaceFromServiceName(metadata.ServiceName)
	if surface == agentVariantCowork {
		return surface
	}
	variant := s.sessionAgentVariant(ctx, metadata.SessionID)
	if variant == agentVariantCowork {
		return agentVariantCowork
	}
	if surface != "" {
		return surface
	}
	if variant != "" {
		return variant
	}
	return metadata.ServiceName
}

// sessionAgentVariant returns the agent variant ("cowork" or "claude-code")
// stamped into the cache by SessionStart, or "" when none is cached (no
// SessionStart processed yet, or a cache miss). Callers should treat "" as an
// ambiguous Claude session rather than assuming claude-code.
func (s *Service) sessionAgentVariant(ctx context.Context, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	var variant string
	if err := s.cache.Get(ctx, sessionAgentVariantCacheKey(sessionID), &variant); err != nil {
		return ""
	}
	return variant
}

// sessionIDToUUID converts an agent session_id string to the chat id its
// transcript is persisted under. Every hook capture path goes through here, and
// the mapping itself lives in the chat package so consumers that read sessions
// back — efficacy scoring, telemetry — resolve the same chat.
func sessionIDToUUID(sessionID string) uuid.UUID {
	return chat.SessionIDToChatID(sessionID)
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
		Decision:           nil,
		Reason:             nil,
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

// constructBlockResponse builds a hook result that blocks the current event
// using the JSON shape Claude Code expects for the given hook. Per
// https://code.claude.com/docs/en/hooks#decision-control:
//
//   - UserPromptSubmit / PostToolUse / Stop / SubagentStop: top-level
//     `decision: "block"` + free-text `reason`. The reason is surfaced to
//     the user (UserPromptSubmit) or to Claude (PostToolUse / Stop).
//   - PreToolUse: nested `hookSpecificOutput.permissionDecision: "deny"`
//   - `permissionDecisionReason`. Top-level `decision` is rejected.
//
// Other events (SessionStart, SessionEnd, Notification, PostToolUseFailure)
// cannot block at all and must not be passed in.
func constructBlockResponse(hookEventName, reason string) *gen.ClaudeHookResult {
	result := makeHookResult(hookEventName)
	if hookEventName == "PreToolUse" {
		deny := "deny"
		if output, ok := result.HookSpecificOutput.(*HookSpecificOutput); ok {
			output.PermissionDecision = &deny
			output.PermissionDecisionReason = &reason
		}
		// systemMessage renders as a warning in the user's terminal;
		// permissionDecisionReason is what Claude itself sees and may quote
		// back. Set both so the user gets visible feedback regardless of how
		// the client renders the deny.
		result.SystemMessage = &reason
		return result
	}
	block := "block"
	result.Decision = &block
	result.Reason = &reason
	return result
}

// constructWarnChallengeResponse builds a PreToolUse deny for a warn challenge,
// splitting the model-facing reason (permissionDecisionReason — authoritative,
// NO link) from the human-facing systemMessage (carries the acknowledgement
// link). Keeping the link out of the model's reason stops the agent from
// treating it as an injected instruction and dismissing the challenge. Only
// PreToolUse carries permissionDecision; any other event falls back to a plain
// block on the agent reason (fail-safe).
func constructWarnChallengeResponse(hookEventName, agentReason, userReason string) *gen.ClaudeHookResult {
	if hookEventName != "PreToolUse" {
		return constructBlockResponse(hookEventName, agentReason)
	}
	result := makeHookResult(hookEventName)
	deny := "deny"
	if output, ok := result.HookSpecificOutput.(*HookSpecificOutput); ok {
		output.PermissionDecision = &deny
		output.PermissionDecisionReason = &agentReason
	}
	// systemMessage renders directly to the user's terminal — the link belongs
	// here, addressed to the human, not in the model-facing reason.
	result.SystemMessage = &userReason
	return result
}

// handleUserPromptSubmit captures the user's prompt text as a chat message.
// When a blocking risk policy matches, it returns 200 with a top-level
// `decision: "block"` + `reason`, the shape Claude Code documents for
// UserPromptSubmit. Claude Code erases the prompt from context and surfaces
// the reason to the user. Returning 200 with a shaped body (instead of 4xx
// or exit-code-2) is what makes the block reason render — stderr-only
// blocks don't carry the reason field at all.
// https://code.claude.com/docs/en/hooks#decision-control
func (s *Service) handleUserPromptSubmit(ctx context.Context, ev *hookevents.UserPromptSubmit) (*gen.ClaudeHookResult, error) {
	payload := claudePayloadFromEvent(ev.Event)
	if payload == nil {
		return makeHookResult(ev.RawEventType), nil
	}
	if s.riskScanner != nil && ev.Prompt != "" && ev.ConversationID != "" {
		if scanResult := s.scanUserPromptForEnforcement(ctx, ev); scanResult != nil {
			// Warn (challenge) defers to the tool call: Claude Code can only show
			// a native [y/n] confirmation at PreToolUse, not at prompt submit.
			// Never hard-block a warn here — let the prompt through so the
			// follow-on tool call carrying the match gets challenged instead.
			if scanResult.Action == "warn" {
				return makeHookResult(ev.RawEventType), nil
			}
			auditReason := fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			// ClickHouse always gets the technical reason; the user_message
			// override only changes what the agent / end user sees.
			if metadata, err := s.getSessionMetadata(ctx, *payload.SessionID); err == nil {
				s.writeClaudeBlockToClickHouse(ctx, payload, &metadata, auditReason)
			}
			return constructBlockResponse(payload.HookEventName, userReason), nil
		}
	}
	return makeHookResult(ev.RawEventType), nil
}

// handleStop captures the assistant's final response text.
// Note: If the Stop event includes tool calls, those are handled separately by PreToolUse events,
// so we skip creating duplicate messages here.
func (s *Service) handleStop(ctx context.Context, ev *hookevents.Stop) (*gen.ClaudeHookResult, error) {
	return makeHookResult(ev.RawEventType), nil
}

// handleSessionEnd returns the native hook response. Efficacy is woken by
// durable observation and message writes rather than this event, which has no
// durable transcript-completion barrier.
func (s *Service) handleSessionEnd(_ context.Context, ev *hookevents.SessionEnd) (*gen.ClaudeHookResult, error) {
	return makeHookResult(ev.RawEventType), nil
}

// handleNotification handles notification events (permission_prompt, idle_prompt, etc.)
func (s *Service) handleNotification(ctx context.Context, ev *hookevents.Notification) (*gen.ClaudeHookResult, error) {
	return makeHookResult(ev.RawEventType), nil
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
		s.logger.DebugContext(ctx, "session capture disabled; skipping Claude chat persistence",
			attr.SlogEvent("claude_hook_session_capture_disabled"),
			attr.SlogOrganizationID(metadata.GramOrgID),
			attr.SlogProjectID(projectID.String()),
			attr.SlogGenAIConversationID(metadata.SessionID),
		)
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
		UserID:         conv.ToPGTextEmpty(metadata.UserID),
		ExternalUserID: conv.ToPGTextEmpty(metadata.UserEmail),
		UserAccountID:  conv.StringToNullUUID(metadata.UserAccountID),
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
		if err := s.backfillLastUserPromptID(ctx, chatID, projectID, payload); err != nil {
			s.logger.WarnContext(ctx, "failed to backfill Claude user prompt ID",
				attr.SlogError(err),
				attr.SlogGenAIConversationID(conv.PtrValOr(payload.SessionID, "")),
				attr.SlogProjectID(metadata.ProjectID),
			)
		}
		role = "assistant"
		content = conv.PtrValOr(payload.LastAssistantMessage, "")
		model = conv.ToPGTextEmpty(conv.PtrValOr(payload.Model, ""))
	default:
		return nil
	}

	if content == "" {
		s.logger.DebugContext(ctx, "skipping empty Claude conversation event",
			attr.SlogEvent("claude_hook_conversation_empty"),
			attr.SlogHookEvent(payload.HookEventName),
			attr.SlogGenAIConversationID(conv.PtrValOr(payload.SessionID, "")),
			attr.SlogProjectID(metadata.ProjectID),
		)
		return nil
	}

	msgParams := chatRepo.CreateChatMessageParams{
		Replayed:         false,
		CreatedAt:        conv.PtrToPGTimestamptz(nil),
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             role,
		Content:          content,
		Model:            model,
		UserID:           conv.ToPGTextEmpty(metadata.UserID),
		Source:           conv.ToPGText(s.claudeSessionSurface(ctx, metadata)),
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

	if err := s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, s.defaultChatTitleForSession(ctx, metadata)); err != nil {
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

func (s *Service) backfillLastUserPromptID(ctx context.Context, chatID uuid.UUID, projectID uuid.UUID, payload *gen.ClaudePayload) error {
	lastUserPromptID := claudeLastUserPromptIDFromAdditionalData(payload.AdditionalData)
	if lastUserPromptID == "" {
		return nil
	}

	_, err := s.repo.BackfillLatestClaudeUserMessagePromptID(ctx, repo.BackfillLatestClaudeUserMessagePromptIDParams{
		ChatID:    chatID,
		ProjectID: projectID,
		MessageID: conv.ToPGText(lastUserPromptID),
	})
	if err != nil {
		return fmt.Errorf("backfill latest Claude user message prompt ID: %w", err)
	}
	return nil
}

func claudeLastUserPromptIDFromAdditionalData(additionalData map[string]any) string {
	if additionalData == nil {
		return ""
	}
	if v, ok := additionalData["LastUserPromptID"].(string); ok {
		return v
	}
	return ""
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
		Replayed:         false,
		CreatedAt:        conv.PtrToPGTimestamptz(nil),
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "assistant",
		Content:          "", // Tool call requests typically have empty content
		Model:            conv.ToPGTextEmpty(conv.PtrValOr(payload.Model, "")),
		UserID:           conv.ToPGTextEmpty(metadata.UserID),
		Source:           conv.ToPGText(s.claudeSessionSurface(ctx, metadata)),
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

	return s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, s.defaultChatTitleForSession(ctx, metadata))
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
		Replayed:         false,
		CreatedAt:        conv.PtrToPGTimestamptz(nil),
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "tool",
		Content:          content,
		UserID:           conv.ToPGTextEmpty(metadata.UserID),
		Source:           conv.ToPGText(s.claudeSessionSurface(ctx, metadata)),
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

	return s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, s.defaultChatTitleForSession(ctx, metadata))
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
