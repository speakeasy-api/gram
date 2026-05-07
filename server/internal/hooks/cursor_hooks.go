package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
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

	result := &gen.CursorHookResult{
		Permission:        nil,
		UserMessage:       nil,
		AdditionalContext: nil,
		AgentMessage:      nil,
	}

	// blockReason is empty unless this call is denied by the shadow-MCP guard.
	// It propagates into the ClickHouse log entry as gram.hook.block_reason so
	// the trace renders as "blocked" in dashboards.
	var blockReason string

	switch payload.HookEventName {
	case "beforeMCPExecution":
		// beforeMCPExecution fires for MCP-routed (non-local) tool calls. Run
		// the risk scanner first (block-only today), then fall through to the
		// shadow-MCP guard so unapproved toolsets are still blocked.
		if scanResult := s.scanCursorForEnforcement(ctx, payload, orgID, projectID); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			blockReason = auditReason
			result.Permission = new("deny")
			result.UserMessage = &userReason
			break
		}
		policy := s.lookupShadowMCPBlockingPolicy(ctx, projectID)
		if policy == nil {
			result.Permission = new("allow")
			break
		}
		toolName := strings.TrimPrefix(conv.PtrValOr(payload.ToolName, ""), "MCP:")
		if detail, denied := s.shadowMCPClient.ValidateToolsetCall(ctx, payload.ToolInput, toolName, orgID); denied {
			s.logger.InfoContext(ctx, "denying cursor tool call: failed gram toolset validation",
				attr.SlogEvent("cursor_hook_denied"),
				attr.SlogValueAny(map[string]any{
					"hookEventName": payload.HookEventName,
					"toolName":      conv.PtrValOr(payload.ToolName, ""),
					"reason":        detail,
					"policyID":      policy.ID,
					"policyName":    policy.Name,
				}),
			)
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
			userReason := renderUserBlockReason(policy.UserMessage, auditReason)
			blockReason = auditReason
			result.Permission = new("deny")
			result.UserMessage = &userReason
			result.AgentMessage = &userReason
		} else {
			result.Permission = new("allow")
		}
	case "preToolUse":
		// preToolUse fires for ALL Cursor tool calls including MCP ones, while
		// beforeMCPExecution also fires for MCP-routed calls and already runs
		// the scan there. Skip the scan here for MCP tools to avoid scanning
		// (and DB-querying) the same input twice on the hot path. Native tools
		// (read_file, edit_file, ...) only have this single event and still
		// get scanned.
		toolName := conv.PtrValOr(payload.ToolName, "")
		if strings.HasPrefix(toolName, "MCP:") {
			result.Permission = new("allow")
			break
		}
		if scanResult := s.scanCursorForEnforcement(ctx, payload, orgID, projectID); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			blockReason = auditReason
			result.Permission = new("deny")
			result.UserMessage = &userReason
		} else {
			result.Permission = new("allow")
		}
	case "beforeSubmitPrompt":
		if scanResult := s.scanCursorForEnforcement(ctx, payload, orgID, projectID); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			blockReason = auditReason
			result.Permission = new("deny")
			result.UserMessage = &userReason
		}
	default:
		// nothing to do
	}

	// Record the hook (will route to ClickHouse for tool calls, PG for all events).
	// Runs after the deny decision so the ClickHouse entry can carry the
	// block reason as an attribute.
	s.recordCursorHook(ctx, payload, orgID, projectID, blockReason)

	return result, nil
}

func (s *Service) recordCursorHook(ctx context.Context, payload *gen.CursorPayload, orgID string, projectID string, blockReason string) {
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

	s.persistCursorHook(ctx, payload, metadata, blockReason)
}

func (s *Service) persistCursorHook(ctx context.Context, payload *gen.CursorPayload, metadata *SessionMetadata, blockReason string) {
	if isCursorConversationEvent(payload.HookEventName) {
		// Conversation events: PG only (user prompts and agent responses)
		var err error
		switch payload.HookEventName {
		case "beforeSubmitPrompt":
			err = s.persistCursorUserPrompt(ctx, payload, metadata)
		case "afterAgentResponse":
			err = s.persistCursorAgentResponse(ctx, payload, metadata)
			// afterAgentResponse also carries token usage — record a metrics entry in ClickHouse
			s.writeCursorMetricsToClickHouse(ctx, payload, metadata.GramOrgID, metadata.ProjectID)
		}
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to persist Cursor conversation event", attr.SlogError(err))
		}
	} else {
		// Tool call events: ClickHouse + PG
		if err := s.persistCursorToolCallEvent(ctx, payload, metadata, blockReason); err != nil {
			s.logger.ErrorContext(ctx, "Failed to persist Cursor tool call event", attr.SlogError(err))
		}
	}
}

