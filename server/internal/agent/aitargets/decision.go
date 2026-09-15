package aitargets

import "slices"

// Decision is an organization's access decision for one AI tool: whether it
// may reach Gram's MCP gateway. Org-level per tool; a per-server override
// would be a later table on the same pair.
type Decision string

const (
	// DecisionUnreviewed is also what a missing row reads as.
	DecisionUnreviewed Decision = "unreviewed"
	DecisionApproved   Decision = "approved"
	DecisionBlocked    Decision = "blocked"
)

// KnownDecisions lists every value the column accepts.
func KnownDecisions() []Decision {
	return []Decision{DecisionUnreviewed, DecisionApproved, DecisionBlocked}
}

// Valid reports whether d is one of the known decisions.
func (d Decision) Valid() bool {
	return slices.Contains(KnownDecisions(), d)
}

// DecisionRecord is one organization's standing decision for one target.
//
// Who set it and when are deliberately absent: the audit log answers those,
// and keeping them here as well would be a second copy of the same fact that
// nothing keeps in step.
type DecisionRecord struct {
	TargetID  string
	Decision  Decision
	Rationale string
}

// UnreviewedDecisionRecord is what a target the organization has said nothing
// about resolves to.
func UnreviewedDecisionRecord(targetID string) DecisionRecord {
	return DecisionRecord{
		TargetID:  targetID,
		Decision:  DecisionUnreviewed,
		Rationale: "",
	}
}

// AccessState is the verdict a table cell renders. Shares its vocabulary with
// the shadow MCP inventory so one column describes both halves of the section.
type AccessState string

const (
	AccessAllowed    AccessState = "allowed"
	AccessBlocked    AccessState = "blocked"
	AccessUnreviewed AccessState = "unreviewed"
)

// AccessSummary is the enforcement verdict for one AI tool, computed
// server-side so a client renders wording without re-deriving it.
type AccessSummary struct {
	State    AccessState
	Decision Decision

	// Enforceable reports whether a block would actually reach the gateway.
	// Separate from State so a surface can explain why no decision is on offer.
	Enforceable bool

	// Record carries who decided and why, for surfaces allowed to see it.
	Record DecisionRecord
}

// SummarizeAccess builds the verdict for one target.
//
// A tool Gram cannot recognize at the gateway reads unreviewed whatever is
// stored: blocking is CIMD-only, so claiming "blocked" would be untrue in the
// one place an admin checks. The stored decision is left alone. A target the
// catalog no longer serves arrives as the zero Target and lands here too.
func SummarizeAccess(target Target, decision DecisionRecord) AccessSummary {
	enforceable := Enforceable(target)
	if !enforceable {
		return AccessSummary{
			State:       AccessUnreviewed,
			Decision:    DecisionUnreviewed,
			Enforceable: false,
			Record:      UnreviewedDecisionRecord(decision.TargetID),
		}
	}

	state := AccessUnreviewed
	switch decision.Decision {
	case DecisionApproved:
		state = AccessAllowed
	case DecisionBlocked:
		state = AccessBlocked
	case DecisionUnreviewed:
		state = AccessUnreviewed
	}
	return AccessSummary{
		State:       state,
		Decision:    decision.Decision,
		Enforceable: true,
		Record:      decision,
	}
}

// Enforceable reports whether a decision could reach the gateway at all: the
// target carries a verifiable matcher, which in practice means it publishes a
// client ID metadata document.
//
// Nothing else gates it. Inventory membership decides whether a target is
// probed for; matcher presence decides whether a decision about it can be
// enforced. A target with no verifiable matcher stays in the inventory and is
// still probed for, it simply cannot be blocked at the gateway. There is no
// separate switch that can leave a decision recorded but inert.
func Enforceable(target Target) bool {
	return len(target.GatewayClient.CIMDVendorKeys)+len(target.GatewayClient.OAuthClientIDs) > 0
}
