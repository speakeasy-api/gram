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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

const vscodeCopilotSource = "copilot"

// VscodeCopilot is the endpoint for VSCode Copilot agent hook events.
func (s *Service) VscodeCopilot(ctx context.Context, payload *gen.VscodeCopilotPayload) (*gen.VSCodeCopilotHookResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	s.logger.InfoContext(ctx, fmt.Sprintf("🪝 HOOK VSCode Copilot: %s", payload.HookEventName),
		attr.SlogEvent("vscode_copilot_hook"),
		attr.SlogValueAny(map[string]any{
			"hookEventName": payload.HookEventName,
			"toolName":      conv.PtrValOr(payload.ToolName, ""),
		}),
	)

	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()

	result := &gen.VSCodeCopilotHookResult{
		Continue:           nil,
		StopReason:         nil,
		SuppressOutput:     nil,
		SystemMessage:      nil,
		HookSpecificOutput: nil,
	}

	// blockReason is empty unless the risk scanner denies this call. It
	// propagates into the ClickHouse log entry as gram.hook.block_reason.
	var blockReason string

	switch payload.HookEventName {
	case "PreToolUse":
		if scanResult := s.scanVSCodeForEnforcement(ctx, payload, projectID); scanResult != nil {
			msg := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			blockReason = msg
			deny := "deny"
			result.HookSpecificOutput = map[string]any{
				"hookEventName":            payload.HookEventName,
				"permissionDecision":       deny,
				"permissionDecisionReason": msg,
			}
			result.SystemMessage = &msg
		}
	case "UserPromptSubmit":
		if scanResult := s.scanVSCodeForEnforcement(ctx, payload, projectID); scanResult != nil {
			msg := fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			blockReason = msg
			deny := "deny"
			result.HookSpecificOutput = map[string]any{
				"hookEventName":            payload.HookEventName,
				"permissionDecision":       deny,
				"permissionDecisionReason": msg,
			}
			result.SystemMessage = &msg
		}
	default:
		// Other events (SessionStart, PostToolUse, PreCompact, Subagent*, Stop)
		// are recorded for observability but not gated.
	}

	s.recordVSCodeHook(ctx, payload, orgID, projectID, blockReason)

	return result, nil
}

func (s *Service) recordVSCodeHook(ctx context.Context, payload *gen.VscodeCopilotPayload, orgID, projectID, blockReason string) {
	if s.telemetryLogger == nil {
		return
	}

	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Invalid project ID for VSCode Copilot hook", attr.SlogError(err))
		return
	}

	attrs := s.buildVSCodeTelemetryAttributes(ctx, payload, orgID, projectID)
	if blockReason != "" {
		attrs[attr.HookBlockReasonKey] = blockReason
	}

	toolName, _ := attrs[attr.ToolNameKey].(string)

	toolInfo := telemetry.ToolInfo{
		Name:           toolName,
		OrganizationID: orgID,
		ProjectID:      parsedProjectID.String(),
		ID:             "",
		URN:            "",
		DeploymentID:   "",
		FunctionID:     nil,
	}

	s.telemetryLogger.Log(ctx, telemetry.LogParams{
		Timestamp:  time.Now(),
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})

	s.logger.DebugContext(ctx, "Wrote VSCode Copilot hook to ClickHouse",
		attr.SlogEvent("vscode_copilot_hook_written"),
	)
}

// buildVSCodeTelemetryAttributes assembles the ClickHouse attributes for a
// VSCode Copilot hook event.
func (s *Service) buildVSCodeTelemetryAttributes(ctx context.Context, payload *gen.VscodeCopilotPayload, orgID, projectID string) map[attr.Key]any {
	toolName := conv.PtrValOr(payload.ToolName, "")
	userEmail := conv.PtrValOr(payload.UserEmailInput, "")
	userEmailSource := conv.PtrValOr(payload.UserEmailSourceInput, "")

	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.ToolNameKey:       toolName,
		attr.HookEventKey:      payload.HookEventName,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        generateTraceID(),
		attr.LogBodyKey:        fmt.Sprintf("Hook: %s", payload.HookEventName),
		attr.UserEmailKey:      userEmail,
		attr.ProjectIDKey:      projectID,
		attr.OrganizationIDKey: orgID,
		attr.HookSourceKey:     vscodeCopilotSource,
	}

	if userEmailSource != "" {
		attrs[attr.UserEmailSourceKey] = userEmailSource
	}

	if payload.SessionID != nil && *payload.SessionID != "" {
		attrs[attr.GenAIConversationIDKey] = *payload.SessionID
	}

	// MCP tool naming convention used across Claude/Cursor: mcp__<server>__<tool>
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.SplitN(toolName, "__", 3)
		if len(parts) == 3 {
			attrs[attr.ToolCallSourceKey] = parts[1]
			attrs[attr.ToolNameKey] = parts[2]
		}
	}

	if payload.ToolUseID != nil && *payload.ToolUseID != "" {
		attrs[attr.TraceIDKey] = hashToolCallIDToTraceID(*payload.ToolUseID)
		attrs[attr.GenAIToolCallIDKey] = *payload.ToolUseID
	}

	// UserPromptSubmit: surface the prompt text as the log body so downstream
	// dashboards can render the original user message.
	if payload.HookEventName == "UserPromptSubmit" && payload.Prompt != nil && *payload.Prompt != "" {
		attrs[attr.LogBodyKey] = *payload.Prompt
	}

	if payload.ToolInput != nil {
		if jsonBytes, err := json.Marshal(payload.ToolInput); err == nil {
			attrs[attr.GenAIToolCallArgumentsKey] = string(jsonBytes)
		} else {
			s.logger.WarnContext(ctx, "Failed to marshal VSCode Copilot ToolInput", attr.SlogError(err))
		}
	}
	if payload.ToolResponse != nil {
		if jsonBytes, err := json.Marshal(payload.ToolResponse); err == nil {
			attrs[attr.GenAIToolCallResultKey] = string(jsonBytes)
		} else {
			s.logger.WarnContext(ctx, "Failed to marshal VSCode Copilot ToolResponse", attr.SlogError(err))
		}
	}

	if payload.AgentID != nil && *payload.AgentID != "" {
		attrs[attr.HookAgentIDKey] = *payload.AgentID
	}
	if payload.AgentType != nil && *payload.AgentType != "" {
		attrs[attr.HookAgentTypeKey] = *payload.AgentType
	}

	return attrs
}
