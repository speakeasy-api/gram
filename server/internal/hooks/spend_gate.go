package hooks

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
)

// checkSpendGate consults the spend rule circuit for the actor on a hook
// event. It runs BEFORE risk-policy scans — an over-budget actor is denied
// before any policy evaluation. Every failure mode resolves to "not blocked"
// (fail-open): a nil gate, an unresolved org/user identity, and cache
// infrastructure errors.
func (s *Service) checkSpendGate(ctx context.Context, ev hookevents.Event) *spendrules.Block {
	if s.spendGate == nil {
		return nil
	}
	if ev.Context.OrganizationID == "" || ev.Context.User.ID == "" {
		return nil
	}

	block, err := s.spendGate.CheckBlocked(ctx, ev.Context.OrganizationID, ev.Context.User.ID)
	if err != nil {
		s.logger.WarnContext(ctx, "spend gate check failed; failing open",
			attr.SlogError(err),
			attr.SlogEvent("spend_gate_error"),
			attr.SlogHookSource(string(ev.Provider)),
			attr.SlogHookEvent(ev.RawEventType),
			attr.SlogOrganizationID(ev.Context.OrganizationID),
		)
		return nil
	}
	return block
}

// spendGatedAdapter reports whether a self-reported ingest source adapter
// has spend enforcement on the unified ingest path: the adapters with a
// per-provider enforcement surface. Matching is case-insensitive so a case
// variant cannot dodge the gate.
func spendGatedAdapter(adapter string) bool {
	switch strings.ToLower(strings.TrimSpace(adapter)) {
	case "claude", "codex", "cursor", "openclaw":
		return true
	default:
		return false
	}
}

// spendBlockReason renders the reason shown to the agent and stored on
// traces when the spend gate denies an event. kind is "prompt",
// "tool call", or "permission request".
func spendBlockReason(kind string, block *spendrules.Block) string {
	return fmt.Sprintf("Speakeasy blocked this %s: spend rule %q — budget resets %s",
		kind, block.RuleName, block.WindowEnd.UTC().Format("Jan 2, 2006 15:04 MST"))
}

// cursorSpendDenyReason renders the audit and user-facing reasons for a
// Cursor tool-call spend deny, minting its durable block page off the hot
// path so the user reason carries a block view link — mirroring the Claude
// and Codex tool-call spend blocks. The MCP: routing prefix is stripped so
// spend-block rows share tool identity with risk-block rows, and idempotent
// redeliveries keep the deny without minting another block row (matching the
// Claude and ingest spend paths).
func (s *Service) cursorSpendDenyReason(ctx context.Context, block *spendrules.Block, toolName string, orgID string, projectID uuid.UUID, userID string, conversationID string) (string, string) {
	auditReason := spendBlockReason("tool call", block)
	userReason := auditReason
	if s.isHookDuplicate(ctx) {
		return auditReason, userReason
	}
	if bURL := s.recordToolCallBlockAsync(ctx, toolCallBlockParams{
		Provider:       "cursor",
		OrganizationID: orgID,
		ProjectID:      projectID,
		Reason:         auditReason,
		ToolName:       strings.TrimPrefix(toolName, "MCP:"),
		UserID:         userID,
		RiskPolicyID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RiskResultID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ChatID:         chatIDForBlock(conversationID),
		ChatMessageID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}); bURL != "" {
		userReason = appendBlockURL(userReason, bURL)
	}
	return auditReason, userReason
}
