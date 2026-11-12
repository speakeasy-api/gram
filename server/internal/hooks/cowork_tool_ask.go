package hooks

// Warn & confirm for tool calls in Claude Cowork.
//
// Cowork cuts off the acknowledgement link, and sending a message again cannot
// confirm a tool call, because the agent, not the person, retries tool calls.
// Claude Code's own permission prompt can: the server answers a warned tool
// call with "ask", the relay turns that into permissionDecision "ask", and
// Cowork shows its Allow / Deny prompt with the warning. The tool only runs if
// the person allows it, so its PostToolUse is the confirmation. The server
// then acknowledges the challenge and writes the same audit entry as the link
// flow.

import (
	"context"
	"errors"
	"fmt"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// coworkToolAskWindow bounds how long a warned tool call can wait for the
// person's answer in Cowork's permission prompt.
const coworkToolAskWindow = 30 * time.Minute

func coworkToolAskCacheKey(projectID uuid.UUID, toolCallID string) string {
	return fmt.Sprintf("hooks:cowork-tool-ask:v1:%s:%s", projectID, toolCallID)
}

// askCoworkToolWarn records the challenge, remembers it under the tool call id
// and marks the response as "ask". ok=false means it could not (no tool call
// id, user or cache), and the caller falls back to the link flow, which denies.
func (s *Service) askCoworkToolWarn(ctx context.Context, payload *gen.IngestPayload, ev hookevents.Event, scanResult *risk.ScanResult, toolName string) (auditReason, userReason string, ok bool) {
	ctx, span := s.tracer.Start(ctx, "hooks.askCoworkToolWarn")
	defer span.End()

	toolCallID := canonicalToolCallID(payload)
	if s.cache == nil || s.riskScanner == nil || toolCallID == "" || ev.Context.User.ID == "" || scanResult.CallFingerprint == "" {
		return "", "", false
	}
	policyID, err := uuid.Parse(scanResult.PolicyID)
	if err != nil {
		return "", "", false
	}

	s.riskScanner.RecordPolicyChallenge(ctx, ev.Context.OrganizationID, ev.Context.ProjectID, ev.Context.User.ID, scanResult.PolicyID, toolName, scanResult.PolicyName, scanResult.Entity, scanResult.RuleID, scanResult.CallFingerprint)
	pending := pendingChallenge{PolicyID: policyID, ToolName: toolName, CallFingerprint: scanResult.CallFingerprint}
	if err := s.cache.Set(ctx, coworkToolAskCacheKey(ev.Context.ProjectID, toolCallID), pending, coworkToolAskWindow); err != nil {
		s.logger.WarnContext(ctx, "store cowork tool ask; denying instead", attr.SlogError(err))
		return "", "", false
	}

	setAskDecision(ctx)
	auditReason = fmt.Sprintf("Speakeasy asked the person to confirm this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
	return auditReason, coworkToolAskReason(scanResult), true
}

// coworkToolAskReason is the text Cowork shows in its permission prompt.
func coworkToolAskReason(scanResult *risk.ScanResult) string {
	return "Speakeasy warning. " + renderWarnBody(scanResult)
}

// confirmCoworkToolAsk acknowledges the challenge behind an asked tool call
// once the call has run, which only happens after the person allowed it.
func (s *Service) confirmCoworkToolAsk(ctx context.Context, payload *gen.IngestPayload, ev hookevents.Event) {
	ctx, span := s.tracer.Start(ctx, "hooks.confirmCoworkToolAsk")
	defer span.End()

	toolCallID := canonicalToolCallID(payload)
	if s.cache == nil || toolCallID == "" {
		return
	}
	var pending pendingChallenge
	if err := s.cache.GetAndDelete(ctx, coworkToolAskCacheKey(ev.Context.ProjectID, toolCallID), &pending); err != nil {
		if !errors.Is(err, redisCache.ErrCacheMiss) {
			s.logger.WarnContext(ctx, "read cowork tool ask", attr.SlogError(err))
		}
		return
	}
	s.acknowledgePendingChallenge(ctx, ev, pending, coworkToolAskWindow, "hooks_cowork_tool_ask_confirmed")
}
