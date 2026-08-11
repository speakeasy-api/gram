package chrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// sq is the squirrel statement builder pre-configured for ClickHouse.
var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// LoadActorWindowSpend returns actor spend keyed by normalized email for all
// fixed UTC windows used by spend-rule enforcement.
func LoadActorWindowSpend(ctx context.Context, queries *Queries, projectIDs []string, now time.Time) (map[string]ActorWindowSpendRow, error) {
	spendByEmail := map[string]ActorWindowSpendRow{}
	if len(projectIDs) == 0 {
		return spendByEmail, nil
	}

	dailyStart, weeklyStart, monthlyStart := fixedWindowStarts(now)
	timeStart := earliestTime(dailyStart, weeklyStart, monthlyStart)
	rows, err := queries.ListActorWindowSpendForRules(
		ctx,
		projectIDs,
		dailyStart.UnixNano(),
		weeklyStart.UnixNano(),
		monthlyStart.UnixNano(),
		timeStart.UnixNano(),
		now.UnixNano(),
	)
	if err != nil {
		return nil, fmt.Errorf("list actor window spend: %w", err)
	}

	for _, row := range rows {
		email := conv.NormalizeEmail(row.Email)
		if email == "" {
			continue
		}
		// ClickHouse groups by raw user_email, so the same actor can arrive as
		// several rows differing only in email casing. Accumulate after
		// normalization rather than overwriting, or spend would be undercounted
		// and a breached blocking rule could stay unblocked.
		existing := spendByEmail[email]
		existing.Email = email
		existing.DailyCost += row.DailyCost
		existing.WeeklyCost += row.WeeklyCost
		existing.MonthlyCost += row.MonthlyCost
		spendByEmail[email] = existing
	}

	return spendByEmail, nil
}

// LoadActorWindowSpendByEmail returns one actor's spend across the fixed UTC
// windows used by spend-rule enforcement. The Redis actor key is user-id keyed,
// but the current ClickHouse rollup is email-keyed, so callers resolve the
// actor first and query by normalized email here.
func LoadActorWindowSpendByEmail(ctx context.Context, queries *Queries, projectIDs []string, email string, now time.Time) (ActorWindowSpendRow, error) {
	if len(projectIDs) == 0 || conv.NormalizeEmail(email) == "" {
		return ActorWindowSpendRow{Email: conv.NormalizeEmail(email), DailyCost: 0, WeeklyCost: 0, MonthlyCost: 0}, nil
	}

	dailyStart, weeklyStart, monthlyStart := fixedWindowStarts(now)
	timeStart := earliestTime(dailyStart, weeklyStart, monthlyStart)
	rows, err := queries.ListActorWindowSpendForRulesByEmail(
		ctx,
		projectIDs,
		conv.NormalizeEmail(email),
		dailyStart.UnixNano(),
		weeklyStart.UnixNano(),
		monthlyStart.UnixNano(),
		timeStart.UnixNano(),
		now.UnixNano(),
	)
	if err != nil {
		return ActorWindowSpendRow{}, fmt.Errorf("list actor window spend by email: %w", err)
	}

	spend := ActorWindowSpendRow{Email: conv.NormalizeEmail(email), DailyCost: 0, WeeklyCost: 0, MonthlyCost: 0}
	for _, row := range rows {
		spend.DailyCost += row.DailyCost
		spend.WeeklyCost += row.WeeklyCost
		spend.MonthlyCost += row.MonthlyCost
	}
	return spend, nil
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

// ListActorWindowSpendForRules returns per-actor spend for the daily, weekly,
// and monthly fixed windows in a single pass over the spend-rule rollup.
func (q *Queries) ListActorWindowSpendForRules(
	ctx context.Context,
	projectIDs []string,
	dailyStart, weeklyStart, monthlyStart, timeStart, timeEnd int64,
) ([]ActorWindowSpendRow, error) {
	if len(projectIDs) == 0 {
		return nil, nil
	}

	sb := sq.Select("user_email").
		Column(squirrel.Expr("sumIf(total_cost, time_bucket >= toStartOfMinute(fromUnixTimestamp64Nano(?))) AS m_daily_total_cost", dailyStart)).
		Column(squirrel.Expr("sumIf(total_cost, time_bucket >= toStartOfMinute(fromUnixTimestamp64Nano(?))) AS m_weekly_total_cost", weeklyStart)).
		Column(squirrel.Expr("sumIf(total_cost, time_bucket >= toStartOfMinute(fromUnixTimestamp64Nano(?))) AS m_monthly_total_cost", monthlyStart)).
		From("spend_rule_usage_summaries").
		Where(squirrel.Eq{"gram_project_id": projectIDs}).
		Where("time_bucket >= toStartOfMinute(fromUnixTimestamp64Nano(?))", timeStart).
		Where("time_bucket <= toStartOfMinute(fromUnixTimestamp64Nano(?))", timeEnd).
		Where("user_email != ''").
		GroupBy("user_email")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building actor window spend for rules query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query actor window spend for rules: %w", err)
	}
	defer o11y.NoLogDefer(rows.Close)

	var out []ActorWindowSpendRow
	for rows.Next() {
		var row ActorWindowSpendRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning actor window spend for rules row: %w", err)
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate actor window spend for rules rows: %w", err)
	}
	return out, nil
}

