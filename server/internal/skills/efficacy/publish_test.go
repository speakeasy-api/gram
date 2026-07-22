package efficacy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// stubJudge answers per surface, so one batch can mix a session the model
// answers for with one it fails on.
type stubJudge struct {
	results map[string]JudgeResult
	errs    map[string]error
	calls   map[string]int
	inputs  []JudgeInput
}

func newStubJudge() *stubJudge {
	return &stubJudge{
		results: map[string]JudgeResult{},
		errs:    map[string]error{},
		calls:   map[string]int{},
		inputs:  nil,
	}
}

func (s *stubJudge) Judge(_ context.Context, in JudgeInput) (JudgeResult, error) {
	s.calls[in.Surface]++
	s.inputs = append(s.inputs, in)
	if err, ok := s.errs[in.Surface]; ok {
		return JudgeResult{}, err
	}

	return s.results[in.Surface], nil
}

func okVerdict() JudgeResult {
	confidence := "high"
	turns := 2.0

	return JudgeResult{
		Verdict: Verdict{
			Score:           0.8,
			Rationale:       "the agent followed the skill and skipped two dead ends",
			EstTurnsSaved:   &turns,
			EstMinutesSaved: nil,
			ROIConfidence:   &confidence,
			Flags:           []string{"partially_followed"},
		},
		Model:         "test-judge-model",
		PromptVersion: JudgePromptVersion,
	}
}

// failingSink delegates the guard read and fails the insert, standing in for a
// ClickHouse outage.
type failingSink struct {
	ScoreSink
	err error
}

func (f failingSink) InsertSkillEfficacyScores(context.Context, []telemetryrepo.SkillEfficacyScore) error {
	return f.err
}

type invalidDevSink struct {
	ScoreSink
}

func (s invalidDevSink) InsertSkillEfficacyScores(ctx context.Context, rows []telemetryrepo.SkillEfficacyScore) error {
	if rows[0].Surface == SurfaceDev {
		return fmt.Errorf("invalid dev score: %w", telemetryrepo.ErrInvalidSkillEfficacyScore)
	}
	if err := s.ScoreSink.InsertSkillEfficacyScores(ctx, rows); err != nil {
		return fmt.Errorf("insert valid score: %w", err)
	}
	return nil
}

// crashingSink commits the score for real and then kills the pass's context, so
// the mark that follows the insert fails exactly the way a crashed worker leaves
// the row: score in ClickHouse, evaluation still reserved.
type crashingSink struct {
	ScoreSink
	cancel context.CancelFunc
}

func (c crashingSink) InsertSkillEfficacyScores(ctx context.Context, rows []telemetryrepo.SkillEfficacyScore) error {
	if err := c.ScoreSink.InsertSkillEfficacyScores(ctx, rows); err != nil {
		return fmt.Errorf("crashing sink insert: %w", err)
	}
	c.cancel()

	return nil
}

// stealingJudge answers as its delegate does, and first returns the named
// evaluation to pending — which is what a stale-reservation reset does to a row
// this pass is still working through.
type stealingJudge struct {
	JudgeClient
	db           *pgxpool.Pool
	projectID    uuid.UUID
	evaluationID uuid.UUID
	surface      string
}

func (s stealingJudge) Judge(ctx context.Context, in JudgeInput) (JudgeResult, error) {
	if in.Surface != s.surface {
		result, err := s.JudgeClient.Judge(ctx, in)
		if err != nil {
			return JudgeResult{}, fmt.Errorf("stealing judge delegate: %w", err)
		}
		return result, nil
	}

	reset, err := testrepo.New(s.db).ResetSkillEfficacyReservationFixture(ctx, testrepo.ResetSkillEfficacyReservationFixtureParams{
		ProjectID: s.projectID,
		ID:        s.evaluationID,
	})
	if err != nil {
		return JudgeResult{}, fmt.Errorf("stealing judge reset: %w", err)
	}
	if reset != 1 {
		return JudgeResult{}, fmt.Errorf("stealing judge reset: reset %d evaluations", reset)
	}

	result, err := s.JudgeClient.Judge(ctx, in)
	if err != nil {
		return JudgeResult{}, fmt.Errorf("stealing judge delegate: %w", err)
	}
	return result, nil
}

// blockingJudge never answers on its own, standing in for a provider call that
// hangs rather than failing.
type blockingJudge struct{}

func (blockingJudge) Judge(ctx context.Context, _ JudgeInput) (JudgeResult, error) {
	<-ctx.Done()

	return JudgeResult{}, fmt.Errorf("blocking judge: %w", ctx.Err())
}

// blockingSink hangs the insert, which is the same hazard one step later: the
// judge has already been paid for and the batch is still holding its claim.
type blockingSink struct {
	ScoreSink
}

func (blockingSink) InsertSkillEfficacyScores(ctx context.Context, _ []telemetryrepo.SkillEfficacyScore) error {
	<-ctx.Done()

	return fmt.Errorf("blocking sink insert: %w", ctx.Err())
}

// countingChats records every page read publication makes, which is how a
// transcript shared by two evaluations shows up as a single read, and how a
// long chat shows up as a bounded number of pages instead of one whole-chat
// load.
type countingChats struct {
	TranscriptSource
	loads  map[uuid.UUID]int
	counts map[uuid.UUID]int
	// rows is every message the source handed back across all pages, which is
	// the peak the loader would have held had it read the chat whole.
	rows int
}

func (c *countingChats) CountChatMessages(ctx context.Context, arg chatrepo.CountChatMessagesParams) (int64, error) {
	c.counts[arg.ChatID]++
	count, err := c.TranscriptSource.CountChatMessages(ctx, arg)
	if err != nil {
		return 0, fmt.Errorf("counting chats count transcript: %w", err)
	}

	return count, nil
}

func (c *countingChats) ListChatTranscriptMessagesPage(ctx context.Context, arg chatrepo.ListChatTranscriptMessagesPageParams) ([]chatrepo.ListChatTranscriptMessagesPageRow, error) {
	c.loads[arg.ChatID]++
	rows, err := c.TranscriptSource.ListChatTranscriptMessagesPage(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("counting chats list transcript page: %w", err)
	}
	c.rows += len(rows)

	return rows, nil
}

