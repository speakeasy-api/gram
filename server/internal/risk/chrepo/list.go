package chrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

// errEmptyPolicyIDs guards the enabled-policy pushdown contract: callers must
// resolve at least one visible policy id before querying; an accidental empty
// list would otherwise silently list nothing (or, worse, everything if the
// filter were dropped).
var errEmptyPolicyIDs = errors.New("risk findings list requires at least one policy id")

// uniqueMatchKey is the LIMIT BY / grouping key for the unique-match toggle.
// The Postgres list dedups on the raw match value; ClickHouse never stores it,
// so the tenant-qualified HMAC fingerprint is the equivalent. Rows where
// fingerprinting failed at ingest (empty fingerprint) fall back to their own
// id so they stay individually unique instead of collapsing into one group
// per (policy, rule).
const uniqueMatchKey = "if(fingerprint_tenant_hs256 != '', fingerprint_tenant_hs256, toString(id))"

// ListRiskFindingsParams scopes one page of the Risk Events listing. From/To
// bound message_created_at (the event-time sort key); Cursor* resume strictly
// after a prior page's last row. PolicyIDs is the enabled-policy pushdown: the
// caller resolves which policies are visible (enabled ones by default, or the
// explicitly filtered policy including disabled) from Postgres, because
// ClickHouse cannot join risk_policies. An empty PolicyIDs matches nothing.
type ListRiskFindingsParams struct {
	OrganizationID string
	ProjectID      string
	PolicyIDs      []string
	From           *time.Time
	To             *time.Time
	Category       string
	RuleIDSubstr   string
	UserIDSubstr   string
	AssistantID    string
	NonAssistant   bool
	UniqueMatch    bool
	CursorTime     *time.Time
	CursorID       uuid.NullUUID
	Limit          uint64
}

// RiskFindingListRow is one listing row served from ClickHouse. The match is
// only ever the precomputed redacted display string — the raw value never
// reaches this store.
type RiskFindingListRow struct {
	ID                uuid.UUID
	MessageCreatedAt  time.Time
	ChatMessageID     string
	ContentPartID     string
	ChatID            string
	ExternalUserID    string
	AssistantID       string
	RiskPolicyID      string
	RiskPolicyVersion int64
	RuleID            string
	Description       string
	Source            string
	Confidence        float64
	Tags              []string
	StartPos          int32
	EndPos            int32
	MatchRedacted     string
}

// listRiskFindingsBase applies the filters shared by the list and count reads:
// tenancy, live findings only (no dead-letter sentinels, no excluded or
// false-positive rows) and the enabled-policy pushdown.
func listRiskFindingsBase(p ListRiskFindingsParams, columns ...string) (squirrel.SelectBuilder, error) {
	if len(p.PolicyIDs) == 0 {
		return squirrel.SelectBuilder{}, errEmptyPolicyIDs
	}
	sb := sq.Select(columns...).
		From("risk_findings").
		Where("organization_id = ?", p.OrganizationID).
		Where("project_id = ?", p.ProjectID).
		Where("dead_letter_reason = ''").
		Where("excluded_at IS NULL").
		Where("false_positive_at IS NULL").
		Where(squirrel.Eq{"risk_policy_id": p.PolicyIDs})
	return sb, nil
}

