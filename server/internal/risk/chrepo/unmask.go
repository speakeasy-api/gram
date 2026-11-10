package chrepo

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

// GetRiskFindingForUnmaskParams scopes the reveal point-read to one tenant and
// one finding id.
type GetRiskFindingForUnmaskParams struct {
	OrganizationID string
	ProjectID      string
	ID             uuid.UUID
}

// ListRiskFindingsForUnmaskParams scopes a batch reveal read to one tenant.
type ListRiskFindingsForUnmaskParams struct {
	OrganizationID string
	ProjectID      string
	IDs            []uuid.UUID
}

// ListPolicyFilterSweepCandidatesParams scopes one ascending candidate page.
type ListPolicyFilterSweepCandidatesParams struct {
	OrganizationID string
	ProjectID      string
	PolicyID       string
	From           time.Time
	To             time.Time
	CursorTime     time.Time
	CursorID       uuid.UUID
	Limit          int
}

// ListRiskFindingSpansForRecallParams scopes recall masking to one chat.
type ListRiskFindingSpansForRecallParams struct {
	OrganizationID string
	ProjectID      string
	ChatID         string
	PolicyIDs      []string
}

// RiskFindingUnmaskRow is the reveal-relevant state of one finding: the anchor
// ids locating the scanned text, the offsets and surface describing which text
// they index, and the match length the reconstructed candidate is checked
// against. See internal/risk/unmask_ch.go for how it is consumed.
type RiskFindingUnmaskRow struct {
	ID               uuid.UUID
	CreatedAt        time.Time
	ChatMessageID    string
	ContentPartID    string
	ChatID           string
	Source           string
	RuleID           string
	StartPos         int32
	EndPos           int32
	MatchLen         uint32
	MatchRedacted    string
	Surface          string
	MediationSurface string
	Field            string
	Path             string
	ToolCallID       string
	OrganizationID   string
	RiskPolicyID     string
	ExecutionID      string
	Phase            string
}

var riskFindingRevealColumns = []string{
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
	"mediation_surface",
	"field",
	"path",
	"tool_call_id",
	"organization_id",
	"risk_policy_id",
	"execution_id",
	"phase",
}
var riskFindingRevealLatestColumns = append(slices.Clone(riskFindingRevealColumns),
	"dead_letter_reason",
	"excluded_at",
	"false_positive_at",
	"ROW_NUMBER() OVER (PARTITION BY id ORDER BY "+latestCopyOrderSQL+") AS rn",
)

// liveRiskFindingRevealQuery ranks each id's newest state before visibility gates.
func liveRiskFindingRevealQuery(organizationID, projectID, chatID string, ids []uuid.UUID, policyIDs []string) squirrel.SelectBuilder {
	innerColumns := riskFindingRevealLatestColumns
	inner := sq.Select(innerColumns...).
		From("risk_findings").
		Where("organization_id = ?", organizationID).
		Where("project_id = ?", projectID).
		Where(notShadowCond)
	if chatID != "" {
		inner = inner.Where("chat_id = ?", chatID)
	}
	if len(ids) > 0 {
		inner = inner.Where(squirrel.Eq{"id": ids})
	}
	if len(policyIDs) > 0 {
		inner = inner.Where(squirrel.Eq{"risk_policy_id": policyIDs})
	}

	return sq.Select(riskFindingRevealColumns...).
		FromSelect(inner, "latest").
		Where("rn = 1").
		Where("dead_letter_reason = ''").
		Where("excluded_at IS NULL").
		// Legacy suppression column, still filtered until the
		// false_positive_at-only rows written before the suppression
		// convergence age out under the table's 90-day TTL.
		Where("false_positive_at IS NULL")
}

type riskFindingRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanRiskFindingUnmaskRows(rows riskFindingRows) ([]RiskFindingUnmaskRow, error) {
	var out []RiskFindingUnmaskRow
	for rows.Next() {
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
			&row.MediationSurface,
			&row.Field,
			&row.Path,
			&row.ToolCallID,
			&row.OrganizationID,
			&row.RiskPolicyID,
			&row.ExecutionID,
			&row.Phase,
		); err != nil {
			return nil, fmt.Errorf("scan risk finding reveal row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read risk finding reveal rows: %w", err)
	}
	return out, nil
}

