package analysis

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func testJudges(t *testing.T, judges ...Judge) *Judges {
	t.Helper()

	roster, err := NewJudges(judges...)
	require.NoError(t, err)
	return roster
}

func TestEnqueuePage_EnqueuesEnabledJudgesPerChat(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newAnalysisFixture(t, "enqueue_enabled")
	roster := testJudges(t, stubNamedJudge{name: "work_units", verdict: stubVerdict(1), err: nil}, stubNamedJudge{name: "second", verdict: stubVerdict(1), err: nil})

	// Only one of the two registered judges is enabled for the organization.
	fixture.enableJudge(t, "work_units", 10)

	quiet := fixture.seedChat(t, 3, time.Hour)
	empty := fixture.seedChat(t, 0, 0)

	result, err := EnqueuePage(ctx, fixture.db, roster, fixture.projectID, EnqueueCursor{}, MaxEnqueuePageSize)
	require.NoError(t, err)
	require.Equal(t, 1, result.Scanned, "the chat with no messages is not a candidate")
	require.True(t, result.Exhausted)

	pending := fixture.pendingEvaluations(t)
	require.Len(t, pending, 1)
	require.Equal(t, quiet, pending[0].ChatID)
	require.Equal(t, "work_units", pending[0].Judge)
	require.NotEqual(t, empty, pending[0].ChatID)

	// Re-running the page is idempotent.
	_, err = EnqueuePage(ctx, fixture.db, roster, fixture.projectID, EnqueueCursor{}, MaxEnqueuePageSize)
	require.NoError(t, err)
	require.Len(t, fixture.pendingEvaluations(t), 1)
}

func TestEnqueuePage_NothingEnabledBuildsNoQueue(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newAnalysisFixture(t, "enqueue_disabled")
	roster := testJudges(t, stubNamedJudge{name: "work_units", verdict: stubVerdict(1), err: nil})

	fixture.seedChat(t, 3, time.Hour)

	result, err := EnqueuePage(ctx, fixture.db, roster, fixture.projectID, EnqueueCursor{}, MaxEnqueuePageSize)
	require.NoError(t, err)
	require.Zero(t, result.Scanned)
	require.True(t, result.Exhausted)
	require.Empty(t, fixture.pendingEvaluations(t))
}

