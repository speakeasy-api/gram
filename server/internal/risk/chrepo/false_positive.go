package chrepo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func falsePositiveSuppressionProjection(excludedReason string) string {
	return copyProjection("?", "NULL", "?", "'"+excludedReason+"'", "?", "'"+EventKindSuppression+"'")
}

func falsePositiveReversalProjection() string {
	return copyProjection("NULL", "NULL", "NULL", "''", "''", "'"+EventKindUnsuppression+"'")
}

func latestRiskFindingsByIDsSubquery(ids []uuid.UUID) (string, []any) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return `(SELECT *, ROW_NUMBER() OVER (PARTITION BY id ORDER BY ` + latestCopyOrderSQL + `) AS rn
	FROM risk_findings
	WHERE organization_id = ? AND project_id = ? AND ` + notShadowCond + `
	  AND id IN (` + placeholders + `)) AS latest`, args
}

// FalsePositiveState is one tenant-scoped finding's current state.
type FalsePositiveState struct {
	ID             uuid.UUID
	Dismissed      bool
	RuleSuppressed bool
}

// ListFalsePositiveStates resolves each requested id to its winning copy.
func (q *Queries) ListFalsePositiveStates(ctx context.Context, organizationID, projectID string, ids []uuid.UUID) ([]FalsePositiveState, error) {
	if len(ids) == 0 {
		return []FalsePositiveState{}, nil
	}

	latest, idArgs := latestRiskFindingsByIDsSubquery(ids)
	query := "SELECT id, " + dismissedStateCond + ", " + suppressedReasonExpr + " = '" + ExcludedReasonRule + "'" +
		" FROM " + latest + " WHERE rn = 1 AND dead_letter_reason = ''"
	args := make([]any, 0, 2+len(idArgs))
	args = append(args, organizationID, projectID)
	args = append(args, idArgs...)

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query false-positive states: %w", err)
	}
	defer func() { _ = rows.Close() }()

	states := make([]FalsePositiveState, 0, len(ids))
	for rows.Next() {
		var state FalsePositiveState
		if err := rows.Scan(&state.ID, &state.Dismissed, &state.RuleSuppressed); err != nil {
			return nil, fmt.Errorf("scan false-positive state: %w", err)
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read false-positive states: %w", err)
	}
	return states, nil
}

// AppendFalsePositiveSuppression appends manual suppression copies for live findings.
func (q *Queries) AppendFalsePositiveSuppression(ctx context.Context, organizationID, projectID, suppressedAt, insertedAt, reason string, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	latest, idArgs := latestRiskFindingsByIDsSubquery(ids)
	query := "INSERT INTO risk_findings (" + strings.Join(riskFindingColumns, ", ") + ") " +
		"SELECT " + falsePositiveSuppressionProjection(ExcludedReasonManual) + " FROM " + latest +
		" WHERE rn = 1 AND dead_letter_reason = '' AND excluded_at IS NULL AND false_positive_at IS NULL"
	args := make([]any, 0, 7+len(idArgs))
	args = append(args, insertedAt, suppressedAt, suppressedAt, reason, organizationID, projectID)
	args = append(args, idArgs...)

	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("append false-positive suppression copies: %w", err)
	}
	return nil
}

// AppendAutomatedFalsePositiveSuppression appends sweep-owned suppression copies.
func (q *Queries) AppendAutomatedFalsePositiveSuppression(ctx context.Context, organizationID, projectID, suppressedAt, insertedAt, reason string, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	latest, idArgs := latestRiskFindingsByIDsSubquery(ids)
	query := "INSERT INTO risk_findings (" + strings.Join(riskFindingColumns, ", ") + ") " +
		"SELECT " + falsePositiveSuppressionProjection(ExcludedReasonAutomated) + " FROM " + latest +
		" WHERE rn = 1 AND dead_letter_reason = '' AND excluded_at IS NULL AND false_positive_at IS NULL"
	args := make([]any, 0, 7+len(idArgs))
	args = append(args, insertedAt, suppressedAt, suppressedAt, reason, organizationID, projectID)
	args = append(args, idArgs...)

	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("append automated false-positive suppression copies: %w", err)
	}
	return nil
}

// AppendFalsePositiveReversal appends reversal copies except for rule suppressions.
func (q *Queries) AppendFalsePositiveReversal(ctx context.Context, organizationID, projectID, insertedAt string, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	latest, idArgs := latestRiskFindingsByIDsSubquery(ids)
	query := "INSERT INTO risk_findings (" + strings.Join(riskFindingColumns, ", ") + ") " +
		"SELECT " + falsePositiveReversalProjection() + " FROM " + latest +
		" WHERE rn = 1 AND dead_letter_reason = '' AND " + dismissedStateCond +
		" AND " + suppressedReasonExpr + " != '" + ExcludedReasonRule + "'"
	args := make([]any, 0, 3+len(idArgs))
	args = append(args, insertedAt, organizationID, projectID)
	args = append(args, idArgs...)

	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("append false-positive reversal copies: %w", err)
	}
	return nil
}

