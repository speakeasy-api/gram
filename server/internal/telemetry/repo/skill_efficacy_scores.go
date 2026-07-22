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

type QuerySkillInsightsParams struct {
	OrganizationID  string
	ProjectID       string
	SkillIDs        []string
	SkillVersionIDs []string
	From            time.Time
	To              time.Time
	IntervalSeconds int64
}

type ListSkillEfficacyScoreSessionsParams struct {
	OrganizationID string
	ProjectID      string
	SkillIDs       []string
	From           time.Time
	To             time.Time
	Limit          uint64
}

type SkillEfficacyScoreSession struct {
	ID                    string    `ch:"id"`
	SkillID               string    `ch:"skill_id"`
	SkillVersionID        string    `ch:"skill_version_id"`
	Surface               string    `ch:"surface"`
	ActivatedAt           time.Time `ch:"activated_at"`
	ScoredAt              time.Time `ch:"scored_at"`
	Score                 float64   `ch:"score"`
	Rationale             string    `ch:"rationale"`
	EstimatedTurnsSaved   *float64  `ch:"estimated_turns_saved"`
	EstimatedMinutesSaved *float64  `ch:"estimated_minutes_saved"`
	ROIConfidence         *string   `ch:"roi_confidence"`
	Flags                 []string  `ch:"flags"`
	GramChatID            string    `ch:"gram_chat_id"`
}

