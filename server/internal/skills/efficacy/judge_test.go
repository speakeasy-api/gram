package efficacy

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func testRoster(t *testing.T, judge *Judge) *analysis.Judges {
	t.Helper()

	roster, err := analysis.NewJudges(judge)
	require.NoError(t, err)
	return roster
}

func TestEnqueueUnitsPageFoldsSessionsAndStampsActivations(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newEfficacyFixture(t, "efficacy_units")
	fixture.enableJudge(t, 10)

	judge := NewJudge(testenv.NewLogger(t), testenv.NewTracerProvider(t), fixture.db, &captureSkillSink{}, &stubCompletionClient{response: "{}"}, judgeLimiter(t))
	roster := testRoster(t, judge)

	now := time.Now().UTC()
	// A live session with two activations, a session whose chat was deleted,
	// and one whose chat never arrived.
	liveChat := fixture.seedChat(t, "session-live", 3, time.Hour)
	fixture.observe(t, "session-live", "claude-code", now.Add(-2*time.Hour))
	fixture.observe(t, "session-live", "claude-code", now.Add(-90*time.Minute))

	deletedChat := fixture.seedChat(t, "session-deleted", 1, time.Hour)
	fixture.observe(t, "session-deleted", "claude-code", now.Add(-2*time.Hour))
	fixture.deleteChat(t, deletedChat)

	fixture.observe(t, "session-absent", "claude-code", now.Add(-2*time.Hour))

	result, err := analysis.EnqueuePage(ctx, fixture.db, roster, fixture.projectID, JudgeName, nil, analysis.MaxEnqueuePageSize)
	require.NoError(t, err)
	require.Equal(t, 4, result.Scanned)
	require.True(t, result.Exhausted)

	pending := fixture.pendingUnits(t)
	require.Len(t, pending, 1, "only the live session becomes a unit")
	require.Equal(t, liveChat, pending[0].ChatID)
	require.Equal(t, "session-live", pending[0].SessionID)
	require.Equal(t, JudgeName, pending[0].Judge)

	// The next walk from the head sees only the absent-chat activation: the
	// live session's activations are stamped and the deleted session's retired.
	again, err := analysis.EnqueuePage(ctx, fixture.db, roster, fixture.projectID, JudgeName, nil, analysis.MaxEnqueuePageSize)
	require.NoError(t, err)
	require.Equal(t, 1, again.Scanned)
	require.Len(t, fixture.pendingUnits(t), 1)
}

func TestSkillEfficacyJudgeEndToEnd(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newEfficacyFixture(t, "efficacy_e2e")
	fixture.enableJudge(t, 10)

	skillSink := &captureSkillSink{}
	client := &stubCompletionClient{response: `{"verdicts":[{"index":0,"score":0.8,"rationale":"followed closely","est_turns_saved":3,"est_minutes_saved":10,"roi_confidence":"med","flags":[]}]}`}
	judge := NewJudge(testenv.NewLogger(t), testenv.NewTracerProvider(t), fixture.db, skillSink, client, judgeLimiter(t))
	roster := testRoster(t, judge)

	now := time.Now().UTC()
	chatID := fixture.seedChat(t, "session-e2e", 3, time.Hour)
	fixture.observe(t, "session-e2e", "claude-code", now.Add(-2*time.Hour))

	_, err := analysis.EnqueuePage(ctx, fixture.db, roster, fixture.projectID, JudgeName, nil, analysis.MaxEnqueuePageSize)
	require.NoError(t, err)

	reserved, _, err := analysis.Reserve(ctx, fixture.db, roster, fixture.projectID, analysis.PendingCursor{}, analysis.MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, 1)

	analysisSink := &captureAnalysisSink{}
	publisher := analysis.NewPublisher(testenv.NewLogger(t), testenv.NewTracerProvider(t), fixture.db, analysisSink, roster)

	result, err := publisher.Publish(ctx, fixture.projectID, []uuid.UUID{reserved[0].ID}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Scored)

	// One skill row in the efficacy sink, keyed deterministically off the
	// evaluation so retries collapse to the same logical event.
	skillSink.mu.Lock()
	skillRows := skillSink.inserted
	skillSink.mu.Unlock()
	require.Len(t, skillRows, 1)
	require.Equal(t, uuid.NewSHA1(reserved[0].ID, []byte(fixture.skillVersionID.String()+"/dev")), skillRows[0].ID)
	require.Equal(t, fixture.organizationID, skillRows[0].OrganizationID)
	require.Equal(t, "session-e2e", skillRows[0].SessionID)
	require.Equal(t, chatID.String(), skillRows[0].GramChatID)
	require.Equal(t, fixture.skillVersionID, skillRows[0].SkillVersionID)
	require.Equal(t, "dev", skillRows[0].Surface)
	require.InDelta(t, 0.8, skillRows[0].Score, 0.0001)
	require.Equal(t, JudgePromptVersion, skillRows[0].JudgePromptVersion)

	// The pipeline's own sink row carries the session-level summary.
	analysisSink.mu.Lock()
	summaryRows := analysisSink.inserted
	analysisSink.mu.Unlock()
	require.Len(t, summaryRows, 1)
	require.Equal(t, reserved[0].ID, summaryRows[0].ID)
	require.Equal(t, JudgeName, summaryRows[0].Judge)
	require.InDelta(t, 0.8, summaryRows[0].Score, 0.0001)
	require.Equal(t, 1, client.calls)
}

func TestJudgeSkipsModelWhenAllRowsPublished(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fixture := newEfficacyFixture(t, "efficacy_dedup")
	fixture.enableJudge(t, 10)

	now := time.Now().UTC()
	fixture.seedChat(t, "session-dedup", 2, time.Hour)
	fixture.observe(t, "session-dedup", "claude-code", now.Add(-2*time.Hour))

	client := &stubCompletionClient{response: "must not be called"}
	skillSink := &captureSkillSink{}
	judge := NewJudge(testenv.NewLogger(t), testenv.NewTracerProvider(t), fixture.db, skillSink, client, judgeLimiter(t))
	roster := testRoster(t, judge)

	_, err := analysis.EnqueuePage(ctx, fixture.db, roster, fixture.projectID, JudgeName, nil, analysis.MaxEnqueuePageSize)
	require.NoError(t, err)
	reserved, _, err := analysis.Reserve(ctx, fixture.db, roster, fixture.projectID, analysis.PendingCursor{}, analysis.MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, 1)

	// Every sink row for the unit already exists: the judge must finish without
	// paying for inference again.
	skillSink.existing = []string{uuid.NewSHA1(reserved[0].ID, []byte(fixture.skillVersionID.String()+"/dev")).String()}

	analysisSink := &captureAnalysisSink{}
	publisher := analysis.NewPublisher(testenv.NewLogger(t), testenv.NewTracerProvider(t), fixture.db, analysisSink, roster)
	result, err := publisher.Publish(ctx, fixture.projectID, []uuid.UUID{reserved[0].ID}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Scored)
	require.Zero(t, client.calls)
}
