package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

var ErrInvalidChatAnalysisScore = errors.New("invalid chat analysis score")

// ChatAnalysisScore is one judged session verdict bound for the
// chat_analysis_scores sink. Score is the verdict's headline metric and Detail
// its full JSON body; both have judge-defined meaning, keyed by Judge.
type ChatAnalysisScore struct {
	ID                 uuid.UUID
	CreatedAt          time.Time
	OrganizationID     string
	ProjectID          string
	ChatID             string
	Judge              string
	Score              float64
	Detail             string
	JudgeModel         string
	JudgePromptVersion string
}

// InsertChatAnalysisScores writes judged chat analysis verdicts. The insert is
// fully synchronous because publication marks the evaluation scored right
// afterwards and a retry re-reads the row through the existence guard: a
// server-side async buffer would acknowledge rows the guard cannot see yet.
func (q *Queries) InsertChatAnalysisScores(ctx context.Context, rows []ChatAnalysisScore) error {
	if len(rows) == 0 {
		return nil
	}

	builder := sq.Insert("chat_analysis_scores").Columns(
		"id",
		"created_at",
		"organization_id",
		"project_id",
		"chat_id",
		"judge",
		"score",
		"detail",
		"judge_model",
		"judge_prompt_version",
	)
	for _, row := range rows {
		builder = builder.Values(
			row.ID,
			row.CreatedAt,
			row.OrganizationID,
			row.ProjectID,
			row.ChatID,
			row.Judge,
			row.Score,
			row.Detail,
			row.JudgeModel,
			row.JudgePromptVersion,
		)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("building chat analysis score insert: %w", err)
	}
	if err := q.conn.Exec(ctx, query, args...); err != nil {
		var exception *clickhouse.Exception
		if errors.As(err, &exception) && exception.Code == int32(proto.ErrViolatedConstraint) {
			return fmt.Errorf("%w: %w", ErrInvalidChatAnalysisScore, err)
		}
		return fmt.Errorf("inserting chat analysis scores: %w", err)
	}
	return nil
}

// ListExistingChatAnalysisScoreIDsParams' filters. Named rather than positional
// because the query takes bare strings, string slices and two timestamps:
// adjacent pairs are assignment-compatible, so a transposition still compiles
// and silently reads the wrong rows — and this read is the dedup guard, where a
// wrong answer either re-pays for inference or suppresses a score that was
// never written.
type ListExistingChatAnalysisScoreIDsParams struct {
	OrganizationID string
	ProjectID      string
	Judges         []string
	IDs            []string
	MinCreatedAt   time.Time
	MaxCreatedAt   time.Time
}

// ListExistingChatAnalysisScoreIDs returns which of the given score ids are
// already published — the publication dedup guard. The filters follow the
// table's ORDER BY prefix — organization_id, project_id, judge — before
// narrowing on created_at and id, so the leading key columns are all pinned to
// the batch's exact values. The read demands sequential consistency so that on
// replicated setups a retry cannot miss an insert acknowledged by another
// replica moments earlier.
func (q *Queries) ListExistingChatAnalysisScoreIDs(ctx context.Context, arg ListExistingChatAnalysisScoreIDsParams) ([]string, error) {
	if len(arg.IDs) == 0 || len(arg.Judges) == 0 {
		return nil, nil
	}

	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"select_sequential_consistency": 1,
	}))

	sb := sq.Select("toString(id) AS id").
		From("chat_analysis_scores").
		Where(squirrel.Eq{"organization_id": arg.OrganizationID}).
		Where(squirrel.Eq{"project_id": arg.ProjectID}).
		Where(squirrel.Eq{"judge": arg.Judges}).
		Where("created_at >= ?", arg.MinCreatedAt).
		Where("created_at <= ?", arg.MaxCreatedAt).
		Where(squirrel.Eq{"id": arg.IDs})

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building existing chat analysis score ids query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying existing chat analysis score ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning existing chat analysis score id: %w", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating existing chat analysis score ids: %w", err)
	}
	return result, nil
}
