package activities

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestIsAcceptedClaudePromptCandidateAcceptsExactMatch(t *testing.T) {
	t.Parallel()

	require.True(t, isAcceptedClaudePromptCandidate([]telemetryrepo.ClaudeUserPromptCandidate{{
		PromptID:   "prompt-1",
		Similarity: 1,
		IsExact:    true,
	}}))
}

func TestIsAcceptedClaudePromptCandidateRejectsLowSimilarity(t *testing.T) {
	t.Parallel()

	require.False(t, isAcceptedClaudePromptCandidate([]telemetryrepo.ClaudeUserPromptCandidate{{
		PromptID:   "prompt-1",
		Similarity: 0.94,
		IsExact:    false,
	}}))
}

func TestIsAcceptedClaudePromptCandidateRejectsAmbiguousFuzzyMatch(t *testing.T) {
	t.Parallel()

	require.False(t, isAcceptedClaudePromptCandidate([]telemetryrepo.ClaudeUserPromptCandidate{
		{PromptID: "prompt-1", Similarity: 0.97, IsExact: false},
		{PromptID: "prompt-2", Similarity: 0.96, IsExact: false},
	}))
}

func TestIsAcceptedClaudePromptCandidateAcceptsConfidentFuzzyMatch(t *testing.T) {
	t.Parallel()

	require.True(t, isAcceptedClaudePromptCandidate([]telemetryrepo.ClaudeUserPromptCandidate{
		{PromptID: "prompt-1", Similarity: 0.98, IsExact: false},
		{PromptID: "prompt-2", Similarity: 0.95, IsExact: false},
	}))
}

func TestCorrelateClaudePromptsReturnsNonTimeoutCandidateErrors(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("clickhouse unavailable")
	act := newTestCorrelateClaudePrompts(t, &fakeClaudePromptStore{
		rows: []activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow{fakeClaudePromptMessageRow()},
	}, &fakeClaudePromptTelemetry{
		err: queryErr,
	})

	result, err := act.Do(context.Background(), fakeCorrelateClaudePromptsArgs())

	require.Nil(t, result)
	require.ErrorIs(t, err, queryErr)
	require.ErrorContains(t, err, "find Claude user prompt match")
}

func TestCorrelateClaudePromptsSkipsPerMessageCandidateTimeout(t *testing.T) {
	t.Parallel()

	store := &fakeClaudePromptStore{
		rows: []activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow{fakeClaudePromptMessageRow()},
	}
	act := newTestCorrelateClaudePrompts(t, store, &fakeClaudePromptTelemetry{
		waitForContextDone: true,
	})

	result, err := act.Do(context.Background(), fakeCorrelateClaudePromptsArgs())

	require.NoError(t, err)
	require.False(t, result.HasMore)
	require.False(t, store.backfilled)
}

func TestCorrelateClaudePromptsReturnsBackfillErrors(t *testing.T) {
	t.Parallel()

	backfillErr := errors.New("postgres unavailable")
	store := &fakeClaudePromptStore{
		rows:        []activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow{fakeClaudePromptMessageRow()},
		backfillErr: backfillErr,
	}
	act := newTestCorrelateClaudePrompts(t, store, &fakeClaudePromptTelemetry{
		candidates: []telemetryrepo.ClaudeUserPromptCandidate{{
			PromptID:      "prompt-1",
			Prompt:        "please summarize this repository change",
			EventSequence: 1,
			TimeUnixNano:  time.Now().UTC().UnixNano(),
			Similarity:    1,
			IsExact:       true,
		}},
	})

	result, err := act.Do(context.Background(), fakeCorrelateClaudePromptsArgs())

	require.Nil(t, result)
	require.ErrorIs(t, err, backfillErr)
	require.ErrorContains(t, err, "backfill Claude prompt ID")
	require.True(t, store.backfilled)
}

func TestCorrelateClaudePromptsAdvancesMessageCursorWithoutMatches(t *testing.T) {
	t.Parallel()

	rows := make([]activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow, 0, claudePromptCorrelationMessageBatchSize+1)
	for i := 1; i <= claudePromptCorrelationMessageBatchSize+1; i++ {
		rows = append(rows, fakeClaudePromptMessageRowWithSeq(int64(i)))
	}
	store := &fakeClaudePromptStore{rows: rows}
	act := newTestCorrelateClaudePrompts(t, store, &fakeClaudePromptTelemetry{})

	result, err := act.Do(context.Background(), fakeCorrelateClaudePromptsArgs())

	require.NoError(t, err)
	require.True(t, result.HasMore)
	require.Equal(t, int64(claudePromptCorrelationMessageBatchSize), result.AfterMessageSeq)
	require.False(t, store.backfilled)
	require.Equal(t, int32(claudePromptCorrelationMessageBatchSize+1), store.lastListParams.LimitCount)
}