// flakyChats fails its first read and serves every later one, so a pass holding
// two evaluations of one chat reveals whether the failure was cached.
type flakyChats struct {
	TranscriptSource
	loads int
}

func (f *flakyChats) ListChatTranscriptMessagesPage(ctx context.Context, arg chatrepo.ListChatTranscriptMessagesPageParams) ([]chatrepo.ListChatTranscriptMessagesPageRow, error) {
	f.loads++
	if f.loads == 1 {
		return nil, errors.New("chat read unavailable")
	}

	rows, err := f.TranscriptSource.ListChatTranscriptMessagesPage(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("flaky chats list transcript page: %w", err)
	}

	return rows, nil
}

type publishHarness struct {
	fixture efficacyFixture
	scores  *telemetryrepo.Queries
	judge   *stubJudge
}

func newPublishHarness(t *testing.T, name string) publishHarness {
	t.Helper()

	fixture := newEfficacyFixture(t, name)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	return publishHarness{fixture: fixture, scores: telemetryrepo.New(conn), judge: newStubJudge()}
}

func (h publishHarness) publisher(t *testing.T, scores ScoreSink) *Publisher {
	t.Helper()

	return NewPublisher(testenv.NewLogger(t), testenv.NewTracerProvider(t), h.fixture.db, scores, h.judge)
}

// today is the reservation day every fixture row spends on unless a test
// backdates it, and the day the guard window's lower bound is derived from.
func today() pgtype.Date {
	return daysAgo(0)
}

// daysAgo is a reservation day offset from today, which is how a test reaches a
// reservation older than guardWindowSlack, or — with a negative offset — one
// taken on a later day than a score already in the sink.
func daysAgo(days int) pgtype.Date {
	day := time.Now().UTC().AddDate(0, 0, -days).Truncate(24 * time.Hour)

	return pgtype.Date{Time: day, InfinityModifier: pgtype.Finite, Valid: true}
}

// reserve enqueues one quiet session's activation and takes it through the
// reservation transition, which is what publication consumes. provider picks the
// surface, which is how a test tells the stub judge one session from another.
func (h publishHarness) reserve(t *testing.T, sessionID string, provider string) repo.SkillEfficacyEvaluation {
	t.Helper()

	return h.reserveOn(t, sessionID, provider, today())
}

// reserveOn is reserve with an explicit reservation day.
func (h publishHarness) reserveOn(t *testing.T, sessionID string, provider string, reservedOn pgtype.Date) repo.SkillEfficacyEvaluation {
	t.Helper()
	ctx := t.Context()

	h.fixture.seedChat(t, sessionID, 2, 90*time.Minute)
	h.fixture.observe(t, sessionID, provider, time.Now().UTC().Add(-2*time.Hour))

	_, err := EnqueuePage(ctx, h.fixture.db, &stubFeatures{enabled: true}, h.fixture.projectID, EnqueueCursor{}, 50)
	require.NoError(t, err)

	var target repo.SkillEfficacyEvaluation
	for _, row := range h.fixture.pendingEvaluations(t) {
		if row.SessionID == sessionID {
			target = row
		}
	}
	require.NotEqual(t, uuid.Nil, target.ID, "session must be enqueued")

	reserved, err := repo.New(h.fixture.db).ReserveSkillEfficacyEvaluations(ctx, repo.ReserveSkillEfficacyEvaluationsParams{
		ReservedOn: reservedOn,
		ProjectID:  h.fixture.projectID,
		Ids:        []uuid.UUID{target.ID},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), reserved)

	return target
}

// reserveSharedChat reserves the two evaluations one session produces when it
// is seen on both surfaces: distinct scoring units bound to the same chat, which
// is the batch shape the per-chat transcript cache exists for. A UUID session id
// resolves straight to the chat id, which is what the assistant surface requires.
func (h publishHarness) reserveSharedChat(t *testing.T, sessionID string) (repo.SkillEfficacyEvaluation, repo.SkillEfficacyEvaluation) {
	t.Helper()
	ctx := t.Context()

	seenAt := time.Now().UTC().Add(-2 * time.Hour)
	h.fixture.seedChat(t, sessionID, 2, 90*time.Minute)
	h.fixture.observe(t, sessionID, "claude-code", seenAt)
	h.fixture.observeAs(t, sessionID, "assistants", actor{userID: "", email: ""}, seenAt)

	_, err := EnqueuePage(ctx, h.fixture.db, &stubFeatures{enabled: true}, h.fixture.projectID, EnqueueCursor{}, 50)
	require.NoError(t, err)

	surfaces := make(map[string]repo.SkillEfficacyEvaluation, 2)
	ids := make([]uuid.UUID, 0, 2)
	for _, row := range h.fixture.pendingEvaluations(t) {
		surfaces[row.Surface] = row
		ids = append(ids, row.ID)
	}
	require.Len(t, surfaces, 2, "one session on two surfaces is two scoring units")

	reserved, err := repo.New(h.fixture.db).ReserveSkillEfficacyEvaluations(ctx, repo.ReserveSkillEfficacyEvaluationsParams{
		ReservedOn: today(),
		ProjectID:  h.fixture.projectID,
		Ids:        ids,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), reserved)

	return surfaces[SurfaceDev], surfaces[SurfaceAssistant]
}

func (h publishHarness) reservedByID(t *testing.T, id uuid.UUID) (repo.SkillEfficacyEvaluation, bool) {
	t.Helper()

	rows, err := repo.New(h.fixture.db).LoadReservedSkillEfficacyEvaluations(t.Context(), repo.LoadReservedSkillEfficacyEvaluationsParams{
		ProjectID: h.fixture.projectID,
		BatchSize: 50,
	})
	require.NoError(t, err)

	for _, row := range rows {
		if row.ID == id {
			return row, true
		}
	}

	return repo.SkillEfficacyEvaluation{}, false
}

// spend counts the rows still holding a budget slot (reserved or scored) today,
// which is how a failed evaluation is told apart from a scored one.
func (h publishHarness) spend(t *testing.T) int64 {
	t.Helper()

	count, err := repo.New(h.fixture.db).CountSkillEfficacyOrgSpendForProject(t.Context(), repo.CountSkillEfficacyOrgSpendForProjectParams{
		ProjectID:  h.fixture.projectID,
		ReservedOn: today(),
	})
	require.NoError(t, err)

	return count
}

