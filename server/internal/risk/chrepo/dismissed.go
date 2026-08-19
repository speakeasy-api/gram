package chrepo

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

// ListDismissedRiskFindingsParams scopes one page of the Dismissed listing to
// one tenant. Cursor* resume strictly after a prior page's last row, keyed on
// the same (suppression time, id) pair the listing sorts by.
type ListDismissedRiskFindingsParams struct {
	OrganizationID string
	ProjectID      string
	// Reasons narrows the listing to rows whose derived suppression reason
	// (suppressedReasonExpr) is in the set. Empty means all reasons.
	Reasons    []string
	CursorTime *time.Time
	CursorID   uuid.NullUUID
	Limit      uint64
}

// DismissedRiskFindingRow is one row of the Dismissed listing: the Risk Events
// listing's row shape — same projection, same store-side redaction, the raw
// match never reaches this store — plus the suppression time the tab sorts and
// paginates on.
type DismissedRiskFindingRow struct {
	RiskFindingListRow

	// SuppressedAt is when the finding was dismissed: excluded_at, falling
	// back to the legacy false_positive_at on rows the suppression backfill
	// did not reach.
	SuppressedAt time.Time

	// SuppressedReason is the row's excluded_reason — manual or automated on
	// this listing (rule rows are gated out), empty on a legacy row carrying
	// only false_positive_at. SuppressedDetail is the free-form context (user
	// dismissal note or sweep catalog reason). ExclusionID rides along for
	// shape parity with the API type; nil on every row this listing serves.
	SuppressedReason string
	SuppressedDetail string
	ExclusionID      *uuid.UUID
}

// dismissedStateCond selects the findings the suppressed listing serves:
// every finding whose latest copy is suppressed, whatever the mechanism — an
// exclusion rule, a manual dismissal, the automated sweep, or a legacy
// pre-convergence row carrying only false_positive_at. Either disjunct
// guarantees suppressedAtExpr resolves non-null, which is what makes it
// usable as a total sort and cursor key.
const dismissedStateCond = "(excluded_at IS NOT NULL OR false_positive_at IS NOT NULL)"

// suppressedReasonExpr derives the effective suppression reason for filtering
// and display. Rows written since the suppression convergence carry
// excluded_reason directly. Legacy rows don't: an ingest- or reconcile-time
// rule exclusion from before the convergence has excluded_at and exclusion_id
// but no reason, and a pre-convergence dismissal has only false_positive_at —
// so exclusion_id decides between rule and manual for them. Keep in lockstep
// with the Go-side mapping in ListDismissedRiskResults.
const suppressedReasonExpr = "multiIf(excluded_reason != '', excluded_reason," +
	" exclusion_id IS NOT NULL, '" + ExcludedReasonRule + "'," +
	" '" + ExcludedReasonManual + "')"

// suppressedAtExpr is the effective suppression time: the converged
// excluded_at, or the legacy false_positive_at for a row that predates the
// backfill.
const suppressedAtExpr = "coalesce(excluded_at, false_positive_at)"

// dismissedStateColumns are the suppression columns dismissedStateCond,
// suppressedAtExpr, and the listing's suppression projection read off each
// id's latest copy. They ride along in the dedup subquery's projection; the
// list query additionally selects the reason/detail/exclusion columns.
var dismissedStateColumns = []string{"dead_letter_reason", "excluded_reason", "excluded_at", "false_positive_at", "excluded_detail", "exclusion_id"}

