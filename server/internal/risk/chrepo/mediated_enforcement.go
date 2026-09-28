package chrepo

import (
	"context"
	"fmt"
	"time"
)

// MediatedEnforcementParams scopes a mediated-execution read to one
// organization and a half-open [From, To) window. It is organization-wide
// rather than project-scoped on purpose: the coverage matrix it serves reports
// on an organization, and organization_id leads the table's sort key.
type MediatedEnforcementParams struct {
	OrganizationID string
	From           time.Time
	To             time.Time
}

// MediatedEnforcementCounts summarises what risk policies did to MCP
// executions Gram mediated — hosted MCP, platform MCP, instances and remote
// MCP. Counts are per execution, not per finding: one denied tool call that
// matched three rules stopped one call.
type MediatedEnforcementCounts struct {
	// Scanned is every mediated execution a policy produced a finding on,
	// whatever the policy then did.
	Scanned uint64
	// Enforced is the subset whose execution did not proceed as requested.
	Enforced uint64
	// LastEnforcedAt is when the most recent execution was stopped. Zero when
	// none was.
	LastEnforcedAt time.Time
	// LastScannedAt is when the most recent mediated finding was produced.
	// Zero when there were none.
	LastScannedAt time.Time
}

// enforcedOutcomes are the enforcement_outcome values meaning the execution
// did not proceed as requested. Warnings are deliberately excluded: they leave
// the caller in control, so they evidence a policy running, not one enforcing.
const enforcedOutcomes = "('denied', 'withheld', 'quarantined')"

// GetMediatedEnforcementCounts reports policy enforcement on Gram-mediated MCP
// executions.
//
// mediation_surface is non-empty only on findings produced at an MCP mediation
// seam, which is what separates gateway enforcement from the chat-message
// scanning the rest of the risk pipeline does.
//
// Executions are deduplicated by execution_id, falling back to the finding id
// for rows written before that column existed. Redelivered copies of one
// finding therefore collapse, as do several findings on one call.
//
// Suppression is not filtered out: dismissing a finding after the fact does
// not un-stop the call that was stopped, and this read is evidence that
// enforcement ran, not a list of live findings.
func (q *Queries) GetMediatedEnforcementCounts(ctx context.Context, p MediatedEnforcementParams) (MediatedEnforcementCounts, error) {
	counts := MediatedEnforcementCounts{Scanned: 0, Enforced: 0, LastEnforcedAt: time.Time{}, LastScannedAt: time.Time{}}

	const executionKey = "if(execution_id != '', execution_id, toString(id))"
	const enforced = "enforcement_outcome IN " + enforcedOutcomes

	query, args, err := sq.Select(
		"uniqExact("+executionKey+") AS scanned",
		"uniqExactIf("+executionKey+", "+enforced+") AS enforced",
		"maxIf(created_at, "+enforced+") AS last_enforced_at",
		"max(created_at) AS last_scanned_at",
	).
		From("risk_findings").
		Where("organization_id = ?", p.OrganizationID).
		Where(notShadowCond).
		Where("mediation_surface != ''").
		Where("created_at >= ?", p.From).
		Where("created_at < ?", p.To).
		ToSql()
	if err != nil {
		return counts, fmt.Errorf("build mediated enforcement counts query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return counts, fmt.Errorf("query mediated enforcement counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&counts.Scanned, &counts.Enforced, &counts.LastEnforcedAt, &counts.LastScannedAt); err != nil {
			return counts, fmt.Errorf("scan mediated enforcement counts: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return counts, fmt.Errorf("read mediated enforcement counts: %w", err)
	}

	// maxIf over no matching row yields the DateTime64 zero value, which is
	// the unix epoch rather than Go's zero time. Normalize so callers can test
	// IsZero.
	if counts.Enforced == 0 {
		counts.LastEnforcedAt = time.Time{}
	}
	if counts.Scanned == 0 {
		counts.LastScannedAt = time.Time{}
	}

	return counts, nil
}
