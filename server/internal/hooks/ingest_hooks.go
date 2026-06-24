package hooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// Ingest is the authenticated, unified hook endpoint. Unlike the legacy
// platform-specific endpoints, attribution comes from the authenticated API key
// owner in AuthContext. Any user email in the payload is treated as
// source-reported metadata only.
func (s *Service) Ingest(ctx context.Context, payload *gen.IngestPayload) (*gen.IngestHookResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	actorEmail := ""
	if authCtx.Email != nil {
		actorEmail = strings.TrimSpace(*authCtx.Email)
	}

	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()
	actorUserID := authCtx.UserID

	logger := s.logger.With(
		attr.SlogHookSource(payload.HookSource),
		attr.SlogHookEvent(payload.EventType),
		attr.SlogToolName(conv.PtrValOr(payload.ToolName, "")),
		attr.SlogGenAIConversationID(ingestConversationID(payload)),
		attr.SlogOrganizationID(orgID),
		attr.SlogProjectID(projectID),
	)
	logger.InfoContext(ctx, "unified hook received", attr.SlogEvent("hooks_ingest"))

	if !s.claimHookIdempotency(ctx, conv.PtrValOr(payload.IdempotencyKey, "")) {
		ctx = withHookDuplicate(ctx)
	}

	hookEvent, err := normalizedEventFromIngest(payload, authCtx, actorEmail, time.Now())
	if err != nil {
		return nil, err
	}
	blockReason, userReason := s.evaluateIngestHook(ctx, hookEvent, payload, authCtx, actorUserID)
	s.recordKnownIngestSource(ctx, payload, authCtx, actorEmail, blockReason)
	if blockReason != "" {
		return blockedIngestResult(payload, userReason), nil
	}
	return allowedIngestResult(payload), nil
}

func normalizedEventFromIngest(payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actorEmail string, timestamp time.Time) (any, error) {
	eventType := hookevents.EventType(strings.TrimSpace(payload.EventType))
	rawEventType := rawIngestHookEventName(payload)
	if rawEventType == "" {
		rawEventType = string(eventType)
	}

	event := hookevents.Event{
		Provider:     hookevents.Provider(strings.TrimSpace(payload.HookSource)),
		Type:         eventType,
		RawEventType: rawEventType,
		Timestamp:    timestamp,
		AuthContext:  authCtx,
		Context: hookevents.EventContext{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			User: hookevents.User{
				ID:    authCtx.UserID,
				Email: actorEmail,
			},
		},
		ConversationID: ingestConversationID(payload),
		Raw:            payload,
	}

	switch eventType {
	case hookevents.EventTypeSessionStart:
		return hookevents.NewSessionStart(event), nil
	case hookevents.EventTypeConfigChange:
		return hookevents.NewConfigChange(event), nil
	case hookevents.EventTypeBeforeToolUse:
		return hookevents.NewBeforeToolUse(event, hookevents.BeforeToolUseParams{
			ToolName:  conv.PtrValOr(payload.ToolName, ""),
			ToolInput: payload.ToolInput,
		}), nil
	case hookevents.EventTypeAfterToolUse:
		return hookevents.NewAfterToolUse(event, hookevents.AfterToolUseParams{
			ToolName:   conv.PtrValOr(payload.ToolName, ""),
			ToolOutput: ingestToolOutput(payload),
		}), nil
	case hookevents.EventTypeAfterToolUseFailure:
		return hookevents.NewAfterToolUseFailure(event, hookevents.AfterToolUseFailureParams{
			ToolName:    conv.PtrValOr(payload.ToolName, ""),
			Error:       payload.Error,
			IsInterrupt: conv.PtrValOr(payload.IsInterrupt, false),
		}), nil
	case hookevents.EventTypeBeforeMCPExecution:
		return hookevents.NewBeforeMCPExecution(event, hookevents.BeforeMCPExecutionParams{
			ToolName:  conv.PtrValOr(payload.ToolName, ""),
			ToolInput: payload.ToolInput,
		}), nil
	case hookevents.EventTypeAfterMCPExecution:
		return hookevents.NewAfterMCPExecution(event, hookevents.AfterMCPExecutionParams{
			ToolName:   conv.PtrValOr(payload.ToolName, ""),
			ToolOutput: ingestToolOutput(payload),
		}), nil
	case hookevents.EventTypePermissionRequest:
		return hookevents.NewPermissionRequest(event, hookevents.PermissionRequestParams{
			ToolName:       conv.PtrValOr(payload.ToolName, ""),
			ToolInput:      payload.ToolInput,
			PermissionType: conv.PtrValOr(payload.PermissionType, ""),
		}), nil
	case hookevents.EventTypeUserPromptSubmit:
		return hookevents.NewUserPromptSubmit(event, hookevents.UserPromptSubmitParams{
			Prompt: conv.PtrValOr(payload.Prompt, ""),
		}), nil
	case hookevents.EventTypeAfterAgentResponse:
		return hookevents.NewAfterAgentResponse(event, hookevents.AfterAgentResponseParams{
			Text: conv.PtrValOr(payload.Text, ""),
		}), nil
	case hookevents.EventTypeAfterAgentThought:
		return hookevents.NewAfterAgentThought(event, hookevents.AfterAgentThoughtParams{
			Text:       conv.PtrValOr(payload.Text, ""),
			DurationMs: conv.PtrValOr(payload.DurationMs, 0),
		}), nil
	case hookevents.EventTypeStop:
		return hookevents.NewStop(event, hookevents.StopParams{
			LastAssistantMessage: conv.PtrValOr(payload.LastAssistantMessage, ""),
		}), nil
	case hookevents.EventTypeSessionEnd:
		return hookevents.NewSessionEnd(event, hookevents.SessionEndParams{
			Reason: conv.PtrValOr(payload.Reason, ""),
		}), nil
	case hookevents.EventTypeNotification:
		return hookevents.NewNotification(event, hookevents.NotificationParams{
			NotificationType: conv.PtrValOr(payload.NotificationType, ""),
			Message:          conv.PtrValOr(payload.Message, ""),
			Title:            conv.PtrValOr(payload.Title, ""),
		}), nil
	default:
		return nil, oops.E(oops.CodeInvalid, nil, "unsupported hook event_type")
	}
}