// ListRiskFindings returns one page of findings ordered by
// (message_created_at, id) descending — the same sort and cursor semantics as
// the Postgres listing, so a pager can cross the flag flip without a cursor
// discontinuity.
//
// Dedup, always on: the table is a plain append-only MergeTree fed by
// at-least-once Pub/Sub delivery, so redelivered rows sharing an id are
// expected; inserted_at DESC in the sort order plus LIMIT 1 BY id keeps the
// latest-inserted copy. With UniqueMatch the LIMIT BY key widens to
// (risk_policy_id, rule_id, tenant-fingerprint) keeping the newest occurrence,
// which subsumes the id dedup (duplicates share the whole key).
func (q *Queries) ListRiskFindings(ctx context.Context, p ListRiskFindingsParams) ([]RiskFindingListRow, error) {
	columns := []string{
		"id",
		"message_created_at",
		"chat_message_id",
		"content_part_id",
		"chat_id",
		"external_user_id",
		"assistant_id",
		"risk_policy_id",
		"risk_policy_version",
		"rule_id",
		"description",
		"source",
		"confidence",
		"tags",
		"start_pos",
		"end_pos",
		"match_redacted",
	}
	sb, err := listRiskFindingsBase(p, columns...)
	if err != nil {
		return nil, err
	}

	if p.From != nil {
		sb = sb.Where("message_created_at >= ?", *p.From)
		// Derived bound for partition pruning: the table is partitioned daily
		// on created_at (scan time), and a scan always happens at or after its
		// message's event time, so message_created_at >= From implies
		// created_at >= From.
		sb = sb.Where("created_at >= ?", *p.From)
	}
	if p.To != nil {
		sb = sb.Where("message_created_at < ?", *p.To)
	}
	if p.Category != "" {
		sb = sb.Where("category = ?", p.Category)
	}
	if p.RuleIDSubstr != "" {
		sb = sb.Where("positionCaseInsensitive(rule_id, ?) > 0", p.RuleIDSubstr)
	}
	if p.UserIDSubstr != "" {
		sb = sb.Where("positionCaseInsensitive(external_user_id, ?) > 0", p.UserIDSubstr)
	}
	if p.AssistantID != "" {
		sb = sb.Where("assistant_id = ?", p.AssistantID)
	} else if p.NonAssistant {
		sb = sb.Where("assistant_id = ''")
	}
	sb = sb.OrderBy("message_created_at DESC", "id DESC", "inserted_at DESC")

	// cursorCond resumes strictly after a prior page's last row; both branches
	// below share it so the cursor shape cannot drift between them.
	const cursorCond = "(message_created_at, id) < (?, ?)"
	hasCursor := p.CursorTime != nil && p.CursorID.Valid

	if p.UniqueMatch {
		// Dedup before the cursor: the inner LIMIT 1 BY runs over the full
		// filtered set so each group resolves to its single newest occurrence,
		// and only then does the outer query apply the cursor and page over
		// the deduped stream — the same subquery shape as the Postgres
		// listing's ROW_NUMBER dedup. Applying the cursor first would remove a
		// group's newest occurrence once it fell behind the cursor and let an
		// older occurrence win LIMIT BY, repeating the group on a later page.
		sb = sb.Suffix("LIMIT 1 BY (risk_policy_id, rule_id, " + uniqueMatchKey + ")")
		outer := sq.Select(columns...).FromSelect(sb, "deduped")
		if hasCursor {
			outer = outer.Where(cursorCond, *p.CursorTime, p.CursorID.UUID)
		}
		sb = outer.
			OrderBy("message_created_at DESC", "id DESC").
			Limit(p.Limit)
	} else {
		// Plain id dedup can stay cursor-first: redelivered copies share both
		// message_created_at and id, so the cursor keeps or drops a whole
		// group at once and no group can straddle a page boundary.
		if hasCursor {
			sb = sb.Where(cursorCond, *p.CursorTime, p.CursorID.UUID)
		}
		// LIMIT BY must precede LIMIT in ClickHouse; squirrel has no native
		// support, so both render through the suffix.
		sb = sb.Suffix(fmt.Sprintf("LIMIT 1 BY id LIMIT %d", p.Limit))
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build risk findings list query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risk findings list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RiskFindingListRow
	for rows.Next() {
		var row RiskFindingListRow
		if err := rows.Scan(
			&row.ID,
			&row.MessageCreatedAt,
			&row.ChatMessageID,
			&row.ContentPartID,
			&row.ChatID,
			&row.ExternalUserID,
			&row.AssistantID,
			&row.RiskPolicyID,
			&row.RiskPolicyVersion,
			&row.RuleID,
			&row.Description,
			&row.Source,
			&row.Confidence,
			&row.Tags,
			&row.StartPos,
			&row.EndPos,
			&row.MatchRedacted,
		); err != nil {
			return nil, fmt.Errorf("scan risk findings list row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read risk findings list: %w", err)
	}

	return out, nil
}

// CountRiskFindings mirrors the Postgres CountAllFindings semantics for the
// listing's total count: live findings scoped to the visible policies, with no
// time-window or per-column filters, counted as uniqExact(id) because
// redelivered duplicate ids are expected in this table.
func (q *Queries) CountRiskFindings(ctx context.Context, p ListRiskFindingsParams) (uint64, error) {
	sb, err := listRiskFindingsBase(p, "uniqExact(id) AS findings")
	if err != nil {
		return 0, err
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return 0, fmt.Errorf("build risk findings count query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("query risk findings count: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var count uint64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("scan risk findings count: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read risk findings count: %w", err)
	}

	return count, nil
}
