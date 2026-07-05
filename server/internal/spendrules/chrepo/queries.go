package chrepo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// sq is the squirrel statement builder pre-configured for ClickHouse.
var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// ListActorSpend returns per-actor total_cost over [timeStart, timeEnd] for
// the given projects, grouped by user_email. Rows without an email are
// excluded because they cannot be attributed to an actor. The source is bucketed
// hourly; timeStart is floored to the hour by the query, so callers that need
// an exclusive lower bound should ceil timeStart to the next hour boundary
// before calling.
func (q *Queries) ListActorSpend(ctx context.Context, projectIDs []string, timeStart, timeEnd int64) ([]ActorSpendRow, error) {
	if len(projectIDs) == 0 {
		return nil, nil
	}

	sb := sq.Select("user_email").
		Column(squirrel.Expr("sumIfMerge(total_cost) AS m_total_cost")).
		From("attribute_metrics_summaries").
		Where(squirrel.Eq{"gram_project_id": projectIDs}).
		Where("time_bucket >= toStartOfHour(fromUnixTimestamp64Nano(?))", timeStart).
		Where("time_bucket <= toStartOfHour(fromUnixTimestamp64Nano(?))", timeEnd).
		Where("user_email != ''").
		GroupBy("user_email")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building actor spend query: %w", err)
	}

	return q.listActorSpend(ctx, query, args, "actor spend")
}

// ListActorSpendForRules returns per-actor total_cost over [timeStart, timeEnd]
// from the dedicated spend-rule rollup. The rollup is bucketed by minute and is
// intentionally narrower than attribute_metrics_summaries so enforcement stays
// decoupled from analytics dimensions.
func (q *Queries) ListActorSpendForRules(ctx context.Context, projectIDs []string, timeStart, timeEnd int64) ([]ActorSpendRow, error) {
	if len(projectIDs) == 0 {
		return nil, nil
	}

	sb := sq.Select("user_email").
		Column(squirrel.Expr("sum(total_cost) AS m_total_cost")).
		From("spend_rule_usage_summaries").
		Where(squirrel.Eq{"gram_project_id": projectIDs}).
		Where("time_bucket >= toStartOfMinute(fromUnixTimestamp64Nano(?))", timeStart).
		Where("time_bucket <= toStartOfMinute(fromUnixTimestamp64Nano(?))", timeEnd).
		Where("user_email != ''").
		GroupBy("user_email")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building actor spend for rules query: %w", err)
	}

	return q.listActorSpend(ctx, query, args, "actor spend for rules")
}

func (q *Queries) listActorSpend(ctx context.Context, query string, args []any, label string) ([]ActorSpendRow, error) {
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", label, err)
	}
	defer o11y.NoLogDefer(rows.Close)

	var out []ActorSpendRow
	for rows.Next() {
		var row ActorSpendRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning %s row: %w", label, err)
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s rows: %w", label, err)
	}
	return out, nil
}
