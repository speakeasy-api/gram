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
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	cursorevents "github.com/speakeasy-api/gram/server/internal/hookevents/adapters/cursor"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// Cursor is the endpoint for Cursor hook events
func (s *Service) Cursor(ctx context.Context, payload *gen.CursorPayload) (*gen.CursorHookResult, error) {
	hookEvent, parsedHookEvent := parseCursorHookEvent(payload.HookEventName)
	logHookEventName := payload.HookEventName
	if parsedHookEvent {
		logHookEventName = string(hookEvent)
	}

	logger := s.logger.With(
		attr.SlogHookSource("cursor"),
		attr.SlogHookEvent(logHookEventName),
		attr.SlogToolName(conv.PtrValOr(payload.ToolName, "")),
		attr.SlogGenAIConversationID(conv.PtrValOr(payload.ConversationID, "")),
		attr.SlogAuthUserEmail(conv.PtrValOr(payload.UserEmail, "")),
	)

	authCtx, authOK := contextvalues.GetAuthContext(ctx)
	if !authOK || authCtx == nil || authCtx.ProjectID == nil {
		logger.WarnContext(ctx, "rejected unauthorized cursor hook request",
			attr.SlogEvent("cursor_hook_unauthorized"),
		)
		return &gen.CursorHookResult{
			Permission:        new("deny"),
			UserMessage:       new("Speakeasy hooks: unauthorized — check your Gram API key and project slug."),
			AdditionalContext: nil,
			AgentMessage:      nil,
		}, nil
	}

	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()
	userEmail := strings.TrimSpace(conv.PtrValOr(payload.UserEmail, ""))
	if userEmail == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "cursor hook payload missing user_email")
	}
	actorUserID := s.resolveUserByEmail(ctx, userEmail, orgID)
	logger = logger.With(
		attr.SlogOrganizationID(orgID),
		attr.SlogProjectID(projectID),
	)

	logger.InfoContext(ctx, "cursor hook received",
		attr.SlogEvent("cursor_hook"),
	)

	// Claim the per-invocation idempotency token before persistence. A retry
	// re-sends the same token: the decision still re-runs so the user stays
	// blocked, but tagging the context as a duplicate suppresses the duplicate
	// writes in recordCursorHook.
	if !s.claimHookIdempotency(ctx, conv.PtrValOr(payload.IdempotencyKey, "")) {
		ctx = withHookDuplicate(ctx)
	}

	result := &gen.CursorHookResult{
		Permission:        nil,
		UserMessage:       nil,
		AdditionalContext: nil,
		AgentMessage:      nil,
	}

	cursorEvent, err := cursorevents.Normalize(authCtx, payload, hookevents.Identity{
		OrganizationID: orgID,
		ProjectID:      *authCtx.ProjectID,
		UserID:         actorUserID,
		UserEmail:      userEmail,
	}, time.Now())
	if err != nil {
		return nil, err
	}
	if cursorEvent == nil {
		logger.InfoContext(ctx, "cursor hook received illegal event type",
			attr.SlogEvent("cursor_hook_illegal_event_type"),
			attr.SlogHookEvent(payload.HookEventName),
		)
		return result, nil
	}

	// blockReason is empty unless this call is denied by the shadow-MCP guard.
	// It propagates into the ClickHouse log entry as gram.hook.block_reason so
	// the trace renders as "blocked" in dashboards.
	var blockReason string

	switch ev := cursorEvent.(type) {
	case *hookevents.BeforeMCPExecution:
		// beforeMCPExecution fires for MCP-routed (non-local) tool calls. Run
		// the risk scanner first (block-only today), then fall through to the
		// shadow-MCP guard so unapproved toolsets are still blocked.
		if scanResult := s.scanMCPRequestForEnforcement(ctx, ev); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			blockReason = auditReason
			result.Permission = new("deny")
			result.UserMessage = &userReason
			break
		}
		policy := s.lookupShadowMCPBlockingPolicy(ctx, orgID, projectID, actorUserID)
		if policy == nil {
			result.Permission = new("allow")
			break
		}
		toolName := strings.TrimPrefix(ev.ToolName, "MCP:")
		evidence := cursorShadowMCPEvidence(payload)
		if detail, denied := s.enforceShadowMCPToolAccess(ctx, orgID, projectID, actorUserID, policy.ID, ev.ToolInput, toolName, evidence); denied {
			logger.InfoContext(ctx, "denying cursor tool call: failed gram toolset validation",
				attr.SlogEvent("cursor_hook_denied"),
				attr.SlogHookBlockReason(detail),
				attr.SlogRiskPolicyID(policy.ID),
				attr.SlogRiskPolicyName(policy.Name),
			)
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
			userReason := s.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
				OrganizationID:  orgID,
				ProjectID:       projectID,
				RequesterUserID: actorUserID,
				UserMessage:     policy.UserMessage,
				AuditReason:     auditReason,
				Evidence:        evidence,
				ToolName:        toolName,
				ToolInput:       ev.ToolInput,
				RiskPolicyID:    policy.ID,
			})
			blockReason = auditReason
			result.Permission = new("deny")
			result.UserMessage = &userReason
			result.AgentMessage = &userReason
		} else {
			result.Permission = new("allow")
		}
	case *hookevents.BeforeToolUse:
		// preToolUse fires for ALL Cursor tool calls including MCP ones, while
		// beforeMCPExecution also fires for MCP-routed calls and already runs
		// the scan there. Skip the scan here for MCP tools to avoid scanning
		// (and DB-querying) the same input twice on the hot path. Native tools
		// (read_file, edit_file, ...) only have this single event and still
		// get scanned.
		toolName := ev.ToolName
		if strings.HasPrefix(toolName, "MCP:") {
			result.Permission = new("allow")
			break
		}
		if scanResult := s.scanToolRequestForEnforcement(ctx, ev); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			blockReason = auditReason
			result.Permission = new("deny")
			result.UserMessage = &userReason
		} else {
			result.Permission = new("allow")
		}
	case *hookevents.UserPromptSubmit:
		if scanResult := s.scanUserPromptForEnforcement(ctx, ev); scanResult != nil {
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
	s.recordCursorHook(ctx, payload, orgID, projectID, actorUserID, blockReason)

	return result, nil
}