// publishedIDs returns one entry per stored score row for the given evaluations,
// so a duplicated publication shows up as a repeated id rather than as a single
// one. The guard read is keyed the way publication keys it: the evaluations'
// skill and skill version ids alongside their score ids.
func (h publishHarness) publishedIDs(t *testing.T, evaluations ...repo.SkillEfficacyEvaluation) []string {
	t.Helper()

	wanted := make([]string, 0, len(evaluations))
	skillIDs := make([]string, 0, len(evaluations))
	skillVersionIDs := make([]string, 0, len(evaluations))
	for _, evaluation := range evaluations {
		wanted = append(wanted, evaluation.ID.String())
		skillIDs = append(skillIDs, evaluation.SkillID.String())
		skillVersionIDs = append(skillVersionIDs, evaluation.SkillVersionID.String())
	}

	day := today().Time
	got, err := h.scores.ListExistingSkillEfficacyScoreIDs(t.Context(), telemetryrepo.ListExistingSkillEfficacyScoreIDsParams{
		OrganizationID:  h.fixture.organizationID,
		ProjectID:       h.fixture.projectID.String(),
		SkillIDs:        skillIDs,
		SkillVersionIDs: skillVersionIDs,
		IDs:             wanted,
		MinCreatedAt:    day.Add(-24 * time.Hour),
		MaxCreatedAt:    day.Add(72 * time.Hour),
	})
	require.NoError(t, err)

	return got
}

func TestPublishScoresReservedEvaluationOnce(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_scores")

	evaluation := h.reserve(t, "claude-session-publish", "claude-code")
	h.judge.results[SurfaceDev] = okVerdict()

	publisher := h.publisher(t, h.scores)
	result, err := publisher.Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 1, ModelFailures: 0, Failed: 0, Retryable: 0}, result)
	require.Equal(t, 1, h.judge.calls[SurfaceDev])
	require.Equal(t, []string{evaluation.ID.String()}, h.publishedIDs(t, evaluation))

	// The judge is told which skill it is scoring, not just its name.
	require.Len(t, h.judge.inputs, 1)
	require.Equal(t, urn.NewSkill(evaluation.SkillID).String(), h.judge.inputs[0].SkillURN)

	// Scored: no longer reserved, no longer pending, still holding its slot.
	_, stillReserved := h.reservedByID(t, evaluation.ID)
	require.False(t, stillReserved)
	require.Empty(t, h.fixture.pendingEvaluations(t))
	require.Equal(t, int64(1), h.spend(t))

	// A replayed batch resolves nothing, because the row left the reserved state.
	replay, err := publisher.Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 0, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 0}, replay)
	require.Equal(t, 1, h.judge.calls[SurfaceDev], "a replay must not pay for inference again")
	require.Equal(t, []string{evaluation.ID.String()}, h.publishedIDs(t, evaluation))
}

// The subject of a reservation can be deleted while the batch is in flight, so
// the judge inputs recheck the liveness the reservation checked. The batch
// resolves nothing: no inference is paid for and no score is written. The row
// keeps its reservation until a stale reset returns it to pending, where the
// candidate guard keeps it out of every page.
func TestPublishSkipsEvaluationWhoseChatWasDeleted(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	h := newPublishHarness(t, "skill_efficacy_publish_deleted_chat")

	evaluation := h.reserve(t, "claude-session-deleted", "claude-code")
	h.judge.results[SurfaceDev] = okVerdict()

	deleted, err := chatrepo.New(h.fixture.db).SoftDeleteChat(ctx, chatrepo.SoftDeleteChatParams{
		ProjectID: h.fixture.projectID,
		ID:        evaluation.ChatID,
	})
	require.NoError(t, err)
	require.True(t, deleted.Deleted)

	result, err := h.publisher(t, h.scores).Publish(ctx, h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 0, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 0}, result)
	require.Zero(t, h.judge.calls[SurfaceDev], "a deleted chat is never judged")
	require.Empty(t, h.publishedIDs(t, evaluation), "a deleted chat is never scored")

	_, stillReserved := h.reservedByID(t, evaluation.ID)
	require.True(t, stillReserved, "publication invents no terminal state for the row")

	reset, err := ResetStaleReservations(ctx, h.fixture.db, h.fixture.projectID, time.Microsecond)
	require.NoError(t, err)
	require.Equal(t, int64(1), reset)
	require.Empty(t, h.fixture.pendingEvaluations(t), "the row returns to pending and stays ineligible")
}

func TestPublishRetryAfterFailureBetweenInsertAndMark(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_partial")

	evaluation := h.reserve(t, "claude-session-partial", "claude-code")
	h.judge.results[SurfaceDev] = okVerdict()

	// The state a crash between the insert and the mark leaves behind: the score
	// is committed under the evaluation id, the row is still reserved.
	committed := okVerdict()
	require.NoError(t, h.scores.InsertSkillEfficacyScores(t.Context(), []telemetryrepo.SkillEfficacyScore{{
		ID:                 evaluation.ID,
		CreatedAt:          time.Now().UTC(),
		OrganizationID:     h.fixture.organizationID,
		ProjectID:          h.fixture.projectID.String(),
		SessionID:          evaluation.SessionID,
		SkillID:            evaluation.SkillID,
		SkillVersionID:     evaluation.SkillVersionID,
		CanonicalSHA256:    evaluation.CanonicalSha256,
		Surface:            evaluation.Surface,
		TraceID:            nil,
		GramChatID:         evaluation.ChatID.String(),
		Score:              committed.Verdict.Score,
		Rationale:          committed.Verdict.Rationale,
		EstTurnsSaved:      committed.Verdict.EstTurnsSaved,
		EstMinutesSaved:    committed.Verdict.EstMinutesSaved,
		ROIConfidence:      committed.Verdict.ROIConfidence,
		Flags:              committed.Verdict.Flags,
		JudgeModel:         committed.Model,
		JudgePromptVersion: committed.PromptVersion,
	}}))

	result, err := h.publisher(t, h.scores).Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 1, Scored: 1, ModelFailures: 0, Failed: 0, Retryable: 0}, result)
	require.Zero(t, h.judge.calls[SurfaceDev], "the guard must run before the judge")
	require.Equal(t, []string{evaluation.ID.String()}, h.publishedIDs(t, evaluation), "the retry inserts nothing")

	_, stillReserved := h.reservedByID(t, evaluation.ID)
	require.False(t, stillReserved)
	require.Equal(t, int64(1), h.spend(t))
}

