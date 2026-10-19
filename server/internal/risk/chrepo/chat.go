package chrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/Masterminds/squirrel"
)

var errEmptyChatPolicyIDs = errors.New("chat findings query requires at least one policy id")

// ChatFindingParams scopes live chat finding reads.
type ChatFindingParams struct {
	// OrganizationID scopes the read to one tenant.
	OrganizationID string
	// ProjectID scopes the read to one project.
	ProjectID string
	// PolicyIDs limits findings to policies currently eligible in Postgres.
	PolicyIDs []string
	// ChatIDs optionally limits counts or anchors to known chats.
	ChatIDs []string
	// MinCount keeps only chats with at least this many live findings. Zero means no threshold.
	MinCount uint64
	// Limit bounds grouped count rows. Zero means no explicit limit.
	Limit uint64
}

// ChatFindingCount is one chat's live finding count.
type ChatFindingCount struct {
	// ChatID identifies the grouped chat.
	ChatID string
	// Count is the number of live findings.
	Count uint64
}

// ChatFindingAnchor identifies the chat record that contains a live finding.
type ChatFindingAnchor struct {
	// ChatMessageID identifies a risky message when present.
	ChatMessageID string
	// ContentPartID identifies a risky attachment when present.
	ContentPartID string
}

func chatFindingsLatest(p ChatFindingParams, columns ...string) (squirrel.SelectBuilder, error) {
	if len(p.PolicyIDs) == 0 {
		return squirrel.SelectBuilder{}, errEmptyChatPolicyIDs
	}

	innerColumns := append([]string{"id"}, columns...)
	innerColumns = append(innerColumns, "excluded_at", "false_positive_at")
	inner := sq.Select(innerColumns...).
		From("risk_findings").
		Where("organization_id = ?", p.OrganizationID).
		Where("project_id = ?", p.ProjectID).
		Where("dead_letter_reason = ''").
		Where(notShadowCond).
		Where(squirrel.Eq{"risk_policy_id": p.PolicyIDs})
	if len(p.ChatIDs) > 0 {
		inner = inner.Where(squirrel.Eq{"chat_id": p.ChatIDs})
	}
	inner = inner.OrderBy(latestCopyOrderSQL).Suffix("LIMIT 1 BY id")

	return sq.Select(columns...).FromSelect(inner, "latest").Where(liveStateCond), nil
}

// ListChatFindingCounts returns live finding counts grouped by chat.
func (q *Queries) ListChatFindingCounts(ctx context.Context, p ChatFindingParams) ([]ChatFindingCount, error) {
	sb, err := chatFindingsLatest(p, "chat_id")
	if err != nil {
		return nil, err
	}
	sb = sb.Where("chat_id != ''").Column("count() AS findings").GroupBy("chat_id").OrderBy("chat_id")
	if p.MinCount > 0 {
		sb = sb.Having("findings >= ?", p.MinCount)
	}
	if p.Limit > 0 {
		sb = sb.Limit(p.Limit)
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build chat finding counts query: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query chat finding counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ChatFindingCount
	for rows.Next() {
		var row ChatFindingCount
		if err := rows.Scan(&row.ChatID, &row.Count); err != nil {
			return nil, fmt.Errorf("scan chat finding count: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read chat finding counts: %w", err)
	}
	return out, nil
}

// ListChatFindingAnchors returns distinct message and content part anchors for one chat.
func (q *Queries) ListChatFindingAnchors(ctx context.Context, p ChatFindingParams) ([]ChatFindingAnchor, error) {
	if len(p.ChatIDs) != 1 {
		return nil, errors.New("chat finding anchors require exactly one chat id")
	}
	sb, err := chatFindingsLatest(p, "chat_message_id", "content_part_id")
	if err != nil {
		return nil, err
	}
	sb = sb.Where("chat_message_id != '' OR content_part_id != ''").GroupBy("chat_message_id", "content_part_id")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build chat finding anchors query: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query chat finding anchors: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ChatFindingAnchor
	for rows.Next() {
		var row ChatFindingAnchor
		if err := rows.Scan(&row.ChatMessageID, &row.ContentPartID); err != nil {
			return nil, fmt.Errorf("scan chat finding anchor: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read chat finding anchors: %w", err)
	}
	return out, nil
}
