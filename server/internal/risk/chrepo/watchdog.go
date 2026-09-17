package chrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

// WatchdogFinding exposes metadata only, never match text or message content.
type WatchdogFinding struct {
	ID                                          uuid.UUID
	MessageCreatedAt                            time.Time
	PolicyID, RuleID, Category, Team, App, User string
}

// WatchdogGroup counts live findings for one whole-window dimension value.
type WatchdogGroup struct {
	Value string
	Count uint64
}

// Positional time.Time parameters are second-precision in clickhouse-go.
// Bind UTC strings through toDateTime64 to preserve bounds and cursor nanos.
const watchdogTimeLayout = "2006-01-02 15:04:05.000000000"

const watchdogIdentitySQL = "if(external_user_id != '', external_user_id, user_id)"

// Resource exhaustion must fail, never return partial results.
const watchdogSettingsSQL = "SETTINGS max_execution_time = 30, max_rows_to_read = 10000000, max_memory_usage = 536870912, read_overflow_mode = 'throw', timeout_overflow_mode = 'throw', group_by_overflow_mode = 'throw', sort_overflow_mode = 'throw', result_overflow_mode = 'throw'"

// watchdogLive projects safe metadata and resolves latest state before filtering
// mutable flags. Callers select enabled policies upstream, including severity.
// Risk Events filters other than tenancy, policies, and window are not applied.
func watchdogLive(p ListRiskFindingsParams, columns ...string) (squirrel.SelectBuilder, error) {
	if p.From == nil || p.To == nil || !p.From.Before(*p.To) {
		return squirrel.SelectBuilder{}, fmt.Errorf("watchdog requires a nonempty bounded time window")
	}
	projection := append(append([]string{}, columns...), "excluded_at", "false_positive_at")
	inner, err := listRiskFindingsBase(p, projection...)
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	inner = inner.Where("message_created_at >= toDateTime64(?, 9, 'UTC')", p.From.UTC().Format(watchdogTimeLayout)).
		Where("message_created_at < toDateTime64(?, 9, 'UTC')", p.To.UTC().Format(watchdogTimeLayout)).
		Where("created_at >= toDateTime64(?, 9, 'UTC')", p.From.UTC().Format(watchdogTimeLayout)).
		OrderBy(latestCopyOrderSQL).Suffix("LIMIT 1 BY id")
	return sq.Select(columns...).FromSelect(inner, "latest").Where(liveStateCond), nil
}

func watchdogListQuery(p ListRiskFindingsParams) (squirrel.SelectBuilder, error) {
	if p.Limit == 0 {
		return squirrel.SelectBuilder{}, fmt.Errorf("watchdog list requires a positive limit")
	}
	live, err := watchdogLive(p, "id", "message_created_at", "risk_policy_id", "rule_id", "category", "team", "chat_source", "external_user_id", "user_id")
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	outer := sq.Select("id", "message_created_at", "risk_policy_id", "rule_id", "category", "team", "chat_source", watchdogIdentitySQL).FromSelect(live, "live")
	if p.CursorTime != nil && p.CursorID.Valid {
		outer = outer.Where("(message_created_at, id) < (toDateTime64(?, 9, 'UTC'), ?)", p.CursorTime.UTC().Format(watchdogTimeLayout), p.CursorID.UUID)
	}
	return outer.OrderBy("message_created_at DESC", "id DESC").Limit(p.Limit).Suffix(watchdogSettingsSQL), nil
}

// ListWatchdogFindings returns a privacy-safe page in descending event order.
func (q *Queries) ListWatchdogFindings(ctx context.Context, p ListRiskFindingsParams) ([]WatchdogFinding, error) {
	sb, err := watchdogListQuery(p)
	if err != nil {
		return nil, err
	}
	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build watchdog list: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query watchdog list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []WatchdogFinding
	for rows.Next() {
		var row WatchdogFinding
		if err := rows.Scan(&row.ID, &row.MessageCreatedAt, &row.PolicyID, &row.RuleID, &row.Category, &row.Team, &row.App, &row.User); err != nil {
			return nil, fmt.Errorf("scan watchdog list: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read watchdog list: %w", err)
	}
	return out, nil
}

func watchdogGroupQuery(p ListRiskFindingsParams, dimension string) (squirrel.SelectBuilder, error) {
	var expression string
	switch dimension {
	case "severity":
		expression = "risk_policy_id"
	case "data_type":
		expression = "category"
	case "team":
		expression = "team"
	case "app":
		expression = "chat_source"
	case "user":
		expression = watchdogIdentitySQL
	default:
		return squirrel.SelectBuilder{}, fmt.Errorf("unsupported watchdog dimension %q", dimension)
	}
	columns := []string{"id", expression}
	if dimension == "user" {
		columns = []string{"id", "external_user_id", "user_id"}
	}
	live, err := watchdogLive(p, columns...)
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	return sq.Select(expression+" AS value", "count() AS count").FromSelect(live, "live").GroupBy("value").OrderBy("count DESC", "value ASC").Limit(201).Suffix(watchdogSettingsSQL), nil
}

// GroupWatchdogFindings ignores pagination and returns up to 201 groups so the
// service can return 200 plus a truncation flag. Severity values are policy IDs;
// the service merges their counts using authoritative policy scores.
func (q *Queries) GroupWatchdogFindings(ctx context.Context, p ListRiskFindingsParams, dimension string) ([]WatchdogGroup, error) {
	sb, err := watchdogGroupQuery(p, dimension)
	if err != nil {
		return nil, err
	}
	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build watchdog groups: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query watchdog groups: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []WatchdogGroup
	for rows.Next() {
		var row WatchdogGroup
		if err := rows.Scan(&row.Value, &row.Count); err != nil {
			return nil, fmt.Errorf("scan watchdog groups: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read watchdog groups: %w", err)
	}
	return out, nil
}

func watchdogCountQuery(p ListRiskFindingsParams) (squirrel.SelectBuilder, error) {
	live, err := watchdogLive(p, "id")
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	return sq.Select("count()").FromSelect(live, "live").Suffix(watchdogSettingsSQL), nil
}

// CountWatchdogFindings counts the exact full window, ignoring pagination.
func (q *Queries) CountWatchdogFindings(ctx context.Context, p ListRiskFindingsParams) (uint64, error) {
	sb, err := watchdogCountQuery(p)
	if err != nil {
		return 0, err
	}
	query, args, err := sb.ToSql()
	if err != nil {
		return 0, fmt.Errorf("build watchdog count: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("query watchdog count: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var count uint64
	for rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("scan watchdog count: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read watchdog count: %w", err)
	}
	return count, nil
}