func TestPublishRetryFindsScoreOfReservationOlderThanGuardSlack(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_aged_reservation")

	// Reserved days ago, so the day the row spent its budget on is further in the
	// past than guardWindowSlack: a window bounded only by reserved_on would end
	// before the score this pass is about to insert.
	evaluation := h.reserveOn(t, "claude-session-aged", "claude-code", daysAgo(4))
	h.judge.results[SurfaceDev] = okVerdict()

	crashCtx, cancel := context.WithCancel(t.Context())
	defer cancel()

	crashed, err := h.publisher(t, crashingSink{ScoreSink: h.scores, cancel: cancel}).Publish(crashCtx, h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.ErrorIs(t, err, ErrRetryable)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 1}, crashed)
	require.Equal(t, 1, h.judge.calls[SurfaceDev])
	require.Equal(t, []string{evaluation.ID.String()}, h.publishedIDs(t, evaluation))

	row, ok := h.reservedByID(t, evaluation.ID)
	require.True(t, ok, "the mark never ran, so the row is still reserved")
	require.Equal(t, StateReserved, row.State)

	// The retry's guard has to see the committed score even though it was stamped
	// long after the reservation day.
	retry, err := h.publisher(t, h.scores).Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 1, Scored: 1, ModelFailures: 0, Failed: 0, Retryable: 0}, retry)
	require.Equal(t, 1, h.judge.calls[SurfaceDev], "the retry must not pay for inference again")
	require.Equal(t, []string{evaluation.ID.String()}, h.publishedIDs(t, evaluation), "the retry inserts nothing")

	_, stillReserved := h.reservedByID(t, evaluation.ID)
	require.False(t, stillReserved)
	require.Empty(t, h.fixture.pendingEvaluations(t))
}

func TestPublishRetryFindsScoreAfterStaleResetMovedReservationDay(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_stale_reset")
	ctx := t.Context()
	queries := repo.New(h.fixture.db)

	evaluation := h.reserve(t, "claude-session-stale-reset", "claude-code")
	h.judge.results[SurfaceDev] = okVerdict()

	// A pass commits the score and dies before marking, leaving the row reserved.
	crashCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	crashed, err := h.publisher(t, crashingSink{ScoreSink: h.scores, cancel: cancel}).Publish(crashCtx, h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.ErrorIs(t, err, ErrRetryable)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 1}, crashed)
	require.Equal(t, 1, h.judge.calls[SurfaceDev])

	// The reservation then goes stale and returns to the queue.
	reset, err := queries.ResetStaleSkillEfficacyReservations(ctx, repo.ResetStaleSkillEfficacyReservationsParams{
		ProjectID:  h.fixture.projectID,
		StaleAfter: pgtype.Interval{Microseconds: 0, Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), reset)

	// A later pass re-reserves it against the UTC day it is running on, which is
	// past the day the committed score was stamped on. daysAgo(-1) stands in for
	// that later day without the test having to wait for midnight.
	reserved, err := queries.ReserveSkillEfficacyEvaluations(ctx, repo.ReserveSkillEfficacyEvaluationsParams{
		ReservedOn: daysAgo(-1),
		ProjectID:  h.fixture.projectID,
		Ids:        []uuid.UUID{evaluation.ID},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), reserved)

	// A window anchored on reserved_on now starts after the committed score and
	// would pay for inference again; the evaluation's created_at does not move.
	retry, err := h.publisher(t, h.scores).Publish(ctx, h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 1, Scored: 1, ModelFailures: 0, Failed: 0, Retryable: 0}, retry)
	require.Equal(t, 1, h.judge.calls[SurfaceDev], "the guard must find the score the crashed pass wrote")
	require.Equal(t, []string{evaluation.ID.String()}, h.publishedIDs(t, evaluation), "the retry inserts no duplicate")

	_, stillReserved := h.reservedByID(t, evaluation.ID)
	require.False(t, stillReserved)
	require.Empty(t, h.fixture.pendingEvaluations(t))
}

func TestPublishModelFailuresChargeAttemptsAndBatchContinues(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_model_failure")

	failing := h.reserve(t, "claude-session-model-failure", "claude-code")
	healthy := h.reserve(t, uuid.NewString(), "assistants")
	h.judge.errs[SurfaceDev] = fmt.Errorf("unparseable judge output: %w", ErrModelFailure)
	h.judge.results[SurfaceAssistant] = okVerdict()

	publisher := h.publisher(t, h.scores)
	batch := []uuid.UUID{failing.ID, healthy.ID}

	first, err := publisher.Publish(t.Context(), h.fixture.projectID, batch, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 2, AlreadyPublished: 0, Scored: 1, ModelFailures: 1, Failed: 0, Retryable: 0}, first)
	require.Equal(t, []string{healthy.ID.String()}, h.publishedIDs(t, failing, healthy))

	// Non-terminal: still reserved, so its budget slot stays spent and no second
	// reservation can re-spend the unit.
	row, ok := h.reservedByID(t, failing.ID)
	require.True(t, ok)
	require.Equal(t, StateReserved, row.State)
	require.Equal(t, int32(1), row.Attempts)

	second, err := publisher.Publish(t.Context(), h.fixture.projectID, batch, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 0, ModelFailures: 1, Failed: 0, Retryable: 0}, second)
	row, ok = h.reservedByID(t, failing.ID)
	require.True(t, ok)
	require.Equal(t, StateReserved, row.State)
	require.Equal(t, int32(2), row.Attempts)

	third, err := publisher.Publish(t.Context(), h.fixture.projectID, batch, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 1, Retryable: 0}, third)

	_, ok = h.reservedByID(t, failing.ID)
	require.False(t, ok, "the third model failure is terminal")
	require.Empty(t, h.fixture.pendingEvaluations(t), "a failed evaluation never returns to pending")
	require.Equal(t, []string{healthy.ID.String()}, h.publishedIDs(t, failing, healthy), "a failed judge writes no score")
	require.Equal(t, int64(1), h.spend(t), "the failed row released its budget slot")
	require.Equal(t, 3, h.judge.calls[SurfaceDev])
	require.Equal(t, 1, h.judge.calls[SurfaceAssistant])
}

