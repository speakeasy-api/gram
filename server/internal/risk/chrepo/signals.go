package chrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
)

// RiskSignalWindowParams scopes signal reads to one tenant and a doubled scan
// window: [WideFrom, To) is scanned once, with From splitting it into the
// current window [From, To) and the equal-length previous window
// [WideFrom, From) via conditional aggregates. This keeps trend comparisons to
// a single pass over the partition-pruned range.
type RiskSignalWindowParams struct {
	OrganizationID string
	ProjectID      string
	WideFrom       time.Time
	From           time.Time
	To             time.Time
}

// signalFindings is the doubled-window analog of overviewFindings: the same
// per-id dedup (latest inserted_at wins) and live-finding filters, but scanning
// [WideFrom, To) so callers can split current vs previous with *If aggregates.
func signalFindings(p RiskSignalWindowParams, columns ...squirrel.Sqlizer) squirrel.SelectBuilder {
	latest := sq.Select("*", "ROW_NUMBER() OVER (PARTITION BY id ORDER BY inserted_at DESC) AS rn").
		From("risk_findings").
		Where("organization_id = ?", p.OrganizationID).
		Where("project_id = ?", p.ProjectID).
		Where("created_at >= ?", p.WideFrom).
		Where("created_at < ?", p.To)

	sb := sq.Select()
	for _, column := range columns {
		sb = sb.Column(column)
	}
	return sb.
		FromSelect(latest, "latest").
		Where("rn = 1").
		Where("dead_letter_reason = ''").
		Where("excluded_at IS NULL").
		Where("false_positive_at IS NULL")
}

// signalUserExpr is the display identity a distinct-user count groups on,
// mirroring the overview's precedence: external user id when present, else the
// internal user id. The paired non-empty guard keeps unattributed findings out
// of user counts.
const (
	signalUserExpr     = "if(external_user_id != '', external_user_id, user_id)"
	signalUserNonEmpty = "(external_user_id != '' OR user_id != '')"
)

// RiskSignalAggregate is one rule's clustered stats over the doubled window.
// Cur fields cover [From, To); Prev fields cover [WideFrom, From). FirstSeen,
// LastSeen, AvgConfidence, Apps, and TeamsCur describe the current window
// only; AvgConfidencePrev is the previous window's counterpart. Both averages
// are zero (never NaN) when their window has no findings. Apps and TeamsCur
// stay empty/zero for rows written before the chat_source and team
// attribution columns existed.
type RiskSignalAggregate struct {
	RuleID   string
	Category string
	Sources  []string
	// PolicyIDsCur and PolicyIDsPrev are the distinct non-empty
	// risk_policy_id values stamped on the rule's findings, split per
	// window. The split matters: each window's score must resolve only from
	// policies that matched that window's findings, or a policy added today
	// would retroactively rescore the previous-window baseline (and vice
	// versa). Empty for pre-policy rows.
	PolicyIDsCur      []string
	PolicyIDsPrev     []string
	Description       string
	Apps              []string
	FindingsCur       uint64
	FindingsPrev      uint64
	UsersCur          uint64
	UsersPrev         uint64
	TeamsCur          uint64
	FirstSeen         time.Time
	LastSeen          time.Time
	AvgConfidence     float64
	AvgConfidencePrev float64
}