// persistCursorToolCallEvent writes tool call events to both ClickHouse and PostgreSQL
func (s *Service) persistCursorToolCallEvent(ctx context.Context, payload *gen.CursorPayload, metadata *SessionMetadata, blockReason string) error {
	// Write to ClickHouse for telemetry
	s.writeCursorHookToClickHouse(ctx, payload, metadata.GramOrgID, metadata.ProjectID, blockReason)

	// Write to PostgreSQL for chat history
	switch payload.HookEventName {
	case "preToolUse", "beforeMCPExecution":
		return s.writeCursorToolCallRequestToPG(ctx, payload, metadata)
	case "postToolUse", "postToolUseFailure", "afterMCPExecution":
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
func (s *Service) writeCursorHookToClickHouse(ctx context.Context, payload *gen.CursorPayload, orgID string, projectID string, blockReason string) {
	attrs := s.buildCursorTelemetryAttributes(ctx, payload, orgID, projectID)
	if blockReason != "" {
		attrs[attr.HookBlockReasonKey] = blockReason
	}
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

// writeCursorMetricsToClickHouse writes Cursor token-usage metrics to ClickHouse
// telemetry_logs. Mirrors writeMetricsToClickHouse for Claude Code: a separate
// log entry with a `cursor:usage:metrics` URN so usage can be aggregated
// independently of tool-call events.
func (s *Service) writeCursorMetricsToClickHouse(ctx context.Context, payload *gen.CursorPayload, orgID string, projectID string) {
	if s.telemetryLogger == nil {
		return
	}

	hasTokens := (payload.InputTokens != nil && *payload.InputTokens > 0) ||
		(payload.OutputTokens != nil && *payload.OutputTokens > 0) ||
		(payload.CacheReadTokens != nil && *payload.CacheReadTokens > 0) ||
		(payload.CacheWriteTokens != nil && *payload.CacheWriteTokens > 0)
	if !hasTokens {
		return
	}

	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Invalid project ID for Cursor metrics", attr.SlogError(err))
		return
	}

	urn := "cursor:usage:metrics"

	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.LogBodyKey:        "Cursor usage metrics",
		attr.ProjectIDKey:      projectID,
		attr.OrganizationIDKey: orgID,
		attr.ResourceURNKey:    urn,
		attr.HookSourceKey:     "cursor",
		attr.HookEventKey:      "AfterAgentResponse",
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        generateTraceID(),
	}

	if payload.InputTokens != nil && *payload.InputTokens > 0 {
		attrs[attr.GenAIUsageInputTokensKey] = *payload.InputTokens
	}
	if payload.OutputTokens != nil && *payload.OutputTokens > 0 {
		attrs[attr.GenAIUsageOutputTokensKey] = *payload.OutputTokens
	}
	if payload.CacheReadTokens != nil && *payload.CacheReadTokens > 0 {
		attrs[attr.GenAIUsageCacheReadInputTokensKey] = *payload.CacheReadTokens
	}
	if payload.CacheWriteTokens != nil && *payload.CacheWriteTokens > 0 {
		attrs[attr.GenAIUsageCacheCreationInputTokensKey] = *payload.CacheWriteTokens
	}
	if payload.Model != nil && *payload.Model != "" {
		attrs[attr.GenAIResponseModelKey] = *payload.Model
	}
	if payload.UserEmail != nil && *payload.UserEmail != "" {
		attrs[attr.UserEmailKey] = *payload.UserEmail
	}
	switch {
	case payload.ConversationID != nil && *payload.ConversationID != "":
		attrs[attr.GenAIConversationIDKey] = *payload.ConversationID
	case payload.SessionID != nil && *payload.SessionID != "":
		attrs[attr.GenAIConversationIDKey] = *payload.SessionID
	}

	toolInfo := telemetry.ToolInfo{
		Name:           "cursor",
		OrganizationID: orgID,
		ProjectID:      parsedProjectID.String(),
		ID:             "",
		URN:            urn,
		DeploymentID:   "",
		FunctionID:     nil,
	}

	s.telemetryLogger.Log(ctx, telemetry.LogParams{
		Timestamp:  time.Now(),
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})

	s.logger.DebugContext(ctx, "Wrote Cursor metrics to ClickHouse",
		attr.SlogEvent("cursor_metrics_written"),
	)
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
	case "beforeMCPExecution":
		hookEvent = "BeforeMCPExecution"
	case "afterMCPExecution":
		hookEvent = "AfterMCPExecution"
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

	// beforeMCPExecution / afterMCPExecution: derive tool_source from the MCP
	// server URL (or command for stdio servers), which the generic
	// preToolUse/postToolUse events do not expose.
	if payload.HookEventName == "beforeMCPExecution" || payload.HookEventName == "afterMCPExecution" {
		if source := cursorMCPToolSource(payload); source != "" {
			attrs[attr.ToolCallSourceKey] = source
		}
		// Tool names for MCP events may arrive with a "MCP:" prefix (the same
		// string used in Cursor hook matchers). Strip it so the stored name
		// matches the bare tool name.
		if stripped, ok := strings.CutPrefix(toolName, "MCP:"); ok {
			attrs[attr.ToolNameKey] = stripped
		}
	}

	if correlationID := cursorToolCorrelationID(payload); correlationID != "" {
		attrs[attr.TraceIDKey] = hashToolCallIDToTraceID(correlationID)
		attrs[attr.GenAIToolCallIDKey] = correlationID
	}
	if payload.ConversationID != nil {
		attrs[attr.GenAIConversationIDKey] = *payload.ConversationID
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

	// afterMCPExecution sends the tool response pre-stringified as result_json.
	// Cursor doesn't emit a separate failure event for MCP — instead the result
	// body's MCP-protocol "isError" flag indicates failure. Surface that as
	// gram.hook.error so trace_summaries.has_error fires the same way it would
	// for an explicit failure event.
	if payload.ResultJSON != nil && *payload.ResultJSON != "" {
		attrs[attr.GenAIToolCallResultKey] = *payload.ResultJSON
		var parsed struct {
			IsError bool `json:"isError"`
		}
		if err := json.Unmarshal([]byte(*payload.ResultJSON), &parsed); err == nil && parsed.IsError {
			attrs[attr.HookErrorKey] = *payload.ResultJSON
		}
	}

	return attrs
}

// cursorToolCorrelationID returns a stable identifier that links a tool call's
// request and result events together. Cursor's beforeMCPExecution /
// afterMCPExecution payloads do not include a tool_use_id, and even
// preToolUse / postToolUse can omit one. We derive a deterministic ID from
// (conversation_id, generation_id, tool_name, tool_input) — which is identical
// for the request and result of the same call. A real tool_use_id is preferred
// when present.
//
// Limitation: an agent that issues the *same* tool with *identical* inputs
// twice within a single generation will collide into one correlation ID. If
// that becomes a problem we can switch to a Redis-backed FIFO of correlation
// IDs keyed by the same tuple.
func cursorToolCorrelationID(payload *gen.CursorPayload) string {
	if payload.ToolUseID != nil && *payload.ToolUseID != "" {
		return *payload.ToolUseID
	}

	convID := conv.PtrValOr(payload.ConversationID, "")
	genID := conv.PtrValOr(payload.GenerationID, "")
	toolName := strings.TrimPrefix(conv.PtrValOr(payload.ToolName, ""), "MCP:")
	if convID == "" && genID == "" && toolName == "" && payload.ToolInput == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(convID)
	b.WriteByte('|')
	b.WriteString(genID)
	b.WriteByte('|')
	b.WriteString(toolName)
	b.WriteByte('|')
	if payload.ToolInput != nil {
		if jsonBytes, err := json.Marshal(payload.ToolInput); err == nil {
			b.Write(jsonBytes)
		}
	}

	sum := sha256.Sum256([]byte(b.String()))
	return "cursor_synth_" + hex.EncodeToString(sum[:8])
}

// cursorMCPToolSource derives a tool_source string for beforeMCPExecution /
// afterMCPExecution events. URL-based servers use the URL host; command-based
// servers fall back to the command string.
func cursorMCPToolSource(payload *gen.CursorPayload) string {
	if payload.URL != nil && *payload.URL != "" {
		if u, err := url.Parse(*payload.URL); err == nil && u.Host != "" {
			return u.Host
		}
		return *payload.URL
	}
	if payload.Command != nil && *payload.Command != "" {
		return *payload.Command
	}
	return ""
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
		"id":   cursorToolCorrelationID(payload),
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
		ContentHash:      nil,
		Generation:       0,
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
	switch {
	case payload.HookEventName == "postToolUse" && payload.ToolResponse != nil:
		content = marshalToJSON(payload.ToolResponse)
	case payload.HookEventName == "postToolUseFailure" && payload.Error != nil:
		content = marshalToJSON(payload.Error)
	case payload.HookEventName == "afterMCPExecution":
		// afterMCPExecution delivers the response as an already-stringified JSON
		// payload; fall back to ToolResponse if a client sends the structured form.
		if payload.ResultJSON != nil && *payload.ResultJSON != "" {
			content = *payload.ResultJSON
		} else if payload.ToolResponse != nil {
			content = marshalToJSON(payload.ToolResponse)
		} else {
			return nil
		}
	default:
		return nil
	}

	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "tool",
		Content:          content,
		UserID:           conv.ToPGTextEmpty(""),
		Source:           conv.ToPGText("Cursor"),
		ToolCallID:       conv.ToPGTextEmpty(cursorToolCorrelationID(payload)),
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
		ContentHash:      nil,
		Generation:       0,
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
		ContentHash:      nil,
		Generation:       0,
	}

	return s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, parsedProjectID, msgParams, activities.DefaultCursorChatTitle)
}