func TestPublishRateLimitLeavesEvaluationUntouched(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_ratelimit")

	evaluation := h.reserve(t, "claude-session-ratelimit", "claude-code")
	h.judge.errs[SurfaceDev] = fmt.Errorf("skill efficacy judge call: %w", ErrRetryable)

	result, err := h.publisher(t, h.scores).Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.ErrorIs(t, err, ErrRetryable)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 1}, result)

	row, ok := h.reservedByID(t, evaluation.ID)
	require.True(t, ok)
	require.Equal(t, StateReserved, row.State)
	require.Zero(t, row.Attempts, "infrastructure failures charge no attempt")
	require.Empty(t, h.publishedIDs(t, evaluation))
}

// A sink failure lands after the judge has answered and been paid for, and the
// guard has nothing to find, so every retry buys that inference again. The row
// is still retried — the failure is infrastructure — but the attempt is charged,
// which is what bounds the paid calls one evaluation can ever cost.
func TestPublishClickHouseFailureRetriesUnderABoundedAttemptBudget(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_sink_failure")

	evaluation := h.reserve(t, "claude-session-sink-failure", "claude-code")
	h.judge.results[SurfaceDev] = okVerdict()

	publisher := h.publisher(t, failingSink{ScoreSink: h.scores, err: errors.New("clickhouse unavailable")})
	batch := []uuid.UUID{evaluation.ID}

	for attempt := 1; attempt < MaxModelAttempts; attempt++ {
		result, err := publisher.Publish(t.Context(), h.fixture.projectID, batch, nil)
		require.ErrorIs(t, err, ErrRetryable)
		require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 1}, result)

		row, ok := h.reservedByID(t, evaluation.ID)
		require.True(t, ok)
		require.Equal(t, StateReserved, row.State, "a sink failure is still infrastructure, so the row stays claimable")
		require.Equal(t, int32(attempt), row.Attempts)
	}

	final, err := publisher.Publish(t.Context(), h.fixture.projectID, batch, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 1, Retryable: 0}, final)

	_, ok := h.reservedByID(t, evaluation.ID)
	require.False(t, ok, "the attempt ceiling terminates the row rather than paying for it again")
	require.Empty(t, h.fixture.pendingEvaluations(t), "a failed evaluation never returns to pending")
	require.Empty(t, h.publishedIDs(t, evaluation))
	require.Equal(t, MaxModelAttempts, h.judge.calls[SurfaceDev], "a broken sink cannot buy the same inference forever")

	// A row the ceiling terminated is out of the batch, so a later pass judges
	// nothing at all for it.
	after, err := publisher.Publish(t.Context(), h.fixture.projectID, batch, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 0, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 0}, after)
	require.Equal(t, MaxModelAttempts, h.judge.calls[SurfaceDev])
}

func TestPublishClickHouseConstraintFailureTerminatesOnlyInvalidRow(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_sink_constraint")

	invalid := h.reserve(t, "claude-session-sink-constraint", "claude-code")
	healthy := h.reserve(t, uuid.NewString(), "assistants")
	h.judge.results[SurfaceDev] = okVerdict()
	h.judge.results[SurfaceAssistant] = okVerdict()

	result, err := h.publisher(t, invalidDevSink{ScoreSink: h.scores}).Publish(
		t.Context(), h.fixture.projectID, []uuid.UUID{invalid.ID, healthy.ID}, nil,
	)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 2, AlreadyPublished: 0, Scored: 1, ModelFailures: 0, Failed: 1, Retryable: 0}, result)
	require.Equal(t, 1, h.judge.calls[SurfaceDev])
	require.Equal(t, 1, h.judge.calls[SurfaceAssistant])
	require.Equal(t, []string{healthy.ID.String()}, h.publishedIDs(t, invalid, healthy))

	_, stillReserved := h.reservedByID(t, invalid.ID)
	require.False(t, stillReserved)
}

func TestPublishNormalizesJudgeClientOutputBeforeTheSink(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "efficacy_publish_normalize_boundary")

	evaluation := h.reserve(t, "claude-session-normalize-boundary", "claude-code")
	invalidConfidence := "invented"
	h.judge.results[SurfaceDev] = JudgeResult{
		Verdict: Verdict{
			Score:           5,
			Rationale:       strings.Repeat("é", maxRationaleRunes+1),
			EstTurnsSaved:   new(-1.0),
			EstMinutesSaved: nil,
			ROIConfidence:   &invalidConfidence,
			Flags:           []string{"invented", "harmful", "harmful"},
		},
		Model:         "test-judge-model",
		PromptVersion: JudgePromptVersion,
	}

	result, err := h.publisher(t, h.scores).Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 1, ModelFailures: 0, Failed: 0, Retryable: 0}, result)
	require.Equal(t, []string{evaluation.ID.String()}, h.publishedIDs(t, evaluation))
}

// The attempt charge writes through a :one query, and a row another owner has
// already moved out of reserved returns nothing. That is an outcome, not an
// error: failing it here would drop the whole batch over a row this pass no
// longer owns.
func TestPublishChargingAnEvaluationThatLeftReservedDoesNotFailTheBatch(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	h := newPublishHarness(t, "skill_efficacy_publish_charge_unreserved")

	stolen := h.reserve(t, "claude-session-charge-unreserved", "claude-code")
	healthy := h.reserve(t, uuid.NewString(), "assistants")
	h.judge.errs[SurfaceDev] = fmt.Errorf("unparseable judge output: %w", ErrModelFailure)
	h.judge.results[SurfaceAssistant] = okVerdict()

	publisher := h.publisher(t, h.scores)
	publisher.judge = stealingJudge{
		JudgeClient:  publisher.judge,
		db:           h.fixture.db,
		projectID:    h.fixture.projectID,
		evaluationID: stolen.ID,
		surface:      SurfaceDev,
	}

	result, err := publisher.Publish(ctx, h.fixture.projectID, []uuid.UUID{stolen.ID, healthy.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 2, AlreadyPublished: 0, Scored: 1, ModelFailures: 1, Failed: 0, Retryable: 0}, result,
		"the charge is a no-op and the rest of the batch still runs")
	require.Equal(t, []string{healthy.ID.String()}, h.publishedIDs(t, stolen, healthy))

	pending := h.fixture.pendingEvaluations(t)
	require.Len(t, pending, 1)
	require.Equal(t, stolen.ID, pending[0].ID)
	require.Zero(t, pending[0].Attempts, "the owner that took the row is the one that accounts for it")
}

