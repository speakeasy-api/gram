package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	hooksgen "github.com/speakeasy-api/gram/server/gen/hooks"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	assistantHookAdapter      = "assistant"
	assistantHookSchemaV1     = "hook.ingest.v1"
	consultDecisionAllow      = "allow"
	consultDecisionDeny       = "deny"
	consultDefaultDenyMessage = "Request denied by Speakeasy policy."
)

// HookIngester is the trusted in-process canonical ingest surface used to
// subject hosted assistant tool calls to the same risk-policy and spend-gate
// enforcement as hook-ingested agent traffic.
type HookIngester interface {
	// IngestAssistantToolCall applies canonical hook evaluation for a hosted
	// assistant tool call, including the reserved assistant adapter.
	IngestAssistantToolCall(context.Context, *contextvalues.AuthContext, *hooksgen.IngestPayload) (*hooksgen.IngestHookResult, error)
}

type consultToolCallRequest struct {
	ThreadID   string          `json:"thread_id"`
	ToolName   string          `json:"tool_name"`
	ToolInput  json.RawMessage `json:"tool_input"`
	ToolCallID string          `json:"tool_call_id"`
}

type consultToolCallResult struct {
	Decision string `json:"decision"`
	Message  string `json:"message,omitempty"`
}

func consultAllowResult() consultToolCallResult {
	return consultToolCallResult{Decision: consultDecisionAllow, Message: ""}
}

func consultDenyResult(message string) consultToolCallResult {
	if strings.TrimSpace(message) == "" {
		message = consultDefaultDenyMessage
	}
	return consultToolCallResult{Decision: consultDecisionDeny, Message: message}
}

// ConsultToolCall evaluates a pending assistant tool invocation against
// canonical hook enforcement (spend gate, risk policy). The session identity
// is the server-owned chat id, never a client-supplied conversation id. A
// nil ingester, or an ingest error, fails open so a control-plane blip does
// not wedge the assistant loop.
func (s *ServiceCore) ConsultToolCall(ctx context.Context, projectID, principalAssistantID uuid.UUID, req consultToolCallRequest) (consultToolCallResult, error) {
	threadID, err := uuid.Parse(strings.TrimSpace(req.ThreadID))
	if err != nil {
		return consultToolCallResult{}, oops.E(oops.CodeBadRequest, err, "invalid thread_id")
	}
	toolName := strings.TrimSpace(req.ToolName)
	if toolName == "" {
		return consultToolCallResult{}, oops.E(oops.CodeBadRequest, nil, "tool_name is required")
	}
	toolCallID := strings.TrimSpace(req.ToolCallID)
	if toolCallID == "" {
		return consultToolCallResult{}, oops.E(oops.CodeBadRequest, nil, "tool_call_id is required")
	}

	logAttrs := []slog.Attr{
		attr.SlogProjectID(projectID.String()),
		attr.SlogAssistantID(principalAssistantID.String()),
		attr.SlogAssistantThreadID(threadID.String()),
		attr.SlogToolName(toolName),
	}

	row, err := assistantrepo.New(s.db).LoadAssistantThreadForBootstrap(ctx, assistantrepo.LoadAssistantThreadForBootstrapParams{
		ThreadID:  threadID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return consultToolCallResult{}, oops.E(oops.CodeNotFound, nil, "assistant thread not found").LogError(ctx, s.logger, logAttrs...)
		}
		return consultToolCallResult{}, oops.E(oops.CodeUnexpected, err, "load assistant thread").LogError(ctx, s.logger, logAttrs...)
	}
	if row.AssistantID != principalAssistantID {
		return consultToolCallResult{}, oops.E(oops.CodeForbidden, nil, "thread does not belong to assistant").LogError(ctx, s.logger, logAttrs...)
	}

	if s.hookIngester == nil {
		s.logger.WarnContext(ctx, "assistant tool-call consult has no hook ingester; allowing",
			attr.SlogEvent("assistant_tool_consult_ingester_missing"),
			attr.SlogAssistantID(principalAssistantID.String()),
			attr.SlogAssistantThreadID(threadID.String()),
			attr.SlogToolName(toolName),
		)
		return consultAllowResult(), nil
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return consultToolCallResult{}, oops.E(oops.CodeUnauthorized, nil, "missing auth context")
	}

	sessionID := row.ChatID.String()
	idempotencyKey := fmt.Sprintf("assistant:%s:%s", threadID.String(), toolCallID)
	toolInput := decodeConsultToolInput(req.ToolInput)

	payload := &hooksgen.IngestPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Replayed:         nil,
		SchemaVersion:    assistantHookSchemaV1,
		IdempotencyKey:   &idempotencyKey,
		Source: &hooksgen.HookIngestSource{
			Adapter:        assistantHookAdapter,
			AdapterVersion: nil,
			RawEventName:   nil,
			Hostname:       nil,
			UserEmail:      nil,
		},
		Session: &hooksgen.HookIngestSession{
			ID:     &sessionID,
			TurnID: nil,
			Cwd:    nil,
			Model:  conv.PtrEmpty(row.Model),
		},
		Event: &hooksgen.HookIngestEvent{
			Type:       "tool.requested",
			OccurredAt: nil,
		},
		Data: &hooksgen.HookIngestData{
			Prompt: nil,
			ToolCall: &hooksgen.HookToolCallData{
				ID:             &toolCallID,
				Name:           &toolName,
				Input:          toolInput,
				Output:         nil,
				Error:          nil,
				IsInterrupt:    nil,
				PermissionType: nil,
				DurationMs:     nil,
				Status:         nil,
			},
			Mcp:                   nil,
			McpInventory:          nil,
			McpInventoryCollected: nil,
			Usage:                 nil,
			Message:               nil,
			Skill:                 nil,
			Notification:          nil,
			McpAttribution:        nil,
			PromptAttachments:     nil,
		},
		Raw: nil,
	}

	outcome, err := s.hookIngester.IngestAssistantToolCall(ctx, authCtx, payload)
	if err != nil {
		s.logger.WarnContext(ctx, "assistant tool-call consult ingest failed; allowing",
			attr.SlogEvent("assistant_tool_consult_ingest_error"),
			attr.SlogError(err),
			attr.SlogAssistantID(principalAssistantID.String()),
			attr.SlogAssistantThreadID(threadID.String()),
			attr.SlogToolName(toolName),
		)
		return consultAllowResult(), nil
	}
	if outcome != nil && strings.EqualFold(strings.TrimSpace(outcome.Decision), consultDecisionDeny) {
		return consultDenyResult(strings.TrimSpace(conv.PtrValOr(outcome.Message, ""))), nil
	}
	return consultAllowResult(), nil
}

func decodeConsultToolInput(raw json.RawMessage) any {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string]any{}
	}
	var input any
	if err := json.Unmarshal(raw, &input); err != nil {
		return map[string]any{}
	}
	if input == nil {
		return map[string]any{}
	}
	return input
}
