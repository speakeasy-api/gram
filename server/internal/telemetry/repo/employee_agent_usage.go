package repo

import (
	"context"
	"fmt"
)

// SearchEmployeeAgentUsageParams parameterizes the MV-backed employee usage read.
type SearchEmployeeAgentUsageParams struct {
	GramProjectID string
	TimeStart     int64 // inclusive window start, unix nanoseconds
	TimeEnd       int64 // inclusive window end, unix nanoseconds
	Limit         int
}

// SearchEmployeeAgentUsage returns per-user usage summaries for the employee
// enrollment list from the pre-aggregated attribute_metrics_summaries view
// instead of scanning raw telemetry_logs. It is far cheaper (the view is a
// per-(user, hour, ...) rollup) but carries three consequences the caller must
// accept:
//
//   - Scope is canonical observed agent usage only — Claude Code, Codex, Cursor,
//     and Claude Chat — matching the costs/billing pages. Gram-hosted chat
//     completions and duplicate usage-metric rows are excluded.
//   - Users are keyed by email. Identities that never carry an email in the
//     window are absent here; surface them via ListEmaillessIdentities.
//   - Activity timestamps are hour-truncated (the view buckets by hour).
//
// Only identity, first/last activity, and input/output/total token sums are
// populated; the remaining UserSummary fields (chats, cost, cache, tool and
// hook breakdowns) are left zero/empty.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) SearchEmployeeAgentUsage(ctx context.Context, arg SearchEmployeeAgentUsageParams) ([]UserSummary, error) {
	sb := sq.Select(
		// Identity: email is the group key for this path.
		"user_email AS user_id",
		"user_email AS user_email",

		// Activity window. time_bucket is a whole-hour DateTime, so these are
		// hour-truncated. toInt64(DateTime) yields unix seconds; scale to nanos to
		// match the UserSummary contract (mirrors QueryAttributeMetricsTimeseries).
		"toInt64(min(time_bucket)) * 1000000000 AS first_seen_unix_nano",
		"toInt64(max(time_bucket)) * 1000000000 AS last_seen_unix_nano",

		// Token sums. The view stores AggregateFunction(sumIf, ...) states, so
		// reads use the matching sumIfMerge combinator (see attributeMeasureSelects).
		"sumIfMerge(total_input_tokens) AS total_input_tokens",
		"sumIfMerge(total_output_tokens) AS total_output_tokens",
		"sumIfMerge(total_tokens) AS total_tokens",
	).
		From("attribute_metrics_summaries").
		// Exclude tombstoned backfill rows (see the is_active column comment in
		// server/clickhouse/schema.sql).
		Where("is_active = 1").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_bucket >= toStartOfHour(fromUnixTimestamp64Nano(?))", arg.TimeStart).
		Where("time_bucket <= toStartOfHour(fromUnixTimestamp64Nano(?))", arg.TimeEnd).
		// Email-keyed rows only; the empty-email bucket is handled by
		// ListEmaillessIdentities so it isn't shown as one synthetic user.
		Where("user_email != ''").
		GroupBy("user_email").
		OrderBy("last_seen_unix_nano DESC", "user_email DESC").
		Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building employee agent usage query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserSummary
	for rows.Next() {
		var u UserSummary
		if err = rows.ScanStruct(&u); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		users = append(users, u)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

// ListEmaillessIdentitiesParams parameterizes the email-less identity supplement.
type ListEmaillessIdentitiesParams struct {
	GramProjectID string
	TimeStart     int64 // inclusive window start, unix nanoseconds
	TimeEnd       int64 // inclusive window end, unix nanoseconds
	Limit         int
}

// ListEmaillessIdentities returns telemetry identities that carry a user_id but
// never an email anywhere in the window. These are typically tool-call/hook rows
// that carry no token usage, so the email-keyed attribute_metrics_summaries view
// cannot represent them. The employee enrollment list surfaces them alongside
// SearchEmployeeAgentUsage so unattributed activity stays visible — with last
// activity but no token counts (they genuinely have none).
//
// The scan reads only materialized columns (user_id, user_email, time_unix_nano)
// with no JSON extraction, join, or map aggregation, so it is much cheaper than
// the full SearchUsers query. Only identity, activity window, and raw_user_ids
// are populated.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListEmaillessIdentities(ctx context.Context, arg ListEmaillessIdentitiesParams) ([]UserSummary, error) {
	sb := sq.Select(
		"user_id AS user_id",
		"'' AS user_email",
		"min(time_unix_nano) AS first_seen_unix_nano",
		"max(time_unix_nano) AS last_seen_unix_nano",
		// Grouped by user_id, so this yields the single id — carried through for
		// the account-enrichment join, exactly like SearchUsers' raw_user_ids.
		"groupUniqArray(user_id) AS raw_user_ids",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where("user_id != ''").
		GroupBy("user_id").
		// Keep only user_ids that never co-occur with an email in the window;
		// those that do are folded into an email key by SearchEmployeeAgentUsage.
		Having("max(user_email != '') = 0").
		OrderBy("last_seen_unix_nano DESC", "user_id DESC").
		Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building emailless identities query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserSummary
	for rows.Next() {
		var u UserSummary
		if err = rows.ScanStruct(&u); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		users = append(users, u)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}