func TestPublishReadsSharedChatTranscriptOncePerPass(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_shared_chat")

	dev, assistant := h.reserveSharedChat(t, uuid.NewString())
	h.judge.results[SurfaceDev] = okVerdict()
	h.judge.results[SurfaceAssistant] = okVerdict()

	publisher := h.publisher(t, h.scores)
	chats := &countingChats{TranscriptSource: publisher.chats, loads: map[uuid.UUID]int{}, counts: map[uuid.UUID]int{}, rows: 0}
	publisher.chats = chats

	result, err := publisher.Publish(t.Context(), h.fixture.projectID, []uuid.UUID{dev.ID, assistant.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 2, AlreadyPublished: 0, Scored: 2, ModelFailures: 0, Failed: 0, Retryable: 0}, result)

	require.Equal(t, dev.ChatID, assistant.ChatID, "both units score the same session")
	require.Equal(t, map[uuid.UUID]int{dev.ChatID: 1}, chats.loads, "the shared chat is read once for the whole pass")

	// Both judge calls were handed the one rendering that read produced.
	require.Len(t, h.judge.inputs, 2)
	require.NotEmpty(t, h.judge.inputs[0].Transcript.Messages)
	require.Equal(t, h.judge.inputs[0].Transcript, h.judge.inputs[1].Transcript)
}

func TestPublishDoesNotCacheAFailedTranscriptRead(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_read_failure")

	dev, assistant := h.reserveSharedChat(t, uuid.NewString())
	h.judge.results[SurfaceDev] = okVerdict()
	h.judge.results[SurfaceAssistant] = okVerdict()

	publisher := h.publisher(t, h.scores)
	chats := &flakyChats{TranscriptSource: publisher.chats, loads: 0}
	publisher.chats = chats

	result, err := publisher.Publish(t.Context(), h.fixture.projectID, []uuid.UUID{dev.ID, assistant.ID}, nil)
	require.ErrorIs(t, err, ErrRetryable)
	require.Equal(t, PublishResult{Loaded: 2, AlreadyPublished: 0, Scored: 1, ModelFailures: 0, Failed: 0, Retryable: 1}, result)
	require.Equal(t, 2, chats.loads, "the second evaluation re-reads rather than inheriting the failure")

	// Whichever of the two the batch reached first is the one that failed, and it
	// is left exactly as it was found.
	published := h.publishedIDs(t, dev, assistant)
	require.Len(t, published, 1, "the evaluation whose read failed wrote no score")

	unread := dev.ID
	if published[0] == dev.ID.String() {
		unread = assistant.ID
	}
	row, ok := h.reservedByID(t, unread)
	require.True(t, ok)
	require.Equal(t, StateReserved, row.State)
	require.Zero(t, row.Attempts, "a failed transcript read charges no attempt")
}

func TestPublishTimesOutAHungJudge(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_judge_hang")

	evaluation := h.reserve(t, "claude-session-judge-hang", "claude-code")

	publisher := h.publisher(t, h.scores)
	require.Equal(t, publishEvaluationTimeout, publisher.evaluationTimeout, "the bound a constructed publisher runs under")
	// Shortened for the test only: what is asserted is that the bound cuts the
	// hang, not how long the production bound is.
	publisher.evaluationTimeout = 100 * time.Millisecond
	publisher.judge = blockingJudge{}

	result, err := publisher.Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.ErrorIs(t, err, ErrRetryable, "a hang is infrastructure, so the same rows are retried")
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 1}, result)

	row, ok := h.reservedByID(t, evaluation.ID)
	require.True(t, ok)
	require.Equal(t, StateReserved, row.State)
	require.Zero(t, row.Attempts, "a hang charges no attempt")
	require.Empty(t, row.LastError.String)
	require.Empty(t, h.publishedIDs(t, evaluation))
}

func TestPublishTimesOutAHungSink(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_sink_hang")

	evaluation := h.reserve(t, "claude-session-sink-hang", "claude-code")
	h.judge.results[SurfaceDev] = okVerdict()

	publisher := h.publisher(t, blockingSink{ScoreSink: h.scores})
	publisher.evaluationTimeout = 100 * time.Millisecond

	result, err := publisher.Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.ErrorIs(t, err, ErrRetryable)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 1}, result)
	require.Equal(t, 1, h.judge.calls[SurfaceDev])

	row, ok := h.reservedByID(t, evaluation.ID)
	require.True(t, ok)
	require.Equal(t, StateReserved, row.State)
	require.Zero(t, row.Attempts)
	require.Empty(t, h.publishedIDs(t, evaluation))
}

func TestPublishModelFailureRecordsClassNotProviderDetail(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_failure_redaction")

	evaluation := h.reserve(t, "claude-session-redaction", "claude-code")

	// A provider error body can quote the request back, and the request carries
	// the session transcript: whatever the model says must reach neither the
	// column nor the log.
	const echoed = "leaked-transcript-fragment-0123456789"
	h.judge.errs[SurfaceDev] = fmt.Errorf("openrouter rejected efficacy judge request: %w: 400 %s", ErrModelFailure, echoed)

	var logs bytes.Buffer
	publisher := NewPublisher(slog.New(slog.NewJSONHandler(&logs, nil)), testenv.NewTracerProvider(t), h.fixture.db, h.scores, h.judge)

	result, err := publisher.Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 0, ModelFailures: 1, Failed: 0, Retryable: 0}, result)

	row, ok := h.reservedByID(t, evaluation.ID)
	require.True(t, ok)
	require.Equal(t, int32(1), row.Attempts)
	require.Equal(t, modelFailureClass, row.LastError.String)

	// The log identifies the row and classifies the failure, and says nothing the
	// provider said.
	logged := logs.String()
	require.NotContains(t, logged, echoed)
	require.NotContains(t, row.LastError.String, echoed)
	require.Contains(t, logged, modelFailureClass)
	require.Contains(t, logged, evaluation.ID.String())
	require.Contains(t, logged, urn.NewSkill(evaluation.SkillID).String())
	require.Contains(t, logged, evaluation.SessionID)
	require.Contains(t, logged, evaluation.ChatID.String())
}