func (q *Queries) ListSkillEfficacyScoreSessions(ctx context.Context, arg ListSkillEfficacyScoreSessionsParams) ([]SkillEfficacyScoreSession, error) {
	if arg.OrganizationID == "" || arg.ProjectID == "" || len(arg.SkillIDs) == 0 || !arg.From.Before(arg.To) || arg.Limit == 0 || arg.Limit > 100 {
		return nil, fmt.Errorf("list skill efficacy score sessions: invalid scope, window, or limit")
	}
	mappings := sq.Select(
		"toString(project_id) AS project_id", "session_id", "surface",
		"toString(skill_id) AS skill_id", "toString(skill_version_id) AS skill_version_id",
		"max(seen_at) AS activated_at",
	).
		From("skill_session_versions").
		Where(squirrel.Eq{"organization_id": arg.OrganizationID}).
		Where(squirrel.Eq{"project_id": arg.ProjectID}).
		Where(squirrel.Eq{"toString(skill_id)": arg.SkillIDs}).
		Where("seen_at >= ?", arg.From).
		Where("seen_at <= ?", arg.To).
		GroupBy("project_id", "session_id", "surface", "skill_id", "skill_version_id")
	mappingSQL, mappingArgs, err := mappings.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building scored session mappings query: %w", err)
	}
	query, args, err := sq.Select(
		"toString(e.id) AS id", "toString(e.skill_id) AS skill_id", "toString(e.skill_version_id) AS skill_version_id",
		"e.surface AS surface", "m.activated_at AS activated_at", "e.created_at AS scored_at", "e.score AS score",
		"e.rationale AS rationale", "e.est_turns_saved AS estimated_turns_saved", "e.est_minutes_saved AS estimated_minutes_saved",
		"e.roi_confidence AS roi_confidence", "e.flags AS flags", "e.gram_chat_id AS gram_chat_id",
	).
		From("skill_efficacy_scores e").
		Join("mappings m ON m.project_id = e.project_id AND m.session_id = e.session_id AND m.surface = e.surface AND m.skill_id = toString(e.skill_id) AND m.skill_version_id = toString(e.skill_version_id)").
		Where(squirrel.Eq{"e.organization_id": arg.OrganizationID}).
		Where(squirrel.Eq{"e.project_id": arg.ProjectID}).
		Where(squirrel.Eq{"toString(e.skill_id)": arg.SkillIDs}).
		OrderBy("e.created_at DESC", "e.id DESC").
		Limit(arg.Limit).
		Prefix("WITH mappings AS ("+mappingSQL+")", mappingArgs...).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building scored sessions query: %w", err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying scored sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var result []SkillEfficacyScoreSession
	for rows.Next() {
		var row SkillEfficacyScoreSession
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning scored session: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// SkillInsightBucket is one activation-time bucket for a skill version. Score
// and ROI fields are sums plus sample counts so callers can combine buckets
// without averaging averages.
type SkillInsightBucket struct {
	SkillID                  string  `ch:"skill_id"`
	SkillVersionID           string  `ch:"skill_version_id"`
	BucketTimeUnixNano       int64   `ch:"bucket_time_unix_nano"`
	ActivationCount          uint64  `ch:"activation_count"`
	ActivatedSessions        uint64  `ch:"activated_sessions"`
	TotalSessionCost         float64 `ch:"total_session_cost"`
	ScoredSessions           uint64  `ch:"scored_sessions"`
	ScoreSum                 float64 `ch:"score_sum"`
	EstimatedTurnsSavedSum   float64 `ch:"estimated_turns_saved_sum"`
	EstimatedTurnsSamples    uint64  `ch:"estimated_turns_samples"`
	EstimatedMinutesSavedSum float64 `ch:"estimated_minutes_saved_sum"`
	EstimatedMinutesSamples  uint64  `ch:"estimated_minutes_samples"`
	ROIConfidenceLow         uint64  `ch:"roi_confidence_low"`
	ROIConfidenceMed         uint64  `ch:"roi_confidence_med"`
	ROIConfidenceHigh        uint64  `ch:"roi_confidence_high"`
	IgnoredCount             uint64  `ch:"ignored_count"`
	MisappliedCount          uint64  `ch:"misapplied_count"`
	PartiallyFollowedCount   uint64  `ch:"partially_followed_count"`
	HarmfulCount             uint64  `ch:"harmful_count"`
}

// QuerySkillInsights joins exact activation mappings to session-grained usage
// and sampled efficacy. The requested window is activation-time based; costs
// include usage rows in the same window and fan out to every activated version,
// matching telemetry.query's skill_version attribution semantics.
func (q *Queries) QuerySkillInsights(ctx context.Context, arg QuerySkillInsightsParams) ([]SkillInsightBucket, error) {
	if arg.OrganizationID == "" || arg.ProjectID == "" || !arg.From.Before(arg.To) || arg.IntervalSeconds <= 0 {
		return nil, fmt.Errorf("query skill insights: invalid scope, window, or interval")
	}

	sessions := sq.Select(
		"toString(gram_project_id) AS project_id",
		"chat_id AS session_id",
		"if("+skillVersionAssistantUsagePredicate+", 'assistant', 'dev') AS surface",
		"countIf("+skillVersionUsageMeasureFilter+") > 0 AS has_usage",
		skillVersionCostExpr+" AS total_cost",
	).
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": []string{arg.ProjectID}}).
		Where("time_unix_nano >= ?", arg.From.UnixNano()).
		Where("time_unix_nano <= ?", arg.To.UnixNano()).
		Where(skillVersionSourceRowPredicate).
		Where("chat_id != ''").
		GroupBy("gram_project_id", "chat_id", "surface")
	sessionSQL, sessionArgs, err := sessions.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building skill insight sessions query: %w", err)
	}

	mappings := sq.Select(
		"toString(project_id) AS project_id",
		"session_id",
		"surface",
		"toString(skill_id) AS skill_id",
		"toString(skill_version_id) AS skill_version_id",
		"max(seen_at) AS observed_at",
		"uniqExact(id) AS activation_count",
	).
		From("skill_session_versions").
		Where(squirrel.Eq{"organization_id": arg.OrganizationID}).
		Where(squirrel.Eq{"project_id": arg.ProjectID}).
		Where("seen_at >= ?", arg.From).
		Where("seen_at <= ?", arg.To)
	if len(arg.SkillIDs) > 0 {
		mappings = mappings.Where(squirrel.Eq{"toString(skill_id)": arg.SkillIDs})
	}
	if len(arg.SkillVersionIDs) > 0 {
		mappings = mappings.Where(squirrel.Eq{"toString(skill_version_id)": arg.SkillVersionIDs})
	}
	mappings = mappings.GroupBy("project_id", "session_id", "surface", "skill_id", "skill_version_id")
	mappingSQL, mappingArgs, err := mappings.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building skill insight mappings query: %w", err)
	}

	scores := sq.Select(
		"project_id",
		"session_id",
		"surface",
		"toString(skill_id) AS skill_id",
		"toString(skill_version_id) AS skill_version_id",
		"count() AS score_count",
		"sum(score) AS score_sum",
		"sum(ifNull(est_turns_saved, 0)) AS turns_sum",
		"countIf(est_turns_saved IS NOT NULL) AS turns_count",
		"sum(ifNull(est_minutes_saved, 0)) AS minutes_sum",
		"countIf(est_minutes_saved IS NOT NULL) AS minutes_count",
		"countIf(roi_confidence = 'low') AS confidence_low",
		"countIf(roi_confidence = 'med') AS confidence_med",
		"countIf(roi_confidence = 'high') AS confidence_high",
		"countIf(has(flags, 'ignored')) AS ignored_count",
		"countIf(has(flags, 'misapplied')) AS misapplied_count",
		"countIf(has(flags, 'partially_followed')) AS partially_followed_count",
		"countIf(has(flags, 'harmful')) AS harmful_count",
	).
		From("skill_efficacy_scores").
		Where(squirrel.Eq{"organization_id": arg.OrganizationID}).
		Where(squirrel.Eq{"project_id": arg.ProjectID})
	if len(arg.SkillIDs) > 0 {
		scores = scores.Where(squirrel.Eq{"toString(skill_id)": arg.SkillIDs})
	}
	if len(arg.SkillVersionIDs) > 0 {
		scores = scores.Where(squirrel.Eq{"toString(skill_version_id)": arg.SkillVersionIDs})
	}
	scores = scores.GroupBy("project_id", "session_id", "surface", "skill_id", "skill_version_id")
	scoreSQL, scoreArgs, err := scores.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building skill insight scores query: %w", err)
	}

	outer := sq.Select(
		"m.skill_id AS skill_id",
		"m.skill_version_id AS skill_version_id",
	).
		Column(squirrel.Expr("toInt64(toStartOfInterval(m.observed_at, toIntervalSecond(?))) * 1000000000 AS bucket_time_unix_nano", arg.IntervalSeconds)).
		Columns(
			"sum(m.activation_count) AS activation_count",
			"count() AS activated_sessions",
			"sum(if(s.has_usage, s.total_cost, 0)) AS total_session_cost",
			"sum(ifNull(e.score_count, 0)) AS scored_sessions",
			"sum(ifNull(e.score_sum, 0)) AS score_sum",
			"sum(ifNull(e.turns_sum, 0)) AS estimated_turns_saved_sum",
			"sum(ifNull(e.turns_count, 0)) AS estimated_turns_samples",
			"sum(ifNull(e.minutes_sum, 0)) AS estimated_minutes_saved_sum",
			"sum(ifNull(e.minutes_count, 0)) AS estimated_minutes_samples",
			"sum(ifNull(e.confidence_low, 0)) AS roi_confidence_low",
			"sum(ifNull(e.confidence_med, 0)) AS roi_confidence_med",
			"sum(ifNull(e.confidence_high, 0)) AS roi_confidence_high",
			"sum(ifNull(e.ignored_count, 0)) AS ignored_count",
			"sum(ifNull(e.misapplied_count, 0)) AS misapplied_count",
			"sum(ifNull(e.partially_followed_count, 0)) AS partially_followed_count",
			"sum(ifNull(e.harmful_count, 0)) AS harmful_count",
		).
		From("mappings m").
		LeftJoin("sessions s ON s.project_id = m.project_id AND s.session_id = m.session_id AND s.surface = m.surface").
		LeftJoin("scores e ON e.project_id = m.project_id AND e.session_id = m.session_id AND e.surface = m.surface AND e.skill_id = m.skill_id AND e.skill_version_id = m.skill_version_id").
		GroupBy("m.skill_id", "m.skill_version_id", "bucket_time_unix_nano").
		OrderBy("bucket_time_unix_nano ASC", "m.skill_id ASC", "m.skill_version_id ASC").
		Prefix("WITH sessions AS ("+sessionSQL+"), mappings AS ("+mappingSQL+"), scores AS ("+scoreSQL+")", append(append(sessionArgs, mappingArgs...), scoreArgs...)...)
	query, args, err := outer.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building skill insights query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying skill insights: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []SkillInsightBucket
	for rows.Next() {
		var row SkillInsightBucket
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning skill insight bucket: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating skill insight buckets: %w", err)
	}
	return result, nil
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