func TestReserve_RespectsJudgeDailyCap(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newAnalysisFixture(t, "reserve_cap")
	roster := testJudges(t, stubNamedJudge{name: "work_units", verdict: stubVerdict(1), err: nil})
	fixture.enableJudge(t, "work_units", 2)

	for range 3 {
		fixture.seedChat(t, 2, time.Hour)
	}
	_, err := EnqueuePage(ctx, fixture.db, roster, fixture.projectID, EnqueueCursor{}, MaxEnqueuePageSize)
	require.NoError(t, err)
	require.Len(t, fixture.pendingEvaluations(t), 3)

	reserved, _, err := Reserve(ctx, fixture.db, roster, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, 2, "the daily cap bounds the batch")

	// The cap is spent for the day, so a second pass reserves nothing.
	again, _, err := Reserve(ctx, fixture.db, roster, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Empty(t, again)
}

func TestReserve_SkipsActiveChats(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newAnalysisFixture(t, "reserve_active")
	roster := testJudges(t, stubNamedJudge{name: "work_units", verdict: stubVerdict(1), err: nil})
	fixture.enableJudge(t, "work_units", 10)

	quiet := fixture.seedChat(t, 2, time.Hour)
	fixture.seedChat(t, 2, time.Minute)

	_, err := EnqueuePage(ctx, fixture.db, roster, fixture.projectID, EnqueueCursor{}, MaxEnqueuePageSize)
	require.NoError(t, err)

	reserved, _, err := Reserve(ctx, fixture.db, roster, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, 1, "a session still writing messages is not analyzable")
	require.Equal(t, quiet, reserved[0].ChatID)
}

func TestReserveQuery_RechecksQuietWindowAtWrite(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newAnalysisFixture(t, "reserve_recheck")
	roster := testJudges(t, stubNamedJudge{name: "work_units", verdict: stubVerdict(1), err: nil})
	fixture.enableJudge(t, "work_units", 10)

	chatID := fixture.seedChat(t, 2, time.Hour)
	_, err := EnqueuePage(ctx, fixture.db, roster, fixture.projectID, EnqueueCursor{}, MaxEnqueuePageSize)
	require.NoError(t, err)
	pending := fixture.pendingEvaluations(t)
	require.Len(t, pending, 1)

	// A message lands after the candidate read admitted the evaluation: the
	// reserve write's own quiet recheck must leave the row pending.
	fixture.seedMessage(t, chatID, time.Minute)

	now := time.Now().UTC()
	written, err := repo.New(fixture.db).ReserveChatAnalysisEvaluations(ctx, repo.ReserveChatAnalysisEvaluationsParams{
		ReservedOn: pgtype.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), InfinityModifier: pgtype.Finite, Valid: true},
		ProjectID:  fixture.projectID,
		Ids:        []uuid.UUID{pending[0].ID},
		Inactivity: pgtype.Interval{Microseconds: InactivityWindow.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	require.Empty(t, written, "an active session must not be reserved")
	require.Equal(t, StatePending, fixture.evaluation(t, pending[0].ID).State)
}

func TestPublish_ScoresReservedBatch(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newAnalysisFixture(t, "publish_scores")
	roster := testJudges(t, stubNamedJudge{name: "work_units", verdict: stubVerdict(24), err: nil})
	fixture.enableJudge(t, "work_units", 10)

	chatID := fixture.seedChat(t, 3, time.Hour)
	_, err := EnqueuePage(ctx, fixture.db, roster, fixture.projectID, EnqueueCursor{}, MaxEnqueuePageSize)
	require.NoError(t, err)
	reserved, _, err := Reserve(ctx, fixture.db, roster, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, 1)

	sink := &captureSink{}
	publisher := NewPublisher(testenv.NewLogger(t), testenv.NewTracerProvider(t), fixture.db, sink, nil, roster)

	result, err := publisher.Publish(ctx, fixture.projectID, []uuid.UUID{reserved[0].ID}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Loaded)
	require.Equal(t, 1, result.Scored)
	require.Zero(t, result.ModelFailures)

	rows := sink.rows(t)
	require.Len(t, rows, 1)
	require.Equal(t, reserved[0].ID, rows[0].ID)
	require.Equal(t, fixture.organizationID, rows[0].OrganizationID)
	require.Equal(t, chatID.String(), rows[0].ChatID)
	require.Equal(t, "work_units", rows[0].Judge)
	require.InDelta(t, 24, rows[0].Score, 0.0001)
	require.JSONEq(t, `{"stub":true}`, rows[0].Detail)
	require.Equal(t, "stub-model", rows[0].JudgeModel)

	require.Equal(t, "scored", fixture.evaluation(t, reserved[0].ID).State)
}

func TestPublish_ModelFailureChargesAttempt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newAnalysisFixture(t, "publish_model_failure")
	roster := testJudges(t, stubNamedJudge{name: "work_units", verdict: JudgeResult{}, err: fmt.Errorf("bad output: %w", ErrModelFailure)})
	fixture.enableJudge(t, "work_units", 1)

	fixture.seedChat(t, 2, time.Hour)
	_, err := EnqueuePage(ctx, fixture.db, roster, fixture.projectID, EnqueueCursor{}, MaxEnqueuePageSize)
	require.NoError(t, err)
	reserved, _, err := Reserve(ctx, fixture.db, roster, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, 1)

	sink := &captureSink{}
	publisher := NewPublisher(testenv.NewLogger(t), testenv.NewTracerProvider(t), fixture.db, sink, nil, roster)

	// Two failed passes stay reserved; the third terminates the evaluation.
	for attempt := 1; attempt <= int(MaxModelAttempts); attempt++ {
		result, err := publisher.Publish(ctx, fixture.projectID, []uuid.UUID{reserved[0].ID}, nil)
		require.NoError(t, err)
		row := fixture.evaluation(t, reserved[0].ID)
		require.Equal(t, int32(attempt), row.Attempts)
		if attempt < int(MaxModelAttempts) {
			require.Equal(t, 1, result.ModelFailures)
			require.Equal(t, StateReserved, row.State)
		} else {
			require.Equal(t, 1, result.Failed)
			require.Equal(t, StateFailed, row.State)
		}
	}
	require.Empty(t, sink.rows(t))

	// The failed evaluation keeps its reserved_on and still counts toward the
	// daily cap: failure never refunds budget, so a fresh candidate cannot be
	// reserved today.
	fixture.seedChat(t, 2, time.Hour)
	_, err = EnqueuePage(ctx, fixture.db, roster, fixture.projectID, EnqueueCursor{}, MaxEnqueuePageSize)
	require.NoError(t, err)
	again, _, err := Reserve(ctx, fixture.db, roster, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Empty(t, again, "the failed evaluation's spend is not returned")
}

func TestPublish_AlreadyPublishedSkipsJudge(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newAnalysisFixture(t, "publish_guard")
	boom := stubNamedJudge{name: "work_units", verdict: JudgeResult{}, err: fmt.Errorf("must not be called: %w", ErrRetryable)}
	roster := testJudges(t, boom)
	fixture.enableJudge(t, "work_units", 10)

	fixture.seedChat(t, 2, time.Hour)
	_, err := EnqueuePage(ctx, fixture.db, roster, fixture.projectID, EnqueueCursor{}, MaxEnqueuePageSize)
	require.NoError(t, err)
	reserved, _, err := Reserve(ctx, fixture.db, roster, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, 1)

	// The sink already holds this evaluation's score: a crash between insert and
	// mark. The pass must finish the transition without paying for inference.
	sink := &captureSink{existing: []string{reserved[0].ID.String()}}
	publisher := NewPublisher(testenv.NewLogger(t), testenv.NewTracerProvider(t), fixture.db, sink, nil, roster)

	result, err := publisher.Publish(ctx, fixture.projectID, []uuid.UUID{reserved[0].ID}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.AlreadyPublished)
	require.Equal(t, 1, result.Scored)
	require.Empty(t, sink.rows(t))
	require.Equal(t, "scored", fixture.evaluation(t, reserved[0].ID).State)
}

func TestPublish_EmitsWorkUnitsScoreEvent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newAnalysisFixture(t, "publish_score_event")
	roster := testJudges(t, stubNamedJudge{name: "work_units", verdict: stubVerdict(24), err: nil})
	fixture.enableJudge(t, "work_units", 10)

	chatID := fixture.seedChat(t, 3, time.Hour)
	_, err := EnqueuePage(ctx, fixture.db, roster, fixture.projectID, EnqueueCursor{}, MaxEnqueuePageSize)
	require.NoError(t, err)
	reserved, _, err := Reserve(ctx, fixture.db, roster, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, 1)

	sessionEnd := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	sink := &captureSink{facts: map[string]telemetryrepo.ChatSessionFacts{
		chatID.String(): {
			ChatID:          chatID.String(),
			UserEmail:       "worker@example.com",
			HookSource:      "claude-code",
			Model:           "claude-opus-4-8",
			AccountTypes:    []string{"", "team"},
			EndTimeUnixNano: sessionEnd.UnixNano(),
			TotalTokens:     4200,
			TotalCost:       1.25,
		},
	}}
	events := &captureEventSink{}
	publisher := NewPublisher(testenv.NewLogger(t), testenv.NewTracerProvider(t), fixture.db, sink, events, roster)

	result, err := publisher.Publish(ctx, fixture.projectID, []uuid.UUID{reserved[0].ID}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Scored)

	logged := events.events(t)
	require.Len(t, logged, 1)
	event := logged[0]
	require.Equal(t, WorkUnitsScoreEventURN, event.ToolInfo.URN)
	require.Equal(t, fixture.projectID.String(), event.ToolInfo.ProjectID)
	require.Equal(t, fixture.organizationID, event.ToolInfo.OrganizationID)
	require.Equal(t, "worker@example.com", event.UserInfo.Email())
	require.Equal(t, sessionEnd, event.Timestamp)
	require.Equal(t, chatID.String(), event.Attributes[attr.GenAIConversationIDKey])
	require.Equal(t, "claude-opus-4-8", event.Attributes[attr.GenAIResponseModelKey])
	require.Equal(t, "claude-code", event.Attributes[attr.HookSourceKey])
	require.Equal(t, "team", event.Attributes[attr.AccountTypeKey])
	require.InDelta(t, 24.0, event.Attributes[attr.ChatAnalysisWorkUnitsKey], 0.0001)
	require.InDelta(t, 1.25, event.Attributes[attr.ChatAnalysisScoredCostKey], 0.0001)
	require.EqualValues(t, int64(4200), event.Attributes[attr.ChatAnalysisScoredTokensKey])
}

func TestPublish_SkipsScoreEventWithoutSessionFacts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newAnalysisFixture(t, "publish_score_event_no_facts")
	roster := testJudges(t, stubNamedJudge{name: "work_units", verdict: stubVerdict(24), err: nil})
	fixture.enableJudge(t, "work_units", 10)

	fixture.seedChat(t, 3, time.Hour)
	_, err := EnqueuePage(ctx, fixture.db, roster, fixture.projectID, EnqueueCursor{}, MaxEnqueuePageSize)
	require.NoError(t, err)
	reserved, _, err := Reserve(ctx, fixture.db, roster, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, 1)

	sink := &captureSink{}
	events := &captureEventSink{}
	publisher := NewPublisher(testenv.NewLogger(t), testenv.NewTracerProvider(t), fixture.db, sink, events, roster)

	result, err := publisher.Publish(ctx, fixture.projectID, []uuid.UUID{reserved[0].ID}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Scored, "missing session facts must not block publication")
	require.Empty(t, events.events(t), "no session facts means no score event")
}