// seedTranscriptMessages appends messages to a chat, oldest first: message i is
// stamped i seconds after start and carries bodyFor(i), which is how a test
// builds a session long enough — or wide enough — to make the loader page.
func (h publishHarness) seedTranscriptMessages(t *testing.T, chatID uuid.UUID, projectID uuid.NullUUID, start time.Time, roles []string, bodyFor func(i int) string, count int) {
	t.Helper()

	queries := chatrepo.New(h.fixture.db)
	for i := range count {
		_, err := queries.SeedChatTranscriptMessage(t.Context(), chatrepo.SeedChatTranscriptMessageParams{
			ChatID:    chatID,
			ProjectID: projectID,
			Role:      roles[i%len(roles)],
			Content:   bodyFor(i),
			CreatedAt: conv.ToPGTimestamptz(start.Add(time.Duration(i) * time.Second)),
		})
		require.NoError(t, err)
	}
}

// wholeChatTranscript is the rendering the pre-paging loader produced: the whole
// chat read at once and handed straight to RenderTranscript. It is the oracle
// the paged loader is held to.
func (h publishHarness) wholeChatTranscript(t *testing.T, chatID uuid.UUID) Transcript {
	t.Helper()

	messages, err := chatrepo.New(h.fixture.db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: h.fixture.projectID,
	})
	require.NoError(t, err)

	inputs := make([]TranscriptInput, 0, len(messages))
	for _, m := range messages {
		inputs = append(inputs, TranscriptInput{
			ID:               m.ID,
			Seq:              m.Seq,
			CreatedAt:        m.CreatedAt,
			Role:             m.Role,
			Content:          m.Content,
			ToolCalls:        m.ToolCalls,
			ToolCallID:       m.ToolCallID,
			ToolURN:          m.ToolUrn,
			ToolOutcome:      m.ToolOutcome,
			ToolOutcomeNotes: m.ToolOutcomeNotes,
		})
	}

	return RenderTranscript(inputs)
}

// transcriptPayload is the transcript exactly as it travels in the judge's user
// turn, which is the level the paged and whole-chat renderings have to agree at:
// semantic equality is not enough, because the bytes are what the model reads
// and what the score is attributed to.
func transcriptPayload(t *testing.T, transcript Transcript) string {
	t.Helper()

	payload, err := json.Marshal(transcript)
	require.NoError(t, err)

	return string(payload)
}

// requireSameTranscript fails on any difference between the two — omission
// marker, message set, field order alike.
func requireSameTranscript(t *testing.T, want Transcript, got Transcript) {
	t.Helper()

	require.Equal(t, transcriptPayload(t, want), transcriptPayload(t, got), "the paged rendering must be byte-for-byte the whole-chat rendering")
}

func TestPublishPagedTranscriptMatchesAWholeChatRead(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_paged_exact")

	session := uuid.NewString()
	evaluation := h.reserve(t, session, "claude-code")
	h.judge.results[SurfaceDev] = okVerdict()

	// Several pages' worth of a mixed transcript that still fits the budget
	// whole, so the paged loader must read all of it and land on the same
	// rendering the whole-chat read produces.
	h.seedTranscriptMessages(t,
		evaluation.ChatID,
		uuid.NullUUID{UUID: h.fixture.projectID, Valid: true},
		time.Now().UTC().Add(-6*time.Hour),
		[]string{"user", "assistant", "tool", "system"},
		func(i int) string { return fmt.Sprintf("message-%02d: %s", i, strings.Repeat("x", i*7)) },
		45,
	)

	publisher := h.publisher(t, h.scores)
	chats := &countingChats{TranscriptSource: publisher.chats, loads: map[uuid.UUID]int{}, counts: map[uuid.UUID]int{}, rows: 0}
	publisher.chats = chats

	result, err := publisher.Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 1, ModelFailures: 0, Failed: 0, Retryable: 0}, result)

	require.Len(t, h.judge.inputs, 1)
	got := h.judge.inputs[0].Transcript
	requireSameTranscript(t, h.wholeChatTranscript(t, evaluation.ChatID), got)
	require.Empty(t, got.Omitted, "nothing was dropped, so the marker stays absent")
	require.Len(t, got.Messages, 47, "the two seeded messages plus the mixed transcript")
	require.Greater(t, chats.loads[evaluation.ChatID], 1, "a transcript past one page is read in pages")
}

func TestPublishBoundsPagesForALongTranscript(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_long_transcript")

	session := uuid.NewString()
	evaluation := h.reserve(t, session, "claude-code")
	h.judge.results[SurfaceDev] = okVerdict()

	// Sixty bodies of 6000 runes are three times the rendering budget, so the
	// loader has to stop well short of the chat.
	const longMessages = 60
	h.seedTranscriptMessages(t,
		evaluation.ChatID,
		uuid.NullUUID{UUID: h.fixture.projectID, Valid: true},
		time.Now().UTC().Add(-time.Hour),
		[]string{"user", "assistant"},
		func(i int) string { return fmt.Sprintf("message-%02d: %s", i, strings.Repeat("y", 6000)) },
		longMessages,
	)
	total := longMessages + 2

	publisher := h.publisher(t, h.scores)
	chats := &countingChats{TranscriptSource: publisher.chats, loads: map[uuid.UUID]int{}, counts: map[uuid.UUID]int{}, rows: 0}
	publisher.chats = chats

	result, err := publisher.Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 1, ModelFailures: 0, Failed: 0, Retryable: 0}, result)

	require.Len(t, h.judge.inputs, 1)
	got := h.judge.inputs[0].Transcript

	// Bounded: the loader never held the chat, and it overshoots the rendering
	// it sends by at most the page that stopped it.
	require.Less(t, chats.rows, total, "the whole chat was never read")
	require.LessOrEqual(t, chats.rows, len(got.Messages)+transcriptPageSize, "the read overshoots by at most one page")

	// Exact: the marker counts the messages the rendering dropped AND the ones
	// the loader never asked for.
	require.Equal(t, fmt.Sprintf("[%d earlier messages omitted]", total-len(got.Messages)), got.Omitted)

	// The end of the session is what the judge is here to read.
	newest := got.Messages[len(got.Messages)-1]
	require.True(t, strings.HasPrefix(newest.Content, fmt.Sprintf("message-%02d: ", longMessages-1)), "the newest message is retained")

	// The kept messages are exactly the ones a whole-chat read would have kept.
	requireSameTranscript(t, h.wholeChatTranscript(t, evaluation.ChatID), got)
}