// BackfillSuppressionState is Postgres suppression state missing from an
// existing ClickHouse finding.
type BackfillSuppressionState struct {
	// ExcludedAt is the exclusion time, when an exclusion suppressed the row.
	ExcludedAt *time.Time

	// ExclusionID identifies the exclusion that suppressed the row.
	ExclusionID *uuid.UUID

	// FalsePositiveAt is the manual dismissal time.
	FalsePositiveAt *time.Time

	// MarkedAt overrides the mark time that newer ClickHouse state events must
	// not exceed. When nil, the exclusion or dismissal time is used.
	MarkedAt *time.Time
}

// BackfillSuppressionCopy targets one existing ClickHouse finding.
type BackfillSuppressionCopy struct {
	// ID is the ClickHouse finding id that receives the state copy.
	ID uuid.UUID

	// State is the Postgres suppression state to append.
	State BackfillSuppressionState
}

// AppendBackfillSuppressions appends state-change copies in one insert.
// Explicit reversals and state events newer than the imported mark win.
func (q *Queries) AppendBackfillSuppressions(
	ctx context.Context,
	organizationID string,
	projectID string,
	insertedAt string,
	copies []BackfillSuppressionCopy,
) error {
	if len(copies) == 0 {
		return nil
	}

	ids := make([]uuid.UUID, 0, len(copies))
	valueRows := make([]string, 0, len(copies))
	valueArgs := make([]any, 0, len(copies)*6)
	for _, copy := range copies {
		state := copy.State
		var (
			excludedAt      any
			exclusionID     any
			falsePositiveAt any
			markedAt        any
			reason          string
		)
		switch {
		case state.ExcludedAt != nil && state.ExclusionID != nil:
			excludedAt = FormatCHTime(*state.ExcludedAt)
			exclusionID = *state.ExclusionID
			falsePositiveAt = nullableCHTime(state.FalsePositiveAt)
			markedAt = FormatCHTime(*state.ExcludedAt)
			reason = ExcludedReasonRule
		case state.FalsePositiveAt != nil:
			excludedAt = FormatCHTime(*state.FalsePositiveAt)
			exclusionID = nil
			falsePositiveAt = FormatCHTime(*state.FalsePositiveAt)
			markedAt = FormatCHTime(*state.FalsePositiveAt)
			reason = ExcludedReasonManual
		default:
			return fmt.Errorf("backfill suppression state for %s is empty", copy.ID)
		}
		if state.MarkedAt != nil {
			markedAt = FormatCHTime(*state.MarkedAt)
		}
		ids = append(ids, copy.ID)
		valueRows = append(valueRows, "(?, ?, ?, ?, ?, ?)")
		valueArgs = append(valueArgs, copy.ID, excludedAt, exclusionID, falsePositiveAt, reason, markedAt)
	}

	latest, idArgs := latestRiskFindingsByIDsSubquery(ids)
	projection := copyProjectionFrom(
		"latest.",
		"state.excluded_at",
		"state.exclusion_id",
		"state.false_positive_at",
		"state.excluded_reason",
		"''",
		"'"+EventKindSuppression+"'",
	)
	query := "INSERT INTO risk_findings (" + strings.Join(riskFindingColumns, ", ") + ") " +
		"SELECT " + projection + " FROM " + latest +
		" INNER JOIN values('id UUID, excluded_at Nullable(DateTime64(9)), exclusion_id Nullable(UUID), " +
		"false_positive_at Nullable(DateTime64(9)), excluded_reason String, marked_at DateTime64(9)', " +
		strings.Join(valueRows, ", ") + ") AS state ON latest.id = state.id" +
		" WHERE rn = 1 AND dead_letter_reason = ''" +
		" AND latest.event_kind != '" + EventKindUnsuppression + "'" +
		" AND NOT (" + stateRankSQL + " AND latest.inserted_at > state.marked_at)" +
		" AND (" +
		"(state.excluded_reason = '" + ExcludedReasonRule + "' AND (latest.excluded_at IS NULL OR latest.exclusion_id IS NULL OR " +
		"(state.false_positive_at IS NOT NULL AND latest.false_positive_at IS NULL))) OR " +
		"(state.excluded_reason = '" + ExcludedReasonManual + "' AND latest.excluded_at IS NULL AND latest.false_positive_at IS NULL))"
	args := []any{insertedAt, organizationID, projectID}
	args = append(args, idArgs...)
	args = append(args, valueArgs...)

	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("append backfill suppression copies: %w", err)
	}
	return nil
}

func nullableCHTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return FormatCHTime(*value)
}