// GetRiskFindingForUnmask returns the reveal-relevant state for one finding id,
// or nil when no live row exists: no dead-letter sentinels, no excluded rows,
// no false positives, tenant-scoped. Shadow engine-comparison rows are hidden
// from every user-facing surface, so a shadow id reads as absent here too.
//
// The table is append-only with at-least-once delivery, so one id can have
// several rows (redeliveries and exclusion / false-positive state mirrors).
// The newest row by inserted_at is the finding's current state, so it is
// selected FIRST and the visibility gates are applied to it: gating before
// picking would let an older, still-clean row of a since-dismissed finding
// survive and be revealed.
func (q *Queries) GetRiskFindingForUnmask(ctx context.Context, p GetRiskFindingForUnmaskParams) (*RiskFindingUnmaskRow, error) {
	rows, err := q.ListRiskFindingsForUnmask(ctx, ListRiskFindingsForUnmaskParams{
		OrganizationID: p.OrganizationID,
		ProjectID:      p.ProjectID,
		IDs:            []uuid.UUID{p.ID},
	})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// ListRiskFindingsForUnmask returns live tenant-scoped rows for the ids.
//
// Each id's newest state copy is selected first and visibility gates are
// applied afterward. Gating first would let an older clean copy of a
// since-dismissed finding survive and be revealed.
func (q *Queries) ListRiskFindingsForUnmask(ctx context.Context, p ListRiskFindingsForUnmaskParams) ([]RiskFindingUnmaskRow, error) {
	if len(p.IDs) == 0 {
		return []RiskFindingUnmaskRow{}, nil
	}
	query, args, err := liveRiskFindingRevealQuery(p.OrganizationID, p.ProjectID, "", p.IDs, nil).ToSql()
	if err != nil {
		return nil, fmt.Errorf("build risk findings reveal query: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risk findings for reveal: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanRiskFindingUnmaskRows(rows)
}

// GetPolicyFilterSweepCursorTime resolves the immutable created_at component
// needed to resume the ClickHouse sweep's (created_at, id) keyset.
func (q *Queries) GetPolicyFilterSweepCursorTime(ctx context.Context, organizationID, projectID string, id uuid.UUID) (time.Time, error) {
	rows, err := q.conn.Query(ctx, `
		SELECT created_at
		FROM risk_findings
		WHERE organization_id = ? AND project_id = ? AND id = ?
		LIMIT 1
	`, organizationID, projectID, id)
	if err != nil {
		return time.Time{}, fmt.Errorf("query policy filter sweep cursor: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return time.Time{}, fmt.Errorf("read policy filter sweep cursor: %w", err)
		}
		return time.Time{}, fmt.Errorf("policy filter sweep cursor %s not found", id)
	}
	var createdAt time.Time
	if err := rows.Scan(&createdAt); err != nil {
		return time.Time{}, fmt.Errorf("scan policy filter sweep cursor: %w", err)
	}
	return createdAt, nil
}

// ListPolicyFilterSweepCandidates returns the latest live, non-shadow copy per
// finding in ascending (created_at, id) order.
func (q *Queries) ListPolicyFilterSweepCandidates(ctx context.Context, p ListPolicyFilterSweepCandidatesParams) ([]RiskFindingUnmaskRow, error) {
	if p.Limit <= 0 {
		return []RiskFindingUnmaskRow{}, nil
	}
	inner := sq.Select(riskFindingRevealLatestColumns...).
		From("risk_findings").
		Where("organization_id = ?", p.OrganizationID).
		Where("project_id = ?", p.ProjectID).
		Where(notShadowCond).
		Where("created_at >= ?", p.From).
		Where("created_at < ?", p.To)
	if p.PolicyID != "" {
		inner = inner.Where("risk_policy_id = ?", p.PolicyID)
	}

	outer := sq.Select(riskFindingRevealColumns...).
		FromSelect(inner, "latest").
		Where("rn = 1").
		Where("dead_letter_reason = ''").
		Where(liveStateCond).
		Where("(created_at, id) > (?, ?)", p.CursorTime, p.CursorID).
		OrderBy("created_at ASC", "id ASC").
		Suffix("LIMIT ?", p.Limit)

	query, args, err := outer.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build policy filter sweep query: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query policy filter sweep candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanRiskFindingUnmaskRows(rows)
}

// ListRiskFindingSpansForRecall returns live message findings for one chat,
// selecting each id's newest state before applying visibility gates.
func (q *Queries) ListRiskFindingSpansForRecall(ctx context.Context, p ListRiskFindingSpansForRecallParams) ([]RiskFindingUnmaskRow, error) {
	if len(p.PolicyIDs) == 0 {
		return []RiskFindingUnmaskRow{}, nil
	}
	sb := liveRiskFindingRevealQuery(p.OrganizationID, p.ProjectID, p.ChatID, nil, p.PolicyIDs).
		Where("chat_message_id != ''")
	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build risk finding spans for recall query: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risk finding spans for recall: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanRiskFindingUnmaskRows(rows)
}