func (s *Service) evaluateIngestHook(ctx context.Context, hookEvent any, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actorUserID string) (string, string) {
	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()

	switch ev := hookEvent.(type) {
	case *hookevents.BeforeMCPExecution:
		if scanResult := s.scanMCPRequestForEnforcement(ctx, ev); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			return auditReason, renderUserBlockReason(scanResult.UserMessage, auditReason)
		}
		return s.evaluateIngestShadowMCP(ctx, orgID, projectID, actorUserID, payload, ev.ToolName, ev.ToolInput)
	case *hookevents.BeforeToolUse:
		if scanResult := s.scanToolRequestForEnforcement(ctx, ev); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			return auditReason, renderUserBlockReason(scanResult.UserMessage, auditReason)
		}
		if toolref.IsMCPToolName(ev.ToolName) {
			return s.evaluateIngestShadowMCP(ctx, orgID, projectID, actorUserID, payload, ev.ToolName, ev.ToolInput)
		}
	case *hookevents.PermissionRequest:
		if scanResult := s.scanPermissionRequestForEnforcement(ctx, ev); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this permission request: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			return auditReason, renderUserBlockReason(scanResult.UserMessage, auditReason)
		}
	case *hookevents.UserPromptSubmit:
		if scanResult := s.scanUserPromptForEnforcement(ctx, ev); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			return auditReason, renderUserBlockReason(scanResult.UserMessage, auditReason)
		}
	default:
	}
	return "", ""
}

func (s *Service) evaluateIngestShadowMCP(ctx context.Context, orgID, projectID, actorUserID string, payload *gen.IngestPayload, rawToolName string, toolInput any) (string, string) {
	policy := s.lookupShadowMCPBlockingPolicy(ctx, orgID, projectID, actorUserID)
	if policy == nil {
		return "", ""
	}

	toolName := toolref.MCPFunctionOf(rawToolName)
	evidence := ingestShadowMCPEvidence(payload, rawToolName)
	if detail, denied := s.enforceShadowMCPToolAccess(ctx, orgID, projectID, actorUserID, policy.ID, toolInput, toolName, evidence); denied {
		auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
		userReason := s.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
			OrganizationID:  orgID,
			ProjectID:       projectID,
			RequesterUserID: actorUserID,
			UserMessage:     policy.UserMessage,
			AuditReason:     auditReason,
			Evidence:        evidence,
			ToolName:        toolName,
			ToolInput:       toolInput,
			RiskPolicyID:    policy.ID,
		})
		return auditReason, userReason
	}
	return "", ""
}

