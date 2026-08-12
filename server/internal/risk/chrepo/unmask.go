package chrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// GetRiskFindingForUnmaskParams scopes the reveal point-read to one tenant and
// one finding id.
type GetRiskFindingForUnmaskParams struct {
	OrganizationID string
	ProjectID      string
	ID             uuid.UUID
}

// RiskFindingUnmaskRow is the reveal-relevant state of one finding: the anchor
// ids locating the scanned text, the offsets and surface describing which text
// they index, and the match length the reconstructed candidate is checked
// against. See internal/risk/unmask_ch.go for how it is consumed.
type RiskFindingUnmaskRow struct {
	ID             uuid.UUID
	CreatedAt      time.Time
	ChatMessageID  string
	ContentPartID  string
	ChatID         string
	Source         string
	RuleID         string
	StartPos       int32
	EndPos         int32
	MatchLen       uint32
	MatchRedacted  string
	Surface        string
	Field          string
	Path           string
	ToolCallID     string
	OrganizationID string
}

// GetRiskFindingForUnmask returns the reveal-relevant state for one finding id,
// or nil when no live row exists. The gates mirror the Postgres unmask ones
// (GetRiskResultByID): no dead-letter sentinels, no excluded rows, no false
// positives, tenant-scoped.
//
// The table is append-only with at-least-once delivery, so one id can have
// several rows (redeliveries and exclusion / false-positive state mirrors).
// The newest row by inserted_at is the finding's current state, so it is
// selected FIRST and the visibility gates are applied to it: gating before
// picking would let an older, still-clean row of a since-dismissed finding
// survive and be revealed.
func (q *Queries) GetRiskFindingForUnmask(ctx context.Context, p GetRiskFindingForUnmaskParams) (*RiskFindingUnmaskRow, error) {
	latest := sq.Select(
		"id",
		"created_at",
		"chat_message_id",
		"content_part_id",
		"chat_id",
		"source",
		"rule_id",
		"start_pos",
		"end_pos",
		"match_len",
		"match_redacted",
		"surface",
		"field",
		"path",
		"tool_call_id",
		"organization_id",
		"dead_letter_reason",
		"excluded_at",
		"false_positive_at",
	).
		From("risk_findings").
		Where("organization_id = ?", p.OrganizationID).
		Where("project_id = ?", p.ProjectID).
		Where("id = ?", p.ID).
		OrderBy("inserted_at DESC").
		Limit(1)

	sb := sq.Select(
		"id",
		"created_at",
		"chat_message_id",
		"content_part_id",
		"chat_id",
		"source",
		"rule_id",
		"start_pos",
		"end_pos",
		"match_len",
		"match_redacted",
		"surface",
		"field",
		"path",
		"tool_call_id",
		"organization_id",
	).
		FromSelect(latest, "latest").
		Where("dead_letter_reason = ''").
		Where("excluded_at IS NULL").
		Where("false_positive_at IS NULL")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build risk finding unmask query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risk finding for unmask: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("read risk finding unmask rows: %w", err)
		}
		return nil, nil
	}

	var row RiskFindingUnmaskRow
	if err := rows.Scan(
		&row.ID,
		&row.CreatedAt,
		&row.ChatMessageID,
		&row.ContentPartID,
		&row.ChatID,
		&row.Source,
		&row.RuleID,
		&row.StartPos,
		&row.EndPos,
		&row.MatchLen,
		&row.MatchRedacted,
		&row.Surface,
		&row.Field,
		&row.Path,
		&row.ToolCallID,
		&row.OrganizationID,
	); err != nil {
		return nil, fmt.Errorf("scan risk finding unmask row: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read risk finding unmask rows: %w", err)
	}

	return &row, nil
}