// ListActorWindowSpendForRulesByEmail returns fixed-window spend rows matching
// a normalized email. Grouping still happens by raw user_email because the
// ClickHouse rollup stores raw event values; callers collapse casing variants.
func (q *Queries) ListActorWindowSpendForRulesByEmail(
	ctx context.Context,
	projectIDs []string,
	email string,
	dailyStart, weeklyStart, monthlyStart, timeStart, timeEnd int64,
) ([]ActorWindowSpendRow, error) {
	if len(projectIDs) == 0 || email == "" {
		return nil, nil
	}

	sb := sq.Select("user_email").
		Column(squirrel.Expr("sumIf(total_cost, time_bucket >= toStartOfMinute(fromUnixTimestamp64Nano(?))) AS m_daily_total_cost", dailyStart)).
		Column(squirrel.Expr("sumIf(total_cost, time_bucket >= toStartOfMinute(fromUnixTimestamp64Nano(?))) AS m_weekly_total_cost", weeklyStart)).
		Column(squirrel.Expr("sumIf(total_cost, time_bucket >= toStartOfMinute(fromUnixTimestamp64Nano(?))) AS m_monthly_total_cost", monthlyStart)).
		From("spend_rule_usage_summaries").
		Where(squirrel.Eq{"gram_project_id": projectIDs}).
		Where("time_bucket >= toStartOfMinute(fromUnixTimestamp64Nano(?))", timeStart).
		Where("time_bucket <= toStartOfMinute(fromUnixTimestamp64Nano(?))", timeEnd).
		Where("lowerUTF8(user_email) = ?", email).
		GroupBy("user_email")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building actor window spend for rules by email query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query actor window spend for rules by email: %w", err)
	}
	defer o11y.NoLogDefer(rows.Close)

	var out []ActorWindowSpendRow
	for rows.Next() {
		var row ActorWindowSpendRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning actor window spend for rules by email row: %w", err)
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate actor window spend for rules by email rows: %w", err)
	}
	return out, nil
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

func fixedWindowStarts(now time.Time) (dailyStart, weeklyStart, monthlyStart time.Time) {
	now = now.UTC()
	dailyStart = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	offset := (int(now.Weekday()) + 6) % 7
	weeklyStart = dailyStart.AddDate(0, 0, -offset)
	monthlyStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return dailyStart, weeklyStart, monthlyStart
}

func earliestTime(times ...time.Time) time.Time {
	if len(times) == 0 {
		return time.Time{}
	}
	earliest := times[0]
	for _, t := range times[1:] {
		if t.Before(earliest) {
			earliest = t
		}
	}
	return earliest
}