func ingestShadowMCPEvidence(payload *gen.IngestPayload, rawToolName string) shadowmcp.AccessEvidence {
	return shadowmcp.AccessEvidence{
		FullURL:        conv.PtrValOr(payload.URL, ""),
		URLHost:        "",
		ServerIdentity: toolref.MCPServerOf(rawToolName),
	}
}

func (s *Service) recordKnownIngestSource(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actorEmail string, blockReason string) {
	switch strings.TrimSpace(payload.HookSource) {
	case "claude":
		claudePayload := claudePayloadFromIngest(payload, actorEmail)
		if sessionID := conv.PtrValOr(claudePayload.SessionID, ""); sessionID != "" {
			metadata := authenticatedSessionMetadata(authCtx, sessionID, "claude-code", actorEmail)
			if err := s.cache.Set(ctx, sessionCacheKey(sessionID), metadata, 24*time.Hour); err != nil {
				s.logger.WarnContext(ctx, "failed to cache authenticated Claude hook session metadata",
					attr.SlogEvent("hooks_ingest_claude_cache_set_failed"),
					attr.SlogError(err),
					attr.SlogGenAIConversationID(sessionID),
				)
			}
		}
		if payload.EventType == string(hookevents.EventTypeSessionStart) || payload.EventType == string(hookevents.EventTypeConfigChange) {
			s.captureMCPListSnapshot(ctx, claudePayload)
		}
		s.recordHook(ctx, claudePayload)
	case "cursor":
		s.recordCursorHook(ctx, cursorPayloadFromIngest(payload, actorEmail), authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID, blockReason)
	case "codex":
		codexPayload := codexPayloadFromIngest(payload, actorEmail)
		metadata := authenticatedSessionMetadata(authCtx, conv.PtrValOr(codexPayload.SessionID, ""), "Codex", actorEmail)
		s.recordCodexHook(ctx, codexPayload, &metadata, blockReason)
	default:
		// Custom hook sources are accepted by /rpc/hooks.ingest, but the
		// current storage schema is still shaped around built-in adapters.
	}
}

func ingestConversationID(payload *gen.IngestPayload) string {
	if id := strings.TrimSpace(conv.PtrValOr(payload.ConversationID, "")); id != "" {
		return id
	}
	return strings.TrimSpace(conv.PtrValOr(payload.SessionID, ""))
}

func rawIngestHookEventName(payload *gen.IngestPayload) string {
	return strings.TrimSpace(conv.PtrValOr(payload.HookEventName, ""))
}

func ingestToolOutput(payload *gen.IngestPayload) any {
	if payload.ToolOutput != nil {
		return payload.ToolOutput
	}
	if payload.ToolResponse != nil {
		return payload.ToolResponse
	}
	if payload.ResultJSON != nil {
		return *payload.ResultJSON
	}
	return nil
}