func (s *Service) recordCursorHook(ctx context.Context, payload *gen.CursorPayload, orgID string, projectID string, userID string, blockReason string) {
	if payload.ConversationID == nil || *payload.ConversationID == "" {
		s.logger.WarnContext(ctx, "Cursor event called without conversation ID")
		return
	}

	// Skip persistence for a redelivery (the token was claimed in Cursor()).
	if s.isHookDuplicate(ctx) {
		return
	}

	// Persistence outlives the request: the client may close the connection
	// the instant the hook returns, which would otherwise cancel in-flight
	// INSERTs and drop the chat message.
	ctx = context.WithoutCancel(ctx)

	userEmail := conv.PtrValOr(payload.UserEmail, "")

	metadata := &SessionMetadata{
		SessionID:   *payload.ConversationID,
		ServiceName: "Cursor",
		UserEmail:   userEmail,
		UserID:      userID,
		ClaudeOrgID: "",
		GramOrgID:   orgID,
		ProjectID:   projectID,
	}

	// Persistence does DB + ClickHouse writes that can take longer than the
	// client is willing to wait for a hook response (`stop` especially —
	// curl in send_hook.sh has a 10s --max-time and the client closes the
	// connection the moment the response lands). Run detached so the
	// response returns promptly and the work completes in the background.
	go s.persistCursorHook(ctx, payload, metadata, blockReason)
}

func (s *Service) persistCursorHook(ctx context.Context, payload *gen.CursorPayload, metadata *SessionMetadata, blockReason string) {
	hookEvent, ok := parseCursorHookEvent(payload.HookEventName)
	if !ok {
		return
	}

	if isCursorConversationEvent(hookEvent) {
		// Conversation events: PG only (user prompts and agent responses)
		var err error
		switch hookEvent {
		case HookEventBeforeSubmitPrompt:
			err = s.persistCursorUserPrompt(ctx, payload, metadata)
		case HookEventAfterAgentResponse:
			err = s.persistCursorAgentResponse(ctx, payload, metadata)
			// afterAgentResponse also carries token usage — record a metrics entry in ClickHouse
			s.writeCursorMetricsToClickHouse(ctx, payload, metadata.GramOrgID, metadata.ProjectID, metadata.UserID)
		default:
			return
		}
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to persist Cursor conversation event", attr.SlogError(err))
		}
	} else {
		// Tool call events: ClickHouse + PG
		if err := s.persistCursorToolCallEvent(ctx, payload, metadata, blockReason, hookEvent); err != nil {
			s.logger.ErrorContext(ctx, "Failed to persist Cursor tool call event", attr.SlogError(err))
		}
	}
}

// persistCursorToolCallEvent writes tool call events to both ClickHouse and PostgreSQL
func (s *Service) persistCursorToolCallEvent(ctx context.Context, payload *gen.CursorPayload, metadata *SessionMetadata, blockReason string, hookEvent HookEvent) error {
	// Write to ClickHouse for telemetry
	s.writeCursorHookToClickHouse(ctx, payload, metadata.GramOrgID, metadata.ProjectID, metadata.UserID, blockReason)

	// Write to PostgreSQL for chat history
	switch hookEvent {
	case HookEventPreToolUse, HookEventBeforeMCPExecution:
		return s.writeCursorToolCallRequestToPG(ctx, payload, metadata)
	case HookEventPostToolUse, HookEventPostToolUseFailure, HookEventAfterMCPExecution:
		return s.writeCursorToolCallResultToPG(ctx, payload, metadata, hookEvent)
	default:
		return nil
	}
}

