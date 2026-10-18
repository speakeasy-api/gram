package chrepo

import (
	"context"
	"fmt"
	"strings"

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