func legacyHookEventName(payload *gen.IngestPayload) string {
	if raw := rawIngestHookEventName(payload); raw != "" {
		return raw
	}

	switch strings.TrimSpace(payload.HookSource) {
	case "claude":
		switch hookevents.EventType(payload.EventType) {
		case hookevents.EventTypeSessionStart:
			return "SessionStart"
		case hookevents.EventTypeConfigChange:
			return "ConfigChange"
		case hookevents.EventTypeBeforeToolUse:
			return "PreToolUse"
		case hookevents.EventTypeAfterToolUse:
			return "PostToolUse"
		case hookevents.EventTypeAfterToolUseFailure:
			return "PostToolUseFailure"
		case hookevents.EventTypeUserPromptSubmit:
			return "UserPromptSubmit"
		case hookevents.EventTypeStop:
			return "Stop"
		case hookevents.EventTypeSessionEnd:
			return "SessionEnd"
		case hookevents.EventTypeNotification:
			return "Notification"
		default:
		}
	case "cursor":
		switch hookevents.EventType(payload.EventType) {
		case hookevents.EventTypeSessionStart:
			return "sessionStart"
		case hookevents.EventTypeUserPromptSubmit:
			return "beforeSubmitPrompt"
		case hookevents.EventTypeAfterAgentResponse:
			return "afterAgentResponse"
		case hookevents.EventTypeAfterAgentThought:
			return "afterAgentThought"
		case hookevents.EventTypeBeforeToolUse:
			return "preToolUse"
		case hookevents.EventTypeAfterToolUse:
			return "postToolUse"
		case hookevents.EventTypeAfterToolUseFailure:
			return "postToolUseFailure"
		case hookevents.EventTypeBeforeMCPExecution:
			return "beforeMCPExecution"
		case hookevents.EventTypeAfterMCPExecution:
			return "afterMCPExecution"
		case hookevents.EventTypeStop:
			return "stop"
		default:
		}
	case "codex":
		switch hookevents.EventType(payload.EventType) {
		case hookevents.EventTypeSessionStart:
			return "SessionStart"
		case hookevents.EventTypeBeforeToolUse:
			return "PreToolUse"
		case hookevents.EventTypeAfterToolUse:
			return "PostToolUse"
		case hookevents.EventTypePermissionRequest:
			return "PermissionRequest"
		case hookevents.EventTypeUserPromptSubmit:
			return "UserPromptSubmit"
		case hookevents.EventTypeStop:
			return "Stop"
		default:
		}
	default:
	}
	return payload.EventType
}

func allowedIngestResult(payload *gen.IngestPayload) *gen.IngestHookResult {
	result := emptyIngestHookResult()
	switch strings.TrimSpace(payload.HookSource) {
	case "claude":
		if hookevents.EventType(payload.EventType) == hookevents.EventTypeSessionStart {
			continueVal := true
			result.Continue = &continueVal
		}
		if hookevents.EventType(payload.EventType) == hookevents.EventTypeBeforeToolUse {
			hookEventName := legacyHookEventName(payload)
			allow := "allow"
			result.HookSpecificOutput = &HookSpecificOutput{
				HookEventName:            &hookEventName,
				AdditionalContext:        nil,
				PermissionDecision:       &allow,
				PermissionDecisionReason: nil,
			}
		}
	case "cursor":
		eventType := hookevents.EventType(payload.EventType)
		if eventType == hookevents.EventTypeBeforeToolUse || eventType == hookevents.EventTypeBeforeMCPExecution {
			allow := "allow"
			result.Permission = &allow
		}
	default:
	}
	return result
}

func blockedIngestResult(payload *gen.IngestPayload, reason string) *gen.IngestHookResult {
	if strings.TrimSpace(payload.HookSource) == "claude" {
		return ingestResultFromClaude(constructBlockResponse(legacyHookEventName(payload), reason))
	}

	result := emptyIngestHookResult()
	deny := "deny"
	result.Decision = &deny
	result.Reason = &reason
	result.Permission = &deny
	result.UserMessage = &reason
	result.AgentMessage = &reason
	return result
}

func emptyIngestHookResult() *gen.IngestHookResult {
	return &gen.IngestHookResult{
		Continue:           nil,
		StopReason:         nil,
		SuppressOutput:     nil,
		SystemMessage:      nil,
		HookSpecificOutput: nil,
		Decision:           nil,
		Reason:             nil,
		Permission:         nil,
		UserMessage:        nil,
		AdditionalContext:  nil,
		AgentMessage:       nil,
	}
}

func authenticatedSessionMetadata(authCtx *contextvalues.AuthContext, sessionID, serviceName, actorEmail string) SessionMetadata {
	return SessionMetadata{
		SessionID:   sessionID,
		ServiceName: serviceName,
		UserEmail:   actorEmail,
		UserID:      authCtx.UserID,
		ClaudeOrgID: "",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}
}

