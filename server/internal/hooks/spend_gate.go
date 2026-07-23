package hooks

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
)

// checkSpendGate consults the spend rule circuit for the actor on a hook
// event. It runs BEFORE risk-policy scans — an over-budget actor is denied
// before any policy evaluation. Every failure mode resolves to "not blocked"
// (fail-open): a nil gate, an unresolved org/email identity (same guard as
// scanHookEventForEnforcement), and cache infrastructure errors.
func (s *Service) checkSpendGate(ctx context.Context, ev hookevents.Event) *spendrules.Block {
	if s.spendGate == nil {
		return nil
	}
	if ev.Context.OrganizationID == "" || ev.Context.User.Email == "" {
		return nil
	}

	block, err := s.spendGate.CheckBlocked(ctx, ev.Context.OrganizationID, ev.Context.User.Email)
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

// spendBlockReason renders the reason shown to the agent and stored on
// traces when the spend gate denies an event. kind is "prompt" or
// "tool call".
func spendBlockReason(kind string, block *spendrules.Block) string {
	return fmt.Sprintf("Speakeasy blocked this %s: spend rule %q — budget resets %s",
		kind, block.RuleName, block.WindowEnd.UTC().Format("Jan 2, 2006 15:04 MST"))
}
