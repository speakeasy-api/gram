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

func (s *Service) Codex(ctx context.Context, payload *gen.CodexPayload) (*gen.CodexHookResult, error) {
	logger := s.logger.With(
		attr.SlogHookSource("codex"),
		attr.SlogHookEvent(payload.HookEventName),
		attr.SlogToolName(conv.PtrValOr(payload.ToolName, "")),
		attr.SlogGenAIConversationID(conv.PtrValOr(payload.SessionID, "")),
	)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		logger.WarnContext(ctx, "rejected unauthorized codex hook request",
			attr.SlogEvent("codex_hook_unauthorized"),
		)
		return nil, oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()
	logger = logger.With(
		attr.SlogOrganizationID(orgID),
		attr.SlogProjectID(projectID),
	)

	logger.InfoContext(ctx, fmt.Sprintf("🪝 HOOK Codex: %s", payload.HookEventName),
		attr.SlogEvent("codex_hook"),
	)

	var blockReason, userReason string

	switch payload.HookEventName {
	case "PreToolUse":
		if scanResult := s.scanCodexForEnforcement(ctx, payload, orgID, projectID); scanResult != nil {
			blockReason = fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason = renderUserBlockReason(scanResult.UserMessage, blockReason)
			break
		}
		policy := s.lookupShadowMCPBlockingPolicy(ctx, projectID)
		if policy != nil {
			toolName := conv.PtrValOr(payload.ToolName, "")
			if detail, denied := s.shadowMCPClient.ValidateToolsetCall(ctx, payload.ToolInput, toolName, orgID); denied {
				logger.InfoContext(ctx, "denying codex tool call: failed gram toolset validation",
					attr.SlogEvent("codex_hook_denied"),
					attr.SlogHookBlockReason(detail),
					attr.SlogRiskPolicyID(policy.ID),
					attr.SlogRiskPolicyName(policy.Name),
				)
				blockReason = fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
				userReason = renderUserBlockReason(policy.UserMessage, blockReason)
			}
		}
	case "PermissionRequest":
		if scanResult := s.scanCodexForEnforcement(ctx, payload, orgID, projectID); scanResult != nil {
			blockReason = fmt.Sprintf("Speakeasy blocked this permission request: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason = renderUserBlockReason(scanResult.UserMessage, blockReason)
		}
	case "UserPromptSubmit":
		if scanResult := s.scanCodexForEnforcement(ctx, payload, orgID, projectID); scanResult != nil {
			blockReason = fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason = renderUserBlockReason(scanResult.UserMessage, blockReason)
		}
	default:
		// Non-blocking events: telemetry only.
	}

	s.recordCodexHook(ctx, payload, orgID, projectID, blockReason)

	if blockReason != "" {
		// HTTP 4xx causes the hook.sh to exit 2, which signals a block to the Codex CLI.
		return nil, oops.E(oops.CodeForbidden, nil, "%s", userReason)
	}

	return &gen.CodexHookResult{
		Decision: nil,
		Reason:   nil,
	}, nil
}

func (s *Service) recordCodexHook(ctx context.Context, payload *gen.CodexPayload, orgID, projectID, blockReason string) {
	// Codex does not send user_email; events are attributed to org/project only.
	metadata := &SessionMetadata{
		SessionID:   conv.PtrValOr(payload.SessionID, ""),
		ServiceName: "Codex",
		UserEmail:   "",
		UserID:      "",
		ClaudeOrgID: "",
		GramOrgID:   orgID,
		ProjectID:   projectID,
	}

	s.writeCodexHookToClickHouse(ctx, payload, orgID, projectID, blockReason)

	switch payload.HookEventName {
	case "PreToolUse":
		if err := s.writeCodexToolCallRequestToPG(ctx, payload, metadata); err != nil {
			s.logger.ErrorContext(ctx, "failed to persist Codex tool call request", attr.SlogError(err))
		}
	case "PostToolUse":
		if err := s.writeCodexToolCallResultToPG(ctx, payload, metadata); err != nil {
			s.logger.ErrorContext(ctx, "failed to persist Codex tool call result", attr.SlogError(err))
		}
	case "UserPromptSubmit":
		if err := s.writeCodexUserPromptToPG(ctx, payload, metadata); err != nil {
			s.logger.ErrorContext(ctx, "failed to persist Codex user prompt", attr.SlogError(err))
		}
	}
}

func (s *Service) writeCodexHookToClickHouse(ctx context.Context, payload *gen.CodexPayload, orgID, projectID, blockReason string) {
	attrs := s.buildCodexTelemetryAttributes(ctx, payload, orgID, projectID)
	if blockReason != "" {
		attrs[attr.HookBlockReasonKey] = blockReason
	}
	toolName, _ := attrs[attr.ToolNameKey].(string)

	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid project ID for Codex hook", attr.SlogError(err))
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

		s.logger.DebugContext(ctx, "wrote Codex hook to ClickHouse",
			attr.SlogEvent("codex_hook_written"),
		)
	}
}

