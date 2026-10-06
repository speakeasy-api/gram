package chrepo

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func falsePositiveSuppressionProjection() string {
	return copyProjection("?", "NULL", "?", "'"+ExcludedReasonManual+"'", "?", "'"+EventKindSuppression+"'")
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

// AppendFalsePositiveSuppression appends a manual-suppression copy of each
// requested finding whose latest state is live. Every other column is copied
// verbatim, including mediated execution metadata.
func (q *Queries) AppendFalsePositiveSuppression(ctx context.Context, organizationID, projectID, suppressedAt, insertedAt, reason string, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	latest, idArgs := latestRiskFindingsByIDsSubquery(ids)
	query := "INSERT INTO risk_findings (" + strings.Join(riskFindingColumns, ", ") + ") " +
		"SELECT " + falsePositiveSuppressionProjection() + " FROM " + latest +
		" WHERE rn = 1 AND dead_letter_reason = '' AND excluded_at IS NULL AND false_positive_at IS NULL"
	args := make([]any, 0, 7+len(idArgs))
	args = append(args, insertedAt, suppressedAt, suppressedAt, reason, organizationID, projectID)
	args = append(args, idArgs...)

	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("append false-positive suppression copies: %w", err)
	}
	return nil
}

// AppendFalsePositiveReversal appends an unsuppressed copy of each requested
// manually or automatically dismissed finding. Rule-owned suppressions remain
// under their exclusion rule.
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
