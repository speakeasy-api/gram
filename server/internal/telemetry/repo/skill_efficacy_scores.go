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

var ErrInvalidSkillEfficacyScore = errors.New("invalid skill efficacy score")

type SkillEfficacyScore struct {
	ID                 uuid.UUID
	CreatedAt          time.Time
	OrganizationID     string
	ProjectID          string
	SessionID          string
	SkillID            uuid.UUID
	SkillVersionID     uuid.UUID
	CanonicalSHA256    string
	Surface            string
	TraceID            *string
	GramChatID         string
	Score              float64
	Rationale          string
	EstTurnsSaved      *float64
	EstMinutesSaved    *float64
	ROIConfidence      *string
	Flags              []string
	JudgeModel         string
	JudgePromptVersion string
}

// InsertSkillEfficacyScores writes judged skill efficacy scores. The insert is
// fully synchronous because publication marks the evaluation scored right
// afterwards and a retry re-reads the row through the existence guard: a
// server-side async buffer would acknowledge rows the guard cannot see yet.
// Synchronous means passing no async option at all — clickhouse.WithAsync(false)
// is NOT a synchronous insert, it sets async_insert=1 with
// wait_for_async_insert=0 (fire and forget).
func (q *Queries) InsertSkillEfficacyScores(ctx context.Context, rows []SkillEfficacyScore) error {
	if len(rows) == 0 {
		return nil
	}

	builder := sq.Insert("skill_efficacy_scores").Columns(
		"id",
		"created_at",
		"organization_id",
		"project_id",
		"session_id",
		"skill_id",
		"skill_version_id",
		"canonical_sha256",
		"surface",
		"trace_id",
		"gram_chat_id",
		"score",
		"rationale",
		"est_turns_saved",
		"est_minutes_saved",
		"roi_confidence",
		"flags",
		"judge_model",
		"judge_prompt_version",
	)
	for _, row := range rows {
		flags := row.Flags
		if flags == nil {
			flags = []string{}
		}

		builder = builder.Values(
			row.ID,
			row.CreatedAt,
			row.OrganizationID,
			row.ProjectID,
			row.SessionID,
			row.SkillID,
			row.SkillVersionID,
			row.CanonicalSHA256,
			row.Surface,
			row.TraceID,
			row.GramChatID,
			row.Score,
			row.Rationale,
			row.EstTurnsSaved,
			row.EstMinutesSaved,
			row.ROIConfidence,
			flags,
			row.JudgeModel,
			row.JudgePromptVersion,
		)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("building skill efficacy score insert: %w", err)
	}
	if err := q.conn.Exec(ctx, query, args...); err != nil {
		var exception *clickhouse.Exception
		if errors.As(err, &exception) && exception.Code == int32(proto.ErrViolatedConstraint) {
			return fmt.Errorf("%w: %w", ErrInvalidSkillEfficacyScore, err)
		}
		return fmt.Errorf("inserting skill efficacy scores: %w", err)
	}
	return nil
}

// ListExistingSkillEfficacyScoreIDs' filters. Named rather than positional
// because the query takes two bare strings, three string slices and two
// timestamps: every adjacent pair is assignment-compatible, so a transposition
// still compiles and silently reads the wrong rows — and this read is the
// dedup guard, where a wrong answer either re-pays for inference or suppresses
// a score that was never written.
type ListExistingSkillEfficacyScoreIDsParams struct {
	OrganizationID  string
	ProjectID       string
	SkillIDs        []string
	SkillVersionIDs []string
	IDs             []string
	MinCreatedAt    time.Time
	MaxCreatedAt    time.Time
}

// ListExistingSkillEfficacyScoreIDs returns which of the given score ids are
// already published — the publication dedup guard. The filters follow the
// table's ORDER BY prefix — organization_id, project_id, skill_id,
// skill_version_id — before narrowing on created_at and id, so the four leading
// key columns are all pinned to the batch's exact values and the scan reads
// index granules for that skill version alone.
//
// The window bounds are the caller's: the lower bound is the batch's earliest
// evaluation created_at, immutable and therefore the same on every pass, while
// the upper bound tracks the current pass because created_at is stamped at
// insert time — a score written by a pass that crashed before marking must still
// fall inside the window a later retry reads. The read demands sequential
// consistency so that on replicated setups a retry cannot miss an insert
// acknowledged by another replica moments earlier.
func (q *Queries) ListExistingSkillEfficacyScoreIDs(ctx context.Context, arg ListExistingSkillEfficacyScoreIDsParams) ([]string, error) {
	if len(arg.IDs) == 0 || len(arg.SkillIDs) == 0 || len(arg.SkillVersionIDs) == 0 {
		return nil, nil
	}

	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"select_sequential_consistency": 1,
	}))

	sb := sq.Select("toString(id) AS id").
		From("skill_efficacy_scores").
		Where(squirrel.Eq{"organization_id": arg.OrganizationID}).
		Where(squirrel.Eq{"project_id": arg.ProjectID}).
		Where(squirrel.Eq{"skill_id": arg.SkillIDs}).
		Where(squirrel.Eq{"skill_version_id": arg.SkillVersionIDs}).
		Where("created_at >= ?", arg.MinCreatedAt).
		Where("created_at <= ?", arg.MaxCreatedAt).
		Where(squirrel.Eq{"id": arg.IDs})

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building existing skill efficacy score ids query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying existing skill efficacy score ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning existing skill efficacy score id: %w", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating existing skill efficacy score ids: %w", err)
	}
	return result, nil
}