func TestPublishTranscriptIgnoresAnotherProjectsMessages(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_isolation")

	session := uuid.NewString()
	evaluation := h.reserve(t, session, "claude-code")
	h.judge.results[SurfaceDev] = okVerdict()

	const foreign = "foreign-project-message"
	h.seedTranscriptMessages(t,
		evaluation.ChatID,
		uuid.NullUUID{UUID: uuid.New(), Valid: true},
		time.Now().UTC().Add(-time.Minute),
		[]string{"user"},
		func(int) string { return foreign },
		3,
	)

	result, err := h.publisher(t, h.scores).Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 1, ModelFailures: 0, Failed: 0, Retryable: 0}, result)

	require.Len(t, h.judge.inputs, 1)
	got := h.judge.inputs[0].Transcript
	require.Len(t, got.Messages, 2, "only this project's messages are transcript material")
	for _, message := range got.Messages {
		require.NotContains(t, message.Content, foreign)
	}
	require.Empty(t, got.Omitted, "a message that was never this project's is not an omission")
}

func TestPublishLoadsEveryShortMessageThatFitsTheTranscriptBudget(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_short_messages")

	evaluation := h.reserve(t, uuid.NewString(), "claude-code")
	h.judge.results[SurfaceDev] = okVerdict()

	const shortMessages = 1001
	h.seedTranscriptMessages(t,
		evaluation.ChatID,
		uuid.NullUUID{UUID: h.fixture.projectID, Valid: true},
		// Newer than reserve's own seeded messages (~90 minutes old), so this
		// batch — not those — holds the newest message the walk must retain.
		time.Now().UTC().Add(-time.Hour),
		[]string{"user", "assistant"},
		func(i int) string { return fmt.Sprintf("m%d", i) },
		shortMessages,
	)
	total := shortMessages + 2

	publisher := h.publisher(t, h.scores)
	chats := &countingChats{TranscriptSource: publisher.chats, loads: map[uuid.UUID]int{}, counts: map[uuid.UUID]int{}, rows: 0}
	publisher.chats = chats

	result, err := publisher.Publish(t.Context(), h.fixture.projectID, []uuid.UUID{evaluation.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 1, AlreadyPublished: 0, Scored: 1, ModelFailures: 0, Failed: 0, Retryable: 0}, result)

	require.Equal(t, (total+transcriptPageSize-1)/transcriptPageSize, chats.loads[evaluation.ChatID])
	require.Equal(t, 1, chats.counts[evaluation.ChatID])
	require.Equal(t, total, chats.rows)

	require.Len(t, h.judge.inputs, 1)
	got := h.judge.inputs[0].Transcript
	require.Len(t, got.Messages, total, "every message that fits is included")
	require.Empty(t, got.Omitted)

	// The end of the session is what the judge is here to read.
	newest := got.Messages[len(got.Messages)-1]
	require.Equal(t, fmt.Sprintf("m%d", shortMessages-1), newest.Content)
}

// A batch is many minutes of paid model calls under a lease the caller owns, so
// the pass reports progress once per evaluation — and stops at the next boundary
// when whatever owns that lease revokes it, rather than paying for the rest of a
// batch a second attempt now owns.
func TestPublishHeartbeatsEachEvaluationAndStopsWhenCancelled(t *testing.T) {
	t.Parallel()
	h := newPublishHarness(t, "skill_efficacy_publish_heartbeat")

	first := h.reserve(t, "claude-session-heartbeat", "claude-code")
	second := h.reserve(t, uuid.NewString(), "assistants")
	h.judge.results[SurfaceDev] = okVerdict()
	h.judge.results[SurfaceAssistant] = okVerdict()

	publisher := h.publisher(t, h.scores)
	batch := []uuid.UUID{first.ID, second.ID}

	beats := 0
	result, err := publisher.Publish(t.Context(), h.fixture.projectID, batch, func() { beats++ })
	require.NoError(t, err)
	require.Equal(t, PublishResult{Loaded: 2, AlreadyPublished: 0, Scored: 2, ModelFailures: 0, Failed: 0, Retryable: 0}, result)
	require.Equal(t, 2, beats, "one report per evaluation")

	// Cancelling from the heartbeat is what a revoked activity does: the pass
	// stops at the boundary and comes back retryable, having judged nothing more.
	replay := newPublishHarness(t, "skill_efficacy_publish_heartbeat_cancel")
	cancelled := replay.reserve(t, "claude-session-heartbeat-cancel", "claude-code")
	other := replay.reserve(t, uuid.NewString(), "assistants")
	replay.judge.results[SurfaceDev] = okVerdict()
	replay.judge.results[SurfaceAssistant] = okVerdict()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped, err := replay.publisher(t, replay.scores).Publish(ctx, replay.fixture.projectID, []uuid.UUID{cancelled.ID, other.ID}, cancel)
	require.ErrorIs(t, err, ErrRetryable)
	require.Zero(t, stopped.Scored, "the pass stops before it pays for anything")
	require.Empty(t, replay.judge.inputs, "a revoked lease buys no inference")

	for _, evaluation := range []repo.SkillEfficacyEvaluation{cancelled, other} {
		row, ok := replay.reservedByID(t, evaluation.ID)
		require.True(t, ok)
		require.Equal(t, StateReserved, row.State)
		require.Zero(t, row.Attempts, "a cancelled pass charges nothing")
	}
}
