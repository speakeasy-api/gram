package chrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
)

// WatchdogAlert groups one rule's live findings across all contributing policies.
// Resolve severity upstream using the maximum non-deleted policy score (including
// disabled policies), with category fallback. Clients are observed chat_source
// product surfaces, not devices or OAuth clients. SampleEvidence is a stored
// display sample that MAY contain passthrough text: callers MUST fully redact
// it before public exposure. No raw match is fetched.
type WatchdogAlert struct {
	RuleID         string
	Category       string
	PolicyIDs      []string
	FindingCount   uint64
	UsersAffected  uint64
	Clients        []string
	FirstSeen      time.Time
	LastSeen       time.Time
	SampleEvidence string
}

// WatchdogAlertLimit includes a sentinel: callers MUST fail closed when more
// than 1000 rows are returned, before filtering or paginating alerts.
const WatchdogAlertLimit = 1001
const watchdogAlertValueLimit = 200

func watchdogAlertPopulation(p RiskSignalWindowParams, columns ...squirrel.Sqlizer) (squirrel.SelectBuilder, error) {
	if p.OrganizationID == "" || p.ProjectID == "" || p.From.IsZero() || p.To.IsZero() || !p.From.Before(p.To) {
		return squirrel.SelectBuilder{}, fmt.Errorf("watchdog alerts require a tenant and nonempty bounded time window")
	}
	p.WideFrom = p.From
	return signalFindings(p, columns...), nil
}

func watchdogAlertsQuery(p RiskSignalWindowParams, limit uint64) (squirrel.SelectBuilder, error) {
	if limit == 0 || limit > WatchdogAlertLimit {
		return squirrel.SelectBuilder{}, fmt.Errorf("watchdog alert limit must be between 1 and %d", WatchdogAlertLimit)
	}
	live, err := watchdogAlertPopulation(p,
		squirrel.Expr("rule_id"),
		squirrel.Expr("any(category) AS alert_category"),
		squirrel.Expr("arraySort(groupUniqArrayIf(201)(risk_policy_id, risk_policy_id != '')) AS policy_ids"),
		squirrel.Expr("count() AS finding_count"),
		squirrel.Expr("uniqExactIf("+signalUserExpr+", "+signalUserNonEmpty+") AS users_affected"),
		squirrel.Expr("arraySort(groupUniqArrayIf(201)(chat_source, chat_source != '')) AS clients"),
		squirrel.Expr("min(message_created_at) AS first_seen"),
		squirrel.Expr("max(message_created_at) AS last_seen"),
		squirrel.Expr("argMaxIf(match_redacted, tuple(message_created_at, id), match_redacted != '') AS sample_evidence"),
	)
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	return live.GroupBy("rule_id").OrderBy("finding_count DESC", "rule_id ASC").Limit(limit).Suffix(watchdogSettingsSQL), nil
}

// ListWatchdogAlerts uses canonical signal latest-copy/suppression semantics and
// the created_at window [From, To); WideFrom is ignored. FirstSeen/LastSeen are
// message times, which may lie outside that window. No policy filter is applied.
// Request WatchdogAlertLimit to detect rule overflow. More than 200 distinct
// clients or policies per rule fails closed rather than returning incomplete
// arrays and potentially understating severity.
func (q *Queries) ListWatchdogAlerts(ctx context.Context, p RiskSignalWindowParams, limit uint64) ([]WatchdogAlert, error) {
	sb, err := watchdogAlertsQuery(p, limit)
	if err != nil {
		return nil, err
	}
	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build watchdog alerts: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query watchdog alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []WatchdogAlert
	for rows.Next() {
		var row WatchdogAlert
		if err := rows.Scan(&row.RuleID, &row.Category, &row.PolicyIDs, &row.FindingCount, &row.UsersAffected, &row.Clients, &row.FirstSeen, &row.LastSeen, &row.SampleEvidence); err != nil {
			return nil, fmt.Errorf("scan watchdog alert: %w", err)
		}
		if len(row.Clients) > watchdogAlertValueLimit || len(row.PolicyIDs) > watchdogAlertValueLimit {
			return nil, fmt.Errorf("watchdog alert exceeds distinct client or policy limit")
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read watchdog alerts: %w", err)
	}
	return out, nil
}

// WatchdogAlertGroup is one attribution bucket within one selected rule.
type WatchdogAlertGroup struct {
	RuleID string
	Value  string
	Count  uint64
}

func watchdogAlertGroupsQuery(p RiskSignalWindowParams, ruleIDs []string, dimension string) (squirrel.SelectBuilder, error) {
	if len(ruleIDs) == 0 || len(ruleIDs) > 100 {
		return squirrel.SelectBuilder{}, fmt.Errorf("watchdog alert groups require between 1 and 100 rule IDs")
	}
	var expression string
	switch dimension {
	case "data_type":
		expression = "category"
	case "team":
		expression = "team"
	case "app":
		expression = "chat_source"
	case "user":
		expression = signalUserExpr
	default:
		return squirrel.SelectBuilder{}, fmt.Errorf("unsupported watchdog alert dimension %q", dimension)
	}
	live, err := watchdogAlertPopulation(p, squirrel.Expr("rule_id"), squirrel.Expr(expression+" AS value"), squirrel.Expr("count() AS count"))
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	if dimension == "user" {
		live = live.Where(signalUserNonEmpty)
	}
	return live.Where(squirrel.Eq{"rule_id": ruleIDs}).GroupBy("rule_id", "value").OrderBy("rule_id ASC", "count DESC", "value ASC").Suffix("LIMIT 201 BY rule_id").Suffix(watchdogSettingsSQL), nil
}

// GroupWatchdogAlerts batches up to 100 selected rules into one scan per
// dimension. Counts use the same canonical live population as ListWatchdogAlerts.
// Empty attribution is retained except for user. Each rule returns at most 201
// buckets: callers can expose 200 plus truncation. Select ruleIDs after resolving
// severity; these are per-alert finding histograms, not dashboard sections.
func (q *Queries) GroupWatchdogAlerts(ctx context.Context, p RiskSignalWindowParams, ruleIDs []string, dimension string) ([]WatchdogAlertGroup, error) {
	sb, err := watchdogAlertGroupsQuery(p, ruleIDs, dimension)
	if err != nil {
		return nil, err
	}
	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build watchdog alert groups: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query watchdog alert groups: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []WatchdogAlertGroup
	for rows.Next() {
		var row WatchdogAlertGroup
		if err := rows.Scan(&row.RuleID, &row.Value, &row.Count); err != nil {
			return nil, fmt.Errorf("scan watchdog alert group: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read watchdog alert groups: %w", err)
	}
	return out, nil
}