func TestCorrelateClaudePromptsCarriesInputCursors(t *testing.T) {
	t.Parallel()

	store := &fakeClaudePromptStore{
		rows: []activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow{fakeClaudePromptMessageRowWithSeq(12)},
	}
	telemetry := &fakeClaudePromptTelemetry{
		candidates: []telemetryrepo.ClaudeUserPromptCandidate{{
			PromptID:      "prompt-2",
			Prompt:        "please summarize this repository change",
			EventSequence: 8,
			TimeUnixNano:  80,
			Similarity:    1,
			IsExact:       true,
		}},
	}
	act := newTestCorrelateClaudePrompts(t, store, telemetry)
	args := fakeCorrelateClaudePromptsArgs()
	args.AfterMessageSeq = 10
	args.AfterEventSequence = 7
	args.AfterEventTimeUnixNano = 70

	result, err := act.Do(context.Background(), args)

	require.NoError(t, err)
	require.False(t, result.HasMore)
	require.Equal(t, int64(12), result.AfterMessageSeq)
	require.Equal(t, int64(8), result.AfterEventSequence)
	require.Equal(t, int64(80), result.AfterEventTimeUnixNano)
	require.Equal(t, int64(10), store.lastListParams.AfterMessageSeq)
	require.Equal(t, int64(7), telemetry.lastCandidateParams.AfterEventSequence)
	require.Equal(t, int64(70), telemetry.lastCandidateParams.AfterEventTimeUnixNano)
}

func newTestCorrelateClaudePrompts(t *testing.T, store claudePromptCorrelationStore, telemetry claudePromptCorrelationTelemetry) *CorrelateClaudePrompts {
	t.Helper()

	return &CorrelateClaudePrompts{
		logger:       testenv.NewLogger(t),
		store:        store,
		telemetry:    telemetry,
		matchTimeout: 10 * time.Millisecond,
	}
}

func fakeCorrelateClaudePromptsArgs() CorrelateClaudePromptsArgs {
	return CorrelateClaudePromptsArgs{
		ProjectID:              uuid.New(),
		ChatID:                 uuid.New(),
		SessionID:              uuid.NewString(),
		AfterMessageSeq:        0,
		AfterEventSequence:     0,
		AfterEventTimeUnixNano: 0,
	}
}

func fakeClaudePromptMessageRow() activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow {
	return fakeClaudePromptMessageRowWithSeq(1)
}

func fakeClaudePromptMessageRowWithSeq(seq int64) activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow {
	return activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow{
		ID:        uuid.New(),
		Seq:       seq,
		Content:   "please summarize this repository change",
		CreatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
}

type fakeClaudePromptStore struct {
	rows           []activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow
	backfillErr    error
	backfilled     bool
	lastListParams activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationParams
}

func (f *fakeClaudePromptStore) ListUnlinkedClaudeUserMessagesForCorrelation(_ context.Context, params activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationParams) ([]activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow, error) {
	f.lastListParams = params
	return f.rows, nil
}

func (f *fakeClaudePromptStore) BackfillClaudeUserMessagePromptID(context.Context, activitiesrepo.BackfillClaudeUserMessagePromptIDParams) error {
	f.backfilled = true
	return f.backfillErr
}

type fakeClaudePromptTelemetry struct {
	candidates          []telemetryrepo.ClaudeUserPromptCandidate
	err                 error
	waitForContextDone  bool
	lastCandidateParams telemetryrepo.ListClaudeUserPromptCandidatesForCorrelationParams
}

func (f *fakeClaudePromptTelemetry) ListClaudeUserPromptCandidatesForCorrelation(ctx context.Context, params telemetryrepo.ListClaudeUserPromptCandidatesForCorrelationParams) ([]telemetryrepo.ClaudeUserPromptCandidate, error) {
	f.lastCandidateParams = params
	if f.waitForContextDone {
		<-ctx.Done()
		return nil, fmt.Errorf("context done: %w", ctx.Err())
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.candidates, nil
}