// ListRiskSignalAggregates groups live findings by rule_id over the doubled
// window, most current-window findings first. Rules with findings only in the
// previous window are included (findings_cur = 0, sorted after every
// current-window rule) so the caller can fold them into the previous-window
// org score; only rules with current findings become signals.
//
// Grouped scalar picks are aliased with a g_ prefix so ClickHouse cannot merge
// the aggregate back into an outer query through a shadowed base column (the
// ILLEGAL_AGGREGATION failure mode).
func (q *Queries) ListRiskSignalAggregates(ctx context.Context, p RiskSignalWindowParams, limit uint64) ([]RiskSignalAggregate, error) {
	query, args, err := signalFindings(p,
		squirrel.Expr("rule_id"),
		squirrel.Alias(squirrel.Expr("any(category)"), "g_category"),
		squirrel.Alias(squirrel.Expr("groupUniqArray(source)"), "g_sources"),
		squirrel.Alias(squirrel.Expr("groupUniqArrayIf(risk_policy_id, risk_policy_id != '' AND created_at >= ?)", p.From), "g_policy_ids_cur"),
		squirrel.Alias(squirrel.Expr("groupUniqArrayIf(risk_policy_id, risk_policy_id != '' AND created_at < ?)", p.From), "g_policy_ids_prev"),
		squirrel.Alias(squirrel.Expr("any(description)"), "g_description"),
		squirrel.Alias(squirrel.Expr("groupUniqArrayIf(chat_source, chat_source != '' AND created_at >= ?)", p.From), "g_apps"),
		squirrel.Alias(squirrel.Expr("uniqExactIf(id, created_at >= ?)", p.From), "findings_cur"),
		squirrel.Alias(squirrel.Expr("uniqExactIf(id, created_at < ?)", p.From), "findings_prev"),
		squirrel.Alias(squirrel.Expr("uniqExactIf("+signalUserExpr+", "+signalUserNonEmpty+" AND created_at >= ?)", p.From), "users_cur"),
		squirrel.Alias(squirrel.Expr("uniqExactIf("+signalUserExpr+", "+signalUserNonEmpty+" AND created_at < ?)", p.From), "users_prev"),
		squirrel.Alias(squirrel.Expr("uniqExactIf(team, team != '' AND created_at >= ?)", p.From), "teams_cur"),
		squirrel.Alias(squirrel.Expr("minIf(message_created_at, created_at >= ?)", p.From), "first_seen"),
		squirrel.Alias(squirrel.Expr("maxIf(message_created_at, created_at >= ?)", p.From), "last_seen"),
		// avgIf over an empty window is NaN; ifNotFinite pins it to zero so
		// the scanned struct never carries a non-finite float.
		squirrel.Alias(squirrel.Expr("ifNotFinite(avgIf(confidence, created_at >= ?), 0)", p.From), "avg_confidence"),
		squirrel.Alias(squirrel.Expr("ifNotFinite(avgIf(confidence, created_at < ?), 0)", p.From), "avg_confidence_prev"),
	).
		GroupBy("rule_id").
		OrderBy("findings_cur DESC", "rule_id ASC").
		Limit(limit).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build risk signal aggregates query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risk signal aggregates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RiskSignalAggregate
	for rows.Next() {
		var row RiskSignalAggregate
		if err := rows.Scan(
			&row.RuleID,
			&row.Category,
			&row.Sources,
			&row.PolicyIDsCur,
			&row.PolicyIDsPrev,
			&row.Description,
			&row.Apps,
			&row.FindingsCur,
			&row.FindingsPrev,
			&row.UsersCur,
			&row.UsersPrev,
			&row.TeamsCur,
			&row.FirstSeen,
			&row.LastSeen,
			&row.AvgConfidence,
			&row.AvgConfidencePrev,
		); err != nil {
			return nil, fmt.Errorf("scan risk signal aggregates row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read risk signal aggregates: %w", err)
	}

	return out, nil
}

// RiskSignalUserCount is one (rule, user) group's finding count in the current
// window. Team and Email are any non-empty values stamped on the user's
// findings at ingest, empty when unattributed — no Postgres resolution
// happens on this read path.
type RiskSignalUserCount struct {
	RuleID         string
	UserID         string
	ExternalUserID string
	Team           string
	Email          string
	Findings       uint64
}

// ListRiskSignalTopUsers returns per-(rule, user) finding counts over the
// current window, capped at perRule rows per rule via LIMIT BY. totalLimit
// bounds the overall result as a safety net against pathological rule
// cardinality.
func (q *Queries) ListRiskSignalTopUsers(ctx context.Context, p RiskOverviewWindowParams, perRule, totalLimit uint64) ([]RiskSignalUserCount, error) {
	query, args, err := overviewFindings(p,
		"rule_id",
		"user_id",
		"external_user_id",
		"anyIf(team, team != '') AS g_team",
		"anyIf(user_email, user_email != '') AS g_email",
		"uniqExact(id) AS findings",
	).
		GroupBy("rule_id", "user_id", "external_user_id").
		OrderBy("rule_id ASC", "findings DESC", "user_id ASC", "external_user_id ASC").
		Suffix(fmt.Sprintf("LIMIT %d BY rule_id LIMIT %d", perRule, totalLimit)).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build risk signal top users query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risk signal top users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RiskSignalUserCount
	for rows.Next() {
		var row RiskSignalUserCount
		if err := rows.Scan(&row.RuleID, &row.UserID, &row.ExternalUserID, &row.Team, &row.Email, &row.Findings); err != nil {
			return nil, fmt.Errorf("scan risk signal top users row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read risk signal top users: %w", err)
	}

	return out, nil
}

// RiskSignalSeriesPoint is one non-empty (rule, bucket) cell of the
// per-signal sparkline series. Buckets are epoch-aligned to the given width;
// empty cells are absent and the caller gap-fills dense arrays.
type RiskSignalSeriesPoint struct {
	RuleID      string
	BucketStart time.Time
	Findings    uint64
}

// ListRiskSignalSeries returns deduplicated finding counts per
// (rule, time bucket) over the current window. bucketSeconds is embedded as an
// integer literal (not a bind parameter) because it composes into the bucket
// expression; callers derive it from the window arithmetic, never from user
// input.
func (q *Queries) ListRiskSignalSeries(ctx context.Context, p RiskOverviewWindowParams, bucketSeconds int64) ([]RiskSignalSeriesPoint, error) {
	query, args, err := overviewFindings(p,
		"rule_id",
		fmt.Sprintf("toDateTime(intDiv(toUnixTimestamp(created_at), %d) * %d) AS bucket_start", bucketSeconds, bucketSeconds),
		"uniqExact(id) AS findings",
	).
		GroupBy("rule_id", "bucket_start").
		OrderBy("rule_id ASC", "bucket_start ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build risk signal series query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risk signal series: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RiskSignalSeriesPoint
	for rows.Next() {
		var row RiskSignalSeriesPoint
		if err := rows.Scan(&row.RuleID, &row.BucketStart, &row.Findings); err != nil {
			return nil, fmt.Errorf("scan risk signal series row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read risk signal series: %w", err)
	}

	return out, nil
}

// RiskSignalSplitCounts are window-level distinct counts split at From:
// Cur covers [From, To), Prev covers [WideFrom, From). Distinct-user counts
// cannot be summed from per-rule aggregates (the same user may trip several
// rules), hence this dedicated window-level pass.
type RiskSignalSplitCounts struct {
	FindingsCur  uint64
	FindingsPrev uint64
	UsersCur     uint64
	UsersPrev    uint64
}

// GetRiskSignalSplitCounts returns deduplicated finding and distinct-user
// counts for the current and previous halves of the scan window. Callers pick
// the window: the KPI row uses it once with [to-48h, split to-24h, to) for the
// 24-hour cards and once with the full doubled request window for users
// exposed.
func (q *Queries) GetRiskSignalSplitCounts(ctx context.Context, p RiskSignalWindowParams) (RiskSignalSplitCounts, error) {
	var counts RiskSignalSplitCounts

	query, args, err := signalFindings(p,
		squirrel.Alias(squirrel.Expr("uniqExactIf(id, created_at >= ?)", p.From), "findings_cur"),
		squirrel.Alias(squirrel.Expr("uniqExactIf(id, created_at < ?)", p.From), "findings_prev"),
		squirrel.Alias(squirrel.Expr("uniqExactIf("+signalUserExpr+", "+signalUserNonEmpty+" AND created_at >= ?)", p.From), "users_cur"),
		squirrel.Alias(squirrel.Expr("uniqExactIf("+signalUserExpr+", "+signalUserNonEmpty+" AND created_at < ?)", p.From), "users_prev"),
	).ToSql()
	if err != nil {
		return counts, fmt.Errorf("build risk signal split counts query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return counts, fmt.Errorf("query risk signal split counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&counts.FindingsCur, &counts.FindingsPrev, &counts.UsersCur, &counts.UsersPrev); err != nil {
			return counts, fmt.Errorf("scan risk signal split counts: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return counts, fmt.Errorf("read risk signal split counts: %w", err)
	}

	return counts, nil
}