func claudePayloadFromIngest(payload *gen.IngestPayload, actorEmail string) *gen.ClaudePayload {
	return &gen.ClaudePayload{
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		HookHostname:         payload.HookHostname,
		IdempotencyKey:       payload.IdempotencyKey,
		HookEventName:        legacyHookEventName(payload),
		ToolName:             payload.ToolName,
		ToolUseID:            payload.ToolUseID,
		ToolInput:            payload.ToolInput,
		ToolResponse:         payload.ToolResponse,
		Error:                payload.Error,
		IsInterrupt:          payload.IsInterrupt,
		SessionID:            payload.SessionID,
		UserEmail:            &actorEmail,
		Cwd:                  payload.Cwd,
		TranscriptPath:       payload.TranscriptPath,
		AdditionalData:       payload.AdditionalData,
		Source:               payload.Source,
		Model:                payload.Model,
		Prompt:               payload.Prompt,
		LastAssistantMessage: payload.LastAssistantMessage,
		StopHookActive:       payload.StopHookActive,
		Reason:               payload.Reason,
		NotificationType:     payload.NotificationType,
		Message:              payload.Message,
		Title:                payload.Title,
	}
}

func cursorPayloadFromIngest(payload *gen.IngestPayload, actorEmail string) *gen.CursorPayload {
	conversationID := payload.ConversationID
	if conversationID == nil {
		conversationID = payload.SessionID
	}
	return &gen.CursorPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		HookHostname:     payload.HookHostname,
		IdempotencyKey:   payload.IdempotencyKey,
		HookEventName:    legacyHookEventName(payload),
		ConversationID:   conversationID,
		GenerationID:     payload.GenerationID,
		Model:            payload.Model,
		CursorVersion:    payload.CursorVersion,
		UserEmail:        &actorEmail,
		SessionID:        payload.SessionID,
		ToolName:         payload.ToolName,
		ToolUseID:        payload.ToolUseID,
		ToolInput:        payload.ToolInput,
		ToolResponse:     payload.ToolResponse,
		Error:            payload.Error,
		IsInterrupt:      payload.IsInterrupt,
		AdditionalData:   payload.AdditionalData,
		Prompt:           payload.Prompt,
		ComposerMode:     payload.ComposerMode,
		TranscriptPath:   payload.TranscriptPath,
		Status:           payload.Status,
		LoopCount:        payload.LoopCount,
		InputTokens:      payload.InputTokens,
		OutputTokens:     payload.OutputTokens,
		CacheReadTokens:  payload.CacheReadTokens,
		CacheWriteTokens: payload.CacheWriteTokens,
		Text:             payload.Text,
		DurationMs:       payload.DurationMs,
		URL:              payload.URL,
		Command:          payload.Command,
		ResultJSON:       payload.ResultJSON,
		Duration:         payload.Duration,
	}
}

func codexPayloadFromIngest(payload *gen.IngestPayload, actorEmail string) *gen.CodexPayload {
	return &gen.CodexPayload{
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		HookHostname:         payload.HookHostname,
		IdempotencyKey:       payload.IdempotencyKey,
		HookEventName:        legacyHookEventName(payload),
		SessionID:            payload.SessionID,
		UserEmail:            &actorEmail,
		AdditionalData:       payload.AdditionalData,
		TranscriptPath:       payload.TranscriptPath,
		Cwd:                  payload.Cwd,
		Model:                payload.Model,
		ToolName:             payload.ToolName,
		ToolInput:            payload.ToolInput,
		ToolOutput:           payload.ToolOutput,
		PermissionType:       payload.PermissionType,
		Prompt:               payload.Prompt,
		LastAssistantMessage: payload.LastAssistantMessage,
	}
}

func ingestResultFromClaude(result *gen.ClaudeHookResult) *gen.IngestHookResult {
	if result == nil {
		return &gen.IngestHookResult{Continue: nil, StopReason: nil, SuppressOutput: nil, SystemMessage: nil, HookSpecificOutput: nil, Decision: nil, Reason: nil, Permission: nil, UserMessage: nil, AdditionalContext: nil, AgentMessage: nil}
	}
	return &gen.IngestHookResult{
		Continue:           result.Continue,
		StopReason:         result.StopReason,
		SuppressOutput:     result.SuppressOutput,
		SystemMessage:      result.SystemMessage,
		HookSpecificOutput: result.HookSpecificOutput,
		Decision:           result.Decision,
		Reason:             result.Reason,
		Permission:         nil,
		UserMessage:        nil,
		AdditionalContext:  nil,
		AgentMessage:       nil,
	}
}
