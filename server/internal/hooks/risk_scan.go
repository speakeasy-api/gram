package hooks

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

func (s *Service) scanUserPromptForEnforcement(ctx context.Context, ev *hookevents.UserPromptSubmit) *risk.ScanResult {
	if ev == nil {
		return nil
	}
	return s.scanHookEventForEnforcement(ctx, ev.Event, ev.Prompt, message.User, "")
}

func (s *Service) scanToolRequestForEnforcement(ctx context.Context, ev *hookevents.BeforeToolUse) *risk.ScanResult {
	if ev == nil {
		return nil
	}
	return s.scanHookEventForEnforcement(ctx, ev.Event, marshalToJSON(ev.ToolInput), message.ToolRequest, ev.ToolName)
}

func (s *Service) scanMCPRequestForEnforcement(ctx context.Context, ev *hookevents.BeforeMCPExecution) *risk.ScanResult {
	if ev == nil {
		return nil
	}
	return s.scanHookEventForEnforcement(ctx, ev.Event, marshalToJSON(ev.ToolInput), message.ToolRequest, ev.ToolName)
}

func (s *Service) scanPermissionRequestForEnforcement(ctx context.Context, ev *hookevents.PermissionRequest) *risk.ScanResult {
	if ev == nil {
		return nil
	}
	return s.scanHookEventForEnforcement(ctx, ev.Event, marshalToJSON(ev.ToolInput), message.ToolRequest, ev.ToolName)
}

func (s *Service) scanHookEventForEnforcement(ctx context.Context, ev hookevents.Event, text string, messageType message.Type, toolName string) *risk.ScanResult {
	if s.riskScanner == nil {
		return nil
	}

	// Empty body + tool attribution still matters for tool-scoped policies; only
	// skip when there is neither.
	if text == "" && toolName == "" {
		return nil
	}

	if messageType == "" {
		return nil
	}

	if ev.OrganizationID == "" || ev.ProjectID == uuid.Nil {
		return nil
	}

	result, err := s.riskScanner.ScanForEnforcement(ctx, ev.OrganizationID, ev.ProjectID, ev.UserID, text, messageType, toolName)
	if err != nil {
		s.logger.WarnContext(ctx, "risk scan failed for hook event",
			attr.SlogError(err),
			attr.SlogEvent("risk_scan_error"),
			attr.SlogHookSource(string(ev.Provider)),
			attr.SlogHookEvent(ev.RawEventType),
		)
		return nil
	}

	return result
}

// renderUserBlockReason returns the message shown to the agent when a tool
// call or prompt is denied. When the policy carries a non-empty user_message,
// that overrides the default Speakeasy-branded format; otherwise the supplied
// audit reason is rendered verbatim. The audit reason itself is what gets
// stored in ClickHouse traces — the user_message override only affects what
// the agent / end user sees.
func renderUserBlockReason(userMessage *string, auditReason string) string {
	if userMessage != nil && *userMessage != "" {
		return *userMessage
	}
	return auditReason
}
