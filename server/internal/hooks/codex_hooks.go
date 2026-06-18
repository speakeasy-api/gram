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
		return &gen.CodexHookResult{
			Decision: new("deny"),
			Reason:   new("Speakeasy hooks: unauthorized — check your Gram API key and project slug."),
		}, nil
	}

	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()
	metadata := s.codexSessionMetadata(ctx, payload, orgID, projectID)
	if metadata.UserEmail == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "codex hook payload missing user_email")
	}
	logger = logger.With(
		attr.SlogOrganizationID(orgID),
		attr.SlogProjectID(projectID),
	)

	logger.InfoContext(ctx, "codex hook received",
		attr.SlogEvent("codex_hook"),
	)

	var blockReason, userReason string

	switch payload.HookEventName {
	case "PreToolUse":
		if scanResult := s.scanCodexForEnforcement(ctx, payload, orgID, projectID, metadata.UserID); scanResult != nil {
			blockReason = fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason = renderUserBlockReason(scanResult.UserMessage, blockReason)
			break
		}
		policy := s.lookupShadowMCPBlockingPolicy(ctx, orgID, projectID, metadata.UserID)
		if policy != nil {
			toolName := conv.PtrValOr(payload.ToolName, "")
			evidence, matched := s.codexShadowMCPEvidence(ctx, payload)
			detail, denied := s.enforceShadowMCPToolAccess(ctx, orgID, projectID, metadata.UserID, policy.ID, payload.ToolInput, toolName, evidence)
			if !denied {
				// Toolset validation proves a valid x-gram-toolset-id was
				// echoed, but a shadow server's schema can coach the client
				// into copying one. The inventory snapshot pins where the
				// call actually routes — deny when it points at a non-Gram
				// target. Unmatched prefixes and missing snapshots stay
				// allowed: older plugin installs ship no inventory.
				if d := s.codexInventoryProvenanceDetail(ctx, matched, orgID); d != "" {
					if _, allowed := s.canBypassPolicy(ctx, orgID, metadata.UserID, policy.ID, evidence, toolName); !allowed {
						detail, denied = d, true
					}
				}
			}
			if denied {
				logger.InfoContext(ctx, "denying codex tool call: failed gram toolset validation",
					attr.SlogEvent("codex_hook_denied"),
					attr.SlogHookBlockReason(detail),
					attr.SlogRiskPolicyID(policy.ID),
					attr.SlogRiskPolicyName(policy.Name),
				)
				blockReason = fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
				userReason = s.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
					OrganizationID:  orgID,
					ProjectID:       projectID,
					RequesterUserID: metadata.UserID,
					UserMessage:     policy.UserMessage,
					AuditReason:     blockReason,
					Evidence:        evidence,
					ToolName:        toolName,
					ToolInput:       payload.ToolInput,
					RiskPolicyID:    policy.ID,
				})
			}
		}
	case "PermissionRequest":
		if scanResult := s.scanCodexForEnforcement(ctx, payload, orgID, projectID, metadata.UserID); scanResult != nil {
			blockReason = fmt.Sprintf("Speakeasy blocked this permission request: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason = renderUserBlockReason(scanResult.UserMessage, blockReason)
		}
	case "UserPromptSubmit":
		if scanResult := s.scanCodexForEnforcement(ctx, payload, orgID, projectID, metadata.UserID); scanResult != nil {
			blockReason = fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason = renderUserBlockReason(scanResult.UserMessage, blockReason)
		}
	default:
		// Non-blocking events: telemetry only.
	}

	s.recordCodexHook(ctx, payload, metadata, blockReason)

	if blockReason != "" {
		// Return the Codex hook JSON shape (decision=deny + reason) so the
		// Codex CLI surfaces the block reason to the user. Returning a 4xx
		// here would hide the reason behind whatever generic message the
		// transport layer renders.
		return &gen.CodexHookResult{
			Decision: new("deny"),
			Reason:   &userReason,
		}, nil
	}

	return &gen.CodexHookResult{
		Decision: nil,
		Reason:   nil,
	}, nil
}

func (s *Service) recordCodexHook(ctx context.Context, payload *gen.CodexPayload, metadata *SessionMetadata, blockReason string) {
	if payload.HookEventName == "SessionStart" {
		s.captureCodexMCPListSnapshot(ctx, payload)
		if metadata.SessionID != "" && metadata.UserEmail != "" {
			if err := s.cache.Set(ctx, sessionCacheKey(metadata.SessionID), *metadata, 24*time.Hour); err != nil {
				s.logger.WarnContext(ctx, "failed to cache Codex session metadata",
					attr.SlogError(err),
					attr.SlogGenAIConversationID(metadata.SessionID),
				)
			}
		}
	} else {
		s.refreshMCPListTTL(ctx, metadata.SessionID)
	}

	s.writeCodexHookToClickHouse(ctx, payload, metadata, blockReason)

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
	case "Stop":
		if err := s.writeCodexAssistantResponseToPG(ctx, payload, metadata); err != nil {
			s.logger.ErrorContext(ctx, "failed to persist Codex assistant response", attr.SlogError(err))
		}
	}
}

