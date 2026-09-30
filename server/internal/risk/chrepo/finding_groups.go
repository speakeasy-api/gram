package chrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

// GroupRiskFindingsByChatParams scopes one page of the per-chat rollup of the
// Risk Events listing. PolicyIDs is the same visible-policy pushdown the flat
// listing uses; an empty list matches nothing. CursorChatID resumes at that
// chat id inclusive, walking chat ids downward, which mirrors the Postgres
// grouped listing so the two stores accept the same cursor.
type GroupRiskFindingsByChatParams struct {
	OrganizationID string
	ProjectID      string
	PolicyIDs      []string
	CursorChatID   string
	Limit          uint64
}

// RiskFindingChatGroup is one chat's live finding rollup.
type RiskFindingChatGroup struct {
	// ChatID is the denormalized chat id stamped at ingest.
	ChatID string

	// ExternalUserID is the external user id of the most recently detected
	// finding in the chat. Empty when attribution was never resolved.
	ExternalUserID string

	// FindingsCount is the number of live findings in the chat.
	FindingsCount uint64

	// LatestDetected is the detection time (created_at) of the newest live
	// finding in the chat.
	LatestDetected time.Time
}

func groupRiskFindingsByChatQuery(p GroupRiskFindingsByChatParams) (squirrel.SelectBuilder, error) {
	inner, err := listRiskFindingsBase(ListRiskFindingsParams{
		OrganizationID:  p.OrganizationID,
		ProjectID:       p.ProjectID,
		PolicyIDs:       p.PolicyIDs,
		MCPServerID:     "",
		ChatID:          "",
		From:            nil,
		To:              nil,
		Category:        "",
		RuleIDSubstr:    "",
		UserIDSubstr:    "",
		ExternalUserIDs: nil,
		AssistantID:     "",
		NonAssistant:    false,
		UniqueMatch:     false,
		CursorTime:      nil,
		CursorID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Limit:           0,
	}, "id", "chat_id", "external_user_id", "created_at", "excluded_at", "false_positive_at")
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	// Unattributed findings (gateway executions with no chat) have no session
	// to roll up into, so they are excluded before the dedup where the
	// predicate also prunes the scan.
	inner = inner.Where("chat_id != ''")
	if p.CursorChatID != "" {
		inner = inner.Where("chat_id <= ?", p.CursorChatID)
	}
	// LIMIT BY must precede LIMIT in ClickHouse; squirrel has no native
	// support, so it renders through the suffix. The live-state gate follows
	// the per-id dedup for the reason documented on listRiskFindingsBase.
	inner = inner.OrderBy(latestCopyOrderSQL).Suffix("LIMIT 1 BY id")
	return sq.Select(
		"chat_id",
		"argMax(external_user_id, created_at) AS external_user_id",
		"count() AS findings_count",
		"max(created_at) AS latest_detected",
	).
		FromSelect(inner, "latest").
		Where(liveStateCond).
		GroupBy("chat_id").
		OrderBy("chat_id DESC").
		Limit(p.Limit), nil
}

// GroupRiskFindingsByChat returns one page of chats with live findings, most
// recent chat id first. Ordering by chat id rather than activity keeps the
// cursor identical to the Postgres grouped listing: a canonical lowercase UUID
// string sorts the same as the UUID it renders.
func (q *Queries) GroupRiskFindingsByChat(ctx context.Context, p GroupRiskFindingsByChatParams) ([]RiskFindingChatGroup, error) {
	sb, err := groupRiskFindingsByChatQuery(p)
	if err != nil {
		return nil, err
	}
	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build risk findings by chat query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risk findings by chat: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RiskFindingChatGroup
	for rows.Next() {
		var row RiskFindingChatGroup
		if err := rows.Scan(&row.ChatID, &row.ExternalUserID, &row.FindingsCount, &row.LatestDetected); err != nil {
			return nil, fmt.Errorf("scan risk findings by chat row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read risk findings by chat: %w", err)
	}

	return out, nil
}

func ruleCountsByCategoryQuery(p RiskOverviewWindowParams, category string, policyIDs []string, limit uint64) (squirrel.SelectBuilder, error) {
	if len(policyIDs) == 0 {
		return squirrel.SelectBuilder{}, errEmptyPolicyIDs
	}
	return overviewFindings(p,
		"rule_id",
		"source",
		"uniqExact(id) AS findings",
	).
		Where("category = ?", category).
		Where(squirrel.Eq{"risk_policy_id": policyIDs}).
		GroupBy("rule_id", "source").
		OrderBy("findings DESC", "rule_id ASC").
		Limit(limit), nil
}

// ListRiskRuleCountsByCategory returns per-(rule_id, source) live finding
// counts for one category over a detection-time window, most findings first.
// It is the ClickHouse counterpart of the Postgres ListRiskRulesByCategory
// query: both key the window on created_at (scan time). PolicyIDs is the
// non-deleted policy pushdown the Postgres query expresses as a risk_policies
// join, because deleted policies' rows linger in this store until TTL; an
// empty list is a caller error rather than an unbounded read.
func (q *Queries) ListRiskRuleCountsByCategory(ctx context.Context, p RiskOverviewWindowParams, category string, policyIDs []string, limit uint64) ([]RiskOverviewRuleCount, error) {
	sb, err := ruleCountsByCategoryQuery(p, category, policyIDs, limit)
	if err != nil {
		return nil, err
	}
	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build risk rule counts by category query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risk rule counts by category: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RiskOverviewRuleCount
	for rows.Next() {
		var row RiskOverviewRuleCount
		if err := rows.Scan(&row.RuleID, &row.Source, &row.Findings); err != nil {
			return nil, fmt.Errorf("scan risk rule counts by category row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read risk rule counts by category: %w", err)
	}

	return out, nil
}
