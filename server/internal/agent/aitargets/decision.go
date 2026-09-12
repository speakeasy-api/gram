package aitargets

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/speakeasy-api/gram/server/internal/agent/repo"
)

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

// DecisionRecord is one organization's recorded decision for one target, with
// who recorded it. DecidedBy and DecidedAt are zero on a defaulted row.
type DecisionRecord struct {
	TargetID  string
	Decision  Decision
	Rationale string
	DecidedBy string
	DecidedAt time.Time
}

// UnreviewedDecisionRecord is what a target with no row resolves to.
func UnreviewedDecisionRecord(targetID string) DecisionRecord {
	return DecisionRecord{
		TargetID:  targetID,
		Decision:  DecisionUnreviewed,
		Rationale: "",
		DecidedBy: "",
		DecidedAt: time.Time{},
	}
}

// DecisionRecordFromRow converts a stored row.
func DecisionRecordFromRow(row repo.AiToolDecision) DecisionRecord {
	return DecisionRecord{
		TargetID:  row.TargetID,
		Decision:  Decision(row.Decision),
		Rationale: row.Rationale.String,
		DecidedBy: row.DecidedBy.String,
		DecidedAt: row.DecidedAt.Time,
	}
}

// LoadDecisions reads an organization's recorded decisions, keyed by target
// id. Targets with no row are absent from the map rather than filled in, so
// callers can tell "never decided" from "decided as unreviewed"; Decisions
// resolves either to the same Decision value.
func LoadDecisions(ctx context.Context, queries *repo.Queries, organizationID string) (map[string]DecisionRecord, error) {
	rows, err := queries.ListAIToolDecisions(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list ai target decisions: %w", err)
	}
	decisions := make(map[string]DecisionRecord, len(rows))
	for _, row := range rows {
		decisions[row.TargetID] = DecisionRecordFromRow(row)
	}
	return decisions, nil
}

// Decisions resolves one target's decision, defaulting to unreviewed.
func Decisions(decisions map[string]DecisionRecord, targetID string) DecisionRecord {
	if decision, ok := decisions[targetID]; ok {
		return decision
	}
	return UnreviewedDecisionRecord(targetID)
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
// organization serves the target and it carries a verifiable matcher, which
// in practice means it publishes a client ID metadata document.
func Enforceable(target Target) bool {
	return target.Enabled &&
		len(target.GatewayClient.CIMDVendorKeys)+len(target.GatewayClient.OAuthClientIDs) > 0
}
