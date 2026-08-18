package chrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
)

// RiskOverviewWindowParams scopes every overview read to one tenant and a
// half-open [From, To) time window. Callers cap the window (the API allows at
// most 31 days), which combines with the table's daily partitioning and
// (organization_id, project_id, created_at, id) sort key to keep these direct
// queries cheap without materialized views.
type RiskOverviewWindowParams struct {
	OrganizationID string
	ProjectID      string
	From           time.Time
	To             time.Time
}

// overviewFindings returns the base builder every overview read shares:
// tenant + window scoped, live findings only.
//
// risk_findings is append-only: a manual dismiss/undo (mirrorFalsePositiveToClickHouse)
// appends a fresh row for an id that may already have one, rather than
// updating in place, and Pub/Sub's at-least-once delivery can also redeliver
// an identical row. Deduping to one row per id is therefore required for
// correctness, not just cheapness — collapsing straight to a count would
// double-count an id with two rows and, worse, a stale first row would still
// satisfy "false_positive_at IS NULL" even after a second row dismissed it.
// The inner select keeps only each id's most-recently-inserted row
// (ROW_NUMBER() OVER ... ORDER BY inserted_at DESC); dead-letter sentinels
// and exclusion/dismissal annotations are then filtered on that latest state.
func overviewFindings(p RiskOverviewWindowParams, columns ...string) squirrel.SelectBuilder {
	latest := sq.Select("*", "ROW_NUMBER() OVER (PARTITION BY id ORDER BY inserted_at DESC) AS rn").
		From("risk_findings").
		Where("organization_id = ?", p.OrganizationID).
		Where("project_id = ?", p.ProjectID).
		Where("created_at >= ?", p.From).
		Where("created_at < ?", p.To)

	return sq.Select(columns...).
		FromSelect(latest, "latest").
		Where("rn = 1").
		Where("dead_letter_reason = ''").
		Where("excluded_at IS NULL").
		// Legacy suppression column, still filtered until the
		// false_positive_at-only rows written before the suppression
		// convergence age out under the table's 90-day TTL.
		Where("false_positive_at IS NULL")
}

// RiskOverviewFindingCounts are the window-wide headline stats.
type RiskOverviewFindingCounts struct {
	Findings        uint64
	FlaggedSessions uint64
}

// GetRiskOverviewFindingCounts returns the deduplicated finding count and the
// number of distinct chats with at least one finding. Rows with an empty
// chat_id (attribution unresolved at ingest) count as findings but not as
// flagged sessions.
func (q *Queries) GetRiskOverviewFindingCounts(ctx context.Context, p RiskOverviewWindowParams) (RiskOverviewFindingCounts, error) {
	var counts RiskOverviewFindingCounts

	query, args, err := overviewFindings(p,
		"uniqExact(id) AS findings",
		"uniqExactIf(chat_id, chat_id != '') AS flagged_sessions",
	).ToSql()
	if err != nil {
		return counts, fmt.Errorf("build risk overview finding counts query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return counts, fmt.Errorf("query risk overview finding counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&counts.Findings, &counts.FlaggedSessions); err != nil {
			return counts, fmt.Errorf("scan risk overview finding counts: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return counts, fmt.Errorf("read risk overview finding counts: %w", err)
	}

	return counts, nil
}

// RiskOverviewRuleCount is one (rule, source) group's finding count.
type RiskOverviewRuleCount struct {
	RuleID   string
	Source   string
	Findings uint64
}

// ListRiskOverviewTopRules returns per-(rule_id, source) finding counts,
// most findings first.
func (q *Queries) ListRiskOverviewTopRules(ctx context.Context, p RiskOverviewWindowParams, limit uint64) ([]RiskOverviewRuleCount, error) {
	query, args, err := overviewFindings(p,
		"rule_id",
		"source",
		"uniqExact(id) AS findings",
	).
		GroupBy("rule_id", "source").
		OrderBy("findings DESC", "rule_id ASC").
		Limit(limit).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build risk overview top rules query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risk overview top rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RiskOverviewRuleCount
	for rows.Next() {
		var row RiskOverviewRuleCount
		if err := rows.Scan(&row.RuleID, &row.Source, &row.Findings); err != nil {
			return nil, fmt.Errorf("scan risk overview top rules row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read risk overview top rules: %w", err)
	}

	return out, nil
}

// RiskOverviewCategoryBucket is one non-empty (hour, category) cell of the
// findings time series. Empty cells are absent: the caller gap-fills the full
// hour x category grid (the Postgres path does this with a generate_series
// cross join; the ClickHouse path does it in Go).
type RiskOverviewCategoryBucket struct {
	BucketStart time.Time
	Category    string
	Findings    uint64
}

// ListRiskOverviewCategoryTimeSeries returns hourly finding counts grouped by
// the category column stamped at ingest.
func (q *Queries) ListRiskOverviewCategoryTimeSeries(ctx context.Context, p RiskOverviewWindowParams) ([]RiskOverviewCategoryBucket, error) {
	query, args, err := overviewFindings(p,
		"toStartOfHour(created_at, 'UTC') AS bucket_start",
		"category",
		"uniqExact(id) AS findings",
	).
		GroupBy("bucket_start", "category").
		OrderBy("bucket_start ASC", "category ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build risk overview category time series query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risk overview category time series: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RiskOverviewCategoryBucket
	for rows.Next() {
		var row RiskOverviewCategoryBucket
		if err := rows.Scan(&row.BucketStart, &row.Category, &row.Findings); err != nil {
			return nil, fmt.Errorf("scan risk overview category time series row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read risk overview category time series: %w", err)
	}

	return out, nil
}

// RiskOverviewUserCount is one (user_id, external_user_id) group's finding
// count. Email resolution happens in Go against Postgres afterwards, so this
// stays a pure ClickHouse aggregate.
type RiskOverviewUserCount struct {
	UserID         string
	ExternalUserID string
	Findings       uint64
}

// ListRiskOverviewTopUsers returns per-user finding counts, most findings
// first. Ordering within equal counts is (user_id, external_user_id) purely
// for determinism; the caller re-sorts after resolving display emails.
func (q *Queries) ListRiskOverviewTopUsers(ctx context.Context, p RiskOverviewWindowParams, limit uint64) ([]RiskOverviewUserCount, error) {
	query, args, err := overviewFindings(p,
		"user_id",
		"external_user_id",
		"uniqExact(id) AS findings",
	).
		GroupBy("user_id", "external_user_id").
		OrderBy("findings DESC", "user_id ASC", "external_user_id ASC").
		Limit(limit).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build risk overview top users query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risk overview top users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RiskOverviewUserCount
	for rows.Next() {
		var row RiskOverviewUserCount
		if err := rows.Scan(&row.UserID, &row.ExternalUserID, &row.Findings); err != nil {
			return nil, fmt.Errorf("scan risk overview top users row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read risk overview top users: %w", err)
	}

	return out, nil
}