func (s *Service) buildCodexTelemetryAttributes(ctx context.Context, payload *gen.CodexPayload, orgID, projectID string) map[attr.Key]any {
	toolName := conv.PtrValOr(payload.ToolName, "")

	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.ToolNameKey:       toolName,
		attr.HookEventKey:      payload.HookEventName,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        generateTraceID(),
		attr.LogBodyKey:        fmt.Sprintf("Hook: %s", payload.HookEventName),
		attr.UserEmailKey:      "",
		attr.ProjectIDKey:      projectID,
		attr.OrganizationIDKey: orgID,
		attr.HookSourceKey:     "codex",
	}

	if payload.Model != nil && *payload.Model != "" {
		attrs[attr.GenAIResponseModelKey] = *payload.Model
	}

	if payload.SessionID != nil && *payload.SessionID != "" {
		attrs[attr.GenAIConversationIDKey] = *payload.SessionID
		attrs[attr.TraceIDKey] = hashToolCallIDToTraceID(*payload.SessionID)
	}

	if payload.HookEventName == "UserPromptSubmit" && payload.Prompt != nil && *payload.Prompt != "" {
		attrs[attr.LogBodyKey] = *payload.Prompt
	}

	// Stringify ToolInput / ToolOutput to prevent ClickHouse key explosion.
	if payload.ToolInput != nil {
		if jsonBytes, err := json.Marshal(payload.ToolInput); err == nil {
			attrs[attr.GenAIToolCallArgumentsKey] = string(jsonBytes)
		} else {
			s.logger.WarnContext(ctx, "failed to marshal Codex ToolInput", attr.SlogError(err))
		}
	}
	if payload.ToolOutput != nil {
		if jsonBytes, err := json.Marshal(payload.ToolOutput); err == nil {
			attrs[attr.GenAIToolCallResultKey] = string(jsonBytes)
		} else {
			s.logger.WarnContext(ctx, "failed to marshal Codex ToolOutput", attr.SlogError(err))
		}
	}

	// Parse MCP tool names using the mcp__<server>__<tool> convention.
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.SplitN(toolName, "__", 3)
		if len(parts) == 3 {
			attrs[attr.ToolCallSourceKey] = parts[1]
			attrs[attr.ToolNameKey] = parts[2]
		}
	}

	return attrs
}

func (s *Service) writeCodexToolCallRequestToPG(ctx context.Context, payload *gen.CodexPayload, metadata *SessionMetadata) error {
	if metadata.SessionID == "" {
		return nil
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	chatID := sessionIDToUUID(metadata.SessionID)

	toolCalls := []map[string]any{{
		"id":   conv.PtrValOr(payload.ToolName, ""),
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
		Source:           conv.ToPGText("Codex"),
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

	return s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, activities.DefaultCodexChatTitle)
}

func (s *Service) writeCodexToolCallResultToPG(ctx context.Context, payload *gen.CodexPayload, metadata *SessionMetadata) error {
	if metadata.SessionID == "" {
		return nil
	}

	if payload.ToolOutput == nil {
		return nil
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	chatID := sessionIDToUUID(metadata.SessionID)

	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "tool",
		Content:          marshalToJSON(payload.ToolOutput),
		UserID:           conv.ToPGTextEmpty(metadata.UserID),
		Source:           conv.ToPGText("Codex"),
		ToolCallID:       conv.ToPGTextEmpty(conv.PtrValOr(payload.ToolName, "")),
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		ContentRaw:       nil,
		ContentAssetUrl:  conv.ToPGTextEmpty(""),
		StorageError:     conv.ToPGTextEmpty(""),
		MessageID:        conv.ToPGTextEmpty(""),
		ExternalUserID:   conv.ToPGTextEmpty(metadata.UserEmail),
		FinishReason:     conv.ToPGTextEmpty(""),
		ToolCalls:        nil,
		Model:            conv.ToPGTextEmpty(""),
		Origin:           conv.ToPGTextEmpty(""),
		UserAgent:        conv.ToPGTextEmpty(""),
		IpAddress:        conv.ToPGTextEmpty(""),
		ContentHash:      nil,
		Generation:       0,
	}

	return s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, activities.DefaultCodexChatTitle)
}

func (s *Service) writeCodexUserPromptToPG(ctx context.Context, payload *gen.CodexPayload, metadata *SessionMetadata) error {
	if metadata.SessionID == "" {
		return nil
	}

	content := conv.PtrValOr(payload.Prompt, "")
	if content == "" {
		return nil
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID for Codex user prompt: %w", err)
	}

	chatID := sessionIDToUUID(metadata.SessionID)

	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "user",
		Content:          content,
		Model:            conv.ToPGTextEmpty(""),
		UserID:           conv.ToPGTextEmpty(metadata.UserID),
		Source:           conv.ToPGText("Codex"),
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

	return s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, activities.DefaultCodexChatTitle)
}
