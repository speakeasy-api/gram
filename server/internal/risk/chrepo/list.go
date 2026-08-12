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

// listRiskFindingsBase applies the filters shared by the list and count reads
// that are immutable across an id's copies: tenancy, dead-letter sentinels and
// the enabled-policy pushdown. The exclusion / false-positive state is
// deliberately NOT here: those flags change by appending a newer copy of the
// row (the retroactive reconcile, the false-positive mirror), so filtering
// them before the latest-copy-per-id dedup would drop the flagged copy and
// let a stale live copy win — callers must gate on them AFTER dedup, the way
// overview.go and signals.go do.
func listRiskFindingsBase(p ListRiskFindingsParams, columns ...string) (squirrel.SelectBuilder, error) {
	if len(p.PolicyIDs) == 0 {
		return squirrel.SelectBuilder{}, errEmptyPolicyIDs
	}
	sb := sq.Select(columns...).
		From("risk_findings").
		Where("organization_id = ?", p.OrganizationID).
		Where("project_id = ?", p.ProjectID).
		Where("dead_letter_reason = ''").
		Where(squirrel.Eq{"risk_policy_id": p.PolicyIDs})
	return sb, nil
}

// liveStateCond gates the latest copy of a finding to live rows only — not
// excluded, not marked false positive. Applied after the per-id dedup.
const liveStateCond = "excluded_at IS NULL AND false_positive_at IS NULL"

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
		// Three layers, innermost first: (1) latest copy per id — the state
		// flags on any older copy are stale; (2) live-state gate, then LIMIT 1
		// BY the match group so each group resolves to its newest LIVE
		// occurrence; (3) cursor + page over the deduped stream. Dedup stays
		// before the cursor: applying the cursor first would remove a group's
		// newest occurrence once it fell behind the cursor and let an older
		// occurrence win LIMIT BY, repeating the group on a later page.
		sb = sb.Column("excluded_at").Column("false_positive_at").
			Column("fingerprint_tenant_hs256").
			Suffix("LIMIT 1 BY id")
		grouped := sq.Select(columns...).
			FromSelect(sb, "latest").
			Where(liveStateCond).
			OrderBy("message_created_at DESC", "id DESC").
			Suffix("LIMIT 1 BY (risk_policy_id, rule_id, " + uniqueMatchKey + ")")
		outer := sq.Select(columns...).FromSelect(grouped, "deduped")
		if hasCursor {
			outer = outer.Where(cursorCond, *p.CursorTime, p.CursorID.UUID)
		}
		sb = outer.
			OrderBy("message_created_at DESC", "id DESC").
			Limit(p.Limit)
	} else {
		// Plain id dedup can stay cursor-first: an id's copies share both
		// message_created_at and id, so the cursor keeps or drops a whole
		// group at once and no group can straddle a page boundary. The
		// live-state gate comes AFTER the LIMIT 1 BY id dedup — see
		// listRiskFindingsBase.
		if hasCursor {
			sb = sb.Where(cursorCond, *p.CursorTime, p.CursorID.UUID)
		}
		// LIMIT BY must precede LIMIT in ClickHouse; squirrel has no native
		// support, so it renders through the suffix.
		sb = sb.Column("excluded_at").Column("false_positive_at").
			Suffix("LIMIT 1 BY id")
		sb = sq.Select(columns...).
			FromSelect(sb, "latest").
			Where(liveStateCond).
			OrderBy("message_created_at DESC", "id DESC").
			Limit(p.Limit)
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
// time-window or per-column filters. Each id resolves to its latest copy
// first (redelivered duplicates and appended flag copies are expected in this
// table) and only then is the live-state gate applied, so a finding whose
// newest copy carries an exclusion or false-positive flag is not counted.
func (q *Queries) CountRiskFindings(ctx context.Context, p ListRiskFindingsParams) (uint64, error) {
	inner, err := listRiskFindingsBase(p, "id", "excluded_at", "false_positive_at")
	if err != nil {
		return 0, err
	}
	inner = inner.OrderBy("inserted_at DESC").Suffix("LIMIT 1 BY id")
	sb := sq.Select("count() AS findings").
		FromSelect(inner, "latest").
		Where(liveStateCond)

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