// captureCodexMCPListSnapshot parses the MCP inventory shipped by the Codex
// SessionStart hook script (additional_data.mcp_inventory_codex, the parsed
// output of `codex mcp list --json`) and caches it under
// sessionMCPListCacheKey, sharing the snapshot shape and cache key with the
// Claude flows so downstream matching and telemetry enrichment work
// unchanged.
func (s *Service) captureCodexMCPListSnapshot(ctx context.Context, payload *gen.CodexPayload) {
	if payload.SessionID == nil || *payload.SessionID == "" || payload.AdditionalData == nil {
		return
	}
	raw := payload.AdditionalData["mcp_inventory_codex"]
	if raw == nil {
		return
	}

	entries := ParseCodexMCPList(raw)
	if err := s.cache.Set(ctx, sessionMCPListCacheKey(*payload.SessionID), entries, sessionMCPListTTL); err != nil {
		s.logger.WarnContext(ctx, "failed to cache Codex MCP list snapshot",
			attr.SlogEvent("codex_hook_mcp_list_cache_set_failed"),
			attr.SlogError(err),
			attr.SlogGenAIConversationID(*payload.SessionID),
		)
	}
}

func (s *Service) codexSessionMetadata(ctx context.Context, payload *gen.CodexPayload, orgID, projectID string) *SessionMetadata {
	metadata := &SessionMetadata{
		SessionID:   conv.PtrValOr(payload.SessionID, ""),
		ServiceName: "Codex",
		UserEmail:   strings.TrimSpace(conv.PtrValOr(payload.UserEmail, "")),
		UserID:      "",
		ClaudeOrgID: "",
		GramOrgID:   orgID,
		ProjectID:   projectID,
	}

	if metadata.SessionID != "" {
		cached, err := s.getSessionMetadata(ctx, metadata.SessionID)
		if err == nil && cached.ServiceName == "Codex" && cached.GramOrgID == orgID && cached.ProjectID == projectID {
			if metadata.UserEmail == "" {
				metadata.UserEmail = cached.UserEmail
			}
		}
	}

	if metadata.UserEmail != "" {
		metadata.UserID = s.resolveUserByEmail(ctx, metadata.UserEmail, orgID)
	} else {
		metadata.UserID = ""
	}

	return metadata
}

func (s *Service) writeCodexHookToClickHouse(ctx context.Context, payload *gen.CodexPayload, metadata *SessionMetadata, blockReason string) {
	attrs := s.buildCodexTelemetryAttributes(ctx, payload, metadata)
	if blockReason != "" {
		attrs[attr.HookBlockReasonKey] = blockReason
	}
	toolName, _ := attrs[attr.ToolNameKey].(string)

	parsedProjectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid project ID for Codex hook", attr.SlogError(err))
		return
	}

	toolInfo := telemetry.ToolInfo{
		Name:           toolName,
		OrganizationID: metadata.GramOrgID,
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

func (s *Service) buildCodexTelemetryAttributes(ctx context.Context, payload *gen.CodexPayload, metadata *SessionMetadata) map[attr.Key]any {
	toolName := conv.PtrValOr(payload.ToolName, "")

	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.ToolNameKey:       toolName,
		attr.HookEventKey:      payload.HookEventName,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        generateTraceID(),
		attr.LogBodyKey:        fmt.Sprintf("Hook: %s", payload.HookEventName),
		attr.UserEmailKey:      metadata.UserEmail,
		attr.ProjectIDKey:      metadata.ProjectID,
		attr.OrganizationIDKey: metadata.GramOrgID,
		attr.HookSourceKey:     "codex",
	}
	if metadata.UserID != "" {
		attrs[attr.UserIDKey] = metadata.UserID
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
			if metadata.SessionID != "" {
				if entries, err := s.getCachedMCPList(ctx, metadata.SessionID); err == nil {
					matched := matchCodexCachedMCPEntry(entries, toolName)
					if matched != nil && matched.ToolPrefix != "" {
						// Sanitized prefixes can contain "__", in which case
						// the naive split misattributes part of the server
						// name to the tool — recompute from the full prefix.
						if rest, ok := strings.CutPrefix(toolName, "mcp__"+matched.ToolPrefix+"__"); ok {
							attrs[attr.ToolCallSourceKey] = matched.ToolPrefix
							attrs[attr.ToolNameKey] = rest
						}
					}
					if v := resolvedMCPMatch(matched, parts[1]); v != "" {
						attrs[attr.MCPMatchKey] = v
					}
					applyMCPInventoryAttrs(attrs, matched)
				}
			}
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

func (s *Service) writeCodexAssistantResponseToPG(ctx context.Context, payload *gen.CodexPayload, metadata *SessionMetadata) error {
	if metadata.SessionID == "" {
		return nil
	}

	content := conv.PtrValOr(payload.LastAssistantMessage, "")
	if content == "" {
		return nil
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID for Codex assistant response: %w", err)
	}

	chatID := sessionIDToUUID(metadata.SessionID)

	msgParams := chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "assistant",
		Content:          content,
		Model:            conv.ToPGTextEmpty(conv.PtrValOr(payload.Model, "")),
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

	if err := s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, msgParams, activities.DefaultCodexChatTitle); err != nil {
		return err
	}

	if s.chatTitleGenerator != nil {
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