// isCursorConversationEvent returns true if the event is a conversation capture event (not a tool call).
func isCursorConversationEvent(hookEvent HookEvent) bool {
	switch hookEvent {
	case HookEventBeforeSubmitPrompt, HookEventAfterAgentResponse:
		return true
	default:
		return false
	}
}

// writeCursorHookToClickHouse writes a Cursor hook event directly to ClickHouse
// Unlike Claude hooks, Cursor payloads are already authenticated and include user_email,
// so no Redis buffering is needed.
func (s *Service) writeCursorHookToClickHouse(ctx context.Context, payload *gen.CursorPayload, orgID string, projectID string, userID string, blockReason string) {
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
			UserInfo:   telemetry.UserInfoByID(userID),
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
func (s *Service) writeCursorMetricsToClickHouse(ctx context.Context, payload *gen.CursorPayload, orgID string, projectID string, userID string) {
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
		UserInfo:   telemetry.UserInfoByID(userID),
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

	hookEvent, ok := parseCursorHookEvent(payload.HookEventName)
	hookEventName := payload.HookEventName
	if ok {
		hookEventName = string(hookEvent)
	}

	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.ToolNameKey:       toolName,
		attr.HookEventKey:      hookEventName,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        generateTraceID(),
		attr.LogBodyKey:        fmt.Sprintf("Hook: %s", hookEventName),
		attr.ProjectIDKey:      projectID,
		attr.OrganizationIDKey: orgID,
		attr.HookSourceKey:     "cursor",
	}
	applyHookHostnameAttr(attrs, payload.HookHostname)

	if payload.Error != nil {
		attrs[attr.HookErrorKey] = payload.Error
	}

	if payload.IsInterrupt != nil {
		attrs[attr.HookIsInterruptKey] = *payload.IsInterrupt
	}

	// Parse MCP tool names (same mcp__ prefix convention). Match only the
	// lowercase Claude-style "mcp__<server>__<tool>" form here; Cursor's own
	// "MCP:" prefix (which AttributeTool also recognizes) is handled separately
	// below, so guard on the mcp__ prefix to keep that split exclusive.
	if strings.HasPrefix(toolName, "mcp__") {
		if server, fn, ok := toolref.AttributeTool(toolName); ok {
			attrs[attr.ToolCallSourceKey] = server
			attrs[attr.ToolNameKey] = fn
		}
	}

	// beforeMCPExecution / afterMCPExecution: derive tool_source from the MCP
	// server URL (or command for stdio servers), which the generic
	// preToolUse/postToolUse events do not expose.
	if hookEvent == HookEventBeforeMCPExecution || hookEvent == HookEventAfterMCPExecution {
		if source := cursorMCPToolSource(payload); source != "" {
			attrs[attr.ToolCallSourceKey] = source
		}
		// Tool names for MCP events may arrive with a "MCP:" prefix (the same
		// string used in Cursor hook matchers). Strip it so the stored name
		// matches the bare tool name.
		if stripped, ok := strings.CutPrefix(toolName, "MCP:"); ok {
			attrs[attr.ToolNameKey] = stripped
		}
		// Cursor surfaces the MCP server URL on the payload directly (no
		// SessionStart inventory), so persist it alongside the tool call.
		if payload.URL != nil && *payload.URL != "" {
			attrs[attr.MCPServerURLKey] = *payload.URL
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
	if hookEvent == HookEventBeforeSubmitPrompt && payload.Prompt != nil && *payload.Prompt != "" {
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
		UserID:           conv.ToPGTextEmpty(metadata.UserID),
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
func (s *Service) writeCursorToolCallResultToPG(ctx context.Context, payload *gen.CursorPayload, metadata *SessionMetadata, hookEvent HookEvent) error {
	if payload.ConversationID == nil || *payload.ConversationID == "" {
		return nil
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	chatID := sessionIDToUUID(*payload.ConversationID)

	var content string
	switch hookEvent {
	case HookEventPostToolUse:
		if payload.ToolResponse == nil {
			return nil
		}
		content = marshalToJSON(payload.ToolResponse)
	case HookEventPostToolUseFailure:
		if payload.Error == nil {
			return nil
		}
		content = marshalToJSON(payload.Error)
	case HookEventAfterMCPExecution:
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
		UserID:           conv.ToPGTextEmpty(metadata.UserID),
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
		UserID:           conv.ToPGTextEmpty(metadata.UserID),
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
		UserID:           conv.ToPGTextEmpty(metadata.UserID),
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