// dismissedRiskFindingsLatest builds the row source both the list and the count
// read from: every id in the tenant resolved to its most-recently-inserted
// copy, then gated on that latest state. innerColumns is what the dedup
// subquery carries through (the suppression columns are always appended);
// outerColumns is what the caller selects from it.
//
// Dedup must come first for the same reason it does in the Risk Events listing:
// suppression state changes by appending a newer copy of the row, so gating
// before the dedup would match a stale copy — here it would keep listing a
// finding whose dismissal was since undone.
func dismissedRiskFindingsLatest(p ListDismissedRiskFindingsParams, innerColumns []string, outerColumns ...string) squirrel.SelectBuilder {
	latest := sq.Select(append(slices.Clone(innerColumns), dismissedStateColumns...)...).
		From("risk_findings").
		Where("organization_id = ?", p.OrganizationID).
		Where("project_id = ?", p.ProjectID).
		// LIMIT BY takes the first row per key in the current order, so
		// inserted_at DESC makes that the latest copy.
		OrderBy("inserted_at DESC").
		Suffix("LIMIT 1 BY id")

	sb := sq.Select(outerColumns...).
		FromSelect(latest, "latest").
		Where("dead_letter_reason = ''").
		Where(dismissedStateCond)
	if len(p.Reasons) > 0 {
		sb = sb.Where(squirrel.Expr(suppressedReasonExpr+" IN ("+
			strings.TrimSuffix(strings.Repeat("?,", len(p.Reasons)), ",")+")", toAnySlice(p.Reasons)...))
	}
	return sb
}

// toAnySlice widens a string slice for squirrel's variadic Expr args.
func toAnySlice(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// ListDismissedRiskFindings returns one page of dismissed findings, newest
// suppression first, ordered by (suppression time, id) descending.
//
// The cursor is applied AFTER the dedup, unlike the Risk Events listing's
// plain-id branch: an id's copies share their message_created_at but not their
// suppression time — the pre-dismissal copy has none at all — so a cursor
// applied first could drop the copy that carries the state this listing reads.
func (q *Queries) ListDismissedRiskFindings(ctx context.Context, p ListDismissedRiskFindingsParams) ([]DismissedRiskFindingRow, error) {
	outerColumns := append(slices.Clone(riskFindingListColumns),
		suppressedAtExpr+" AS suppressed_at", "excluded_reason", "excluded_detail", "exclusion_id")
	sb := dismissedRiskFindingsLatest(p, riskFindingListColumns, outerColumns...)

	if p.CursorTime != nil && p.CursorID.Valid {
		// The cursor timestamp binds as a formatted string through
		// toDateTime64 because the driver truncates a bound time.Time to whole
		// seconds. Suppression times written by a SQL projection (the
		// retroactive reconcile, the offline sweep) keep their sub-second
		// component, and a truncated cursor would skip every row suppressed
		// later in the cursor's second.
		sb = sb.Where("("+suppressedAtExpr+", id) < (toDateTime64(?, 9), ?)", FormatCHTime(*p.CursorTime), p.CursorID.UUID)
	}

	sb = sb.OrderBy("suppressed_at DESC", "id DESC").Limit(p.Limit)

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build dismissed risk findings list query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query dismissed risk findings list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []DismissedRiskFindingRow
	for rows.Next() {
		var row DismissedRiskFindingRow
		if err := rows.Scan(append(row.scanTargets(), &row.SuppressedAt, &row.SuppressedReason, &row.SuppressedDetail, &row.ExclusionID)...); err != nil {
			return nil, fmt.Errorf("scan dismissed risk findings list row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read dismissed risk findings list: %w", err)
	}

	return out, nil
}

// CountDismissedRiskFindings is the Dismissed listing's total: the same
// predicate as ListDismissedRiskFindings over the whole tenant, with no cursor
// or page bound.
func (q *Queries) CountDismissedRiskFindings(ctx context.Context, p ListDismissedRiskFindingsParams) (uint64, error) {
	query, args, err := dismissedRiskFindingsLatest(p, []string{"id"}, "count() AS dismissed").ToSql()
	if err != nil {
		return 0, fmt.Errorf("build dismissed risk findings count query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("query dismissed risk findings count: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var count uint64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("scan dismissed risk findings count: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read dismissed risk findings count: %w", err)
	}

	return count, nil
}
