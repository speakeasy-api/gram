package repo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

var ErrInvalidChatAnalysisScore = errors.New("invalid chat analysis score")

// ChatAnalysisJudgeWorkUnits keys work-units verdicts in the
// chat_analysis_scores sink. It is the canonical judge name shared by the
// producer (chat/analysis) and readers such as the chat service, which cannot
// import chat/analysis without an import cycle.
const ChatAnalysisJudgeWorkUnits = "work_units"

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

// ChatAnalysisVerdict is one published verdict read back from the
// chat_analysis_scores sink. Score is the verdict's headline metric and Detail
// its full JSON body; both have judge-defined meaning.
type ChatAnalysisVerdict struct {
	ChatID   string    `ch:"chat_id"`
	Score    float64   `ch:"score"`
	Detail   string    `ch:"detail"`
	ScoredAt time.Time `ch:"scored_at"`
}

// GetChatAnalysisVerdictsByChatIDsParams' filters. Named for the same reason
// as ListExistingChatAnalysisScoreIDsParams: adjacent string parameters are
// assignment-compatible, so a transposition still compiles and silently reads
// the wrong rows.
type GetChatAnalysisVerdictsByChatIDsParams struct {
	OrganizationID string
	ProjectID      string
	Judge          string
	ChatIDs        []string
}

// GetChatAnalysisVerdictsByChatIDs returns the newest published verdict per
// chat for one judge. Physical inserts are at-least-once, so rows are first
// collapsed to one coherent first-published row per evaluation id (argMin over
// (inserted_at, created_at)) before the newest verdict per chat wins.
func (q *Queries) GetChatAnalysisVerdictsByChatIDs(ctx context.Context, arg GetChatAnalysisVerdictsByChatIDsParams) (map[string]ChatAnalysisVerdict, error) {
	if len(arg.ChatIDs) == 0 {
		return map[string]ChatAnalysisVerdict{}, nil
	}

	physical := sq.Select(
		"id",
		"argMin(tuple(chat_id, score, detail, created_at), tuple(inserted_at, created_at)) AS event",
	).
		From("chat_analysis_scores").
		Where(squirrel.Eq{"organization_id": arg.OrganizationID}).
		Where(squirrel.Eq{"project_id": arg.ProjectID}).
		Where(squirrel.Eq{"judge": arg.Judge}).
		Where(squirrel.Eq{"chat_id": arg.ChatIDs}).
		GroupBy("id")
	physicalSQL, args, err := physical.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building deduplicated chat analysis verdicts query: %w", err)
	}

	query := `
		SELECT
			chat_id,
			tupleElement(verdict, 1) AS score,
			tupleElement(verdict, 2) AS detail,
			tupleElement(verdict, 3) AS scored_at
		FROM (
			SELECT
				tupleElement(event, 1) AS chat_id,
				argMax(
					tuple(tupleElement(event, 2), tupleElement(event, 3), tupleElement(event, 4)),
					tuple(tupleElement(event, 4), id)
				) AS verdict
			FROM (` + physicalSQL + `)
			GROUP BY chat_id
		)`

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying chat analysis verdicts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]ChatAnalysisVerdict, len(arg.ChatIDs))
	for rows.Next() {
		var row ChatAnalysisVerdict
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning chat analysis verdict: %w", err)
		}
		result[row.ChatID] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating chat analysis verdicts: %w", err)
	}
	return result, nil
}

// ListChatAnalysisVerdictsParams' filters. Named for the same
// transposition-safety reason as the other read params in this file.
type ListChatAnalysisVerdictsParams struct {
	OrganizationID string
	ProjectID      string
	Judge          string
	From           time.Time
	To             time.Time
	Limit          uint64
}

// ListChatAnalysisVerdicts returns the newest published verdict per chat for
// one judge across a scoring-time window, oldest first. Deduplication follows
// GetChatAnalysisVerdictsByChatIDs: one coherent first-published row per
// evaluation id, then the newest verdict per chat wins.
func (q *Queries) ListChatAnalysisVerdicts(ctx context.Context, arg ListChatAnalysisVerdictsParams) ([]ChatAnalysisVerdict, error) {
	if arg.OrganizationID == "" || arg.ProjectID == "" || arg.Judge == "" || !arg.From.Before(arg.To) || arg.Limit == 0 || arg.Limit > 10000 {
		return nil, fmt.Errorf("list chat analysis verdicts: invalid scope, window, or limit")
	}

	physical := sq.Select(
		"id",
		"argMin(tuple(chat_id, score, detail, created_at), tuple(inserted_at, created_at)) AS event",
	).
		From("chat_analysis_scores").
		Where(squirrel.Eq{"organization_id": arg.OrganizationID}).
		Where(squirrel.Eq{"project_id": arg.ProjectID}).
		Where(squirrel.Eq{"judge": arg.Judge}).
		Where("created_at >= ?", arg.From).
		Where("created_at <= ?", arg.To).
		GroupBy("id")
	physicalSQL, args, err := physical.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building deduplicated chat analysis verdict window query: %w", err)
	}

	query := `
		SELECT
			chat_id,
			tupleElement(verdict, 1) AS score,
			tupleElement(verdict, 2) AS detail,
			tupleElement(verdict, 3) AS scored_at
		FROM (
			SELECT
				tupleElement(event, 1) AS chat_id,
				argMax(
					tuple(tupleElement(event, 2), tupleElement(event, 3), tupleElement(event, 4)),
					tuple(tupleElement(event, 4), id)
				) AS verdict
			FROM (` + physicalSQL + `)
			GROUP BY chat_id
		)
		ORDER BY scored_at ASC
		LIMIT ` + strconv.FormatUint(arg.Limit, 10)

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying chat analysis verdict window: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []ChatAnalysisVerdict
	for rows.Next() {
		var row ChatAnalysisVerdict
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning chat analysis verdict window row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating chat analysis verdict window: %w", err)
	}
	return result, nil
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
