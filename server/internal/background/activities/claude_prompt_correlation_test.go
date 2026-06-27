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
		ProjectID: uuid.New(),
		ChatID:    uuid.New(),
		SessionID: uuid.NewString(),
	}
}

func fakeClaudePromptMessageRow() activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow {
	return activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow{
		ID:        uuid.New(),
		Content:   "please summarize this repository change",
		CreatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
}

type fakeClaudePromptStore struct {
	rows        []activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow
	backfillErr error
	backfilled  bool
}

func (f *fakeClaudePromptStore) ListUnlinkedClaudeUserMessagesForCorrelation(context.Context, activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationParams) ([]activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow, error) {
	return f.rows, nil
}

func (f *fakeClaudePromptStore) BackfillClaudeUserMessagePromptID(context.Context, activitiesrepo.BackfillClaudeUserMessagePromptIDParams) error {
	f.backfilled = true
	return f.backfillErr
}

type fakeClaudePromptTelemetry struct {
	candidates         []telemetryrepo.ClaudeUserPromptCandidate
	err                error
	waitForContextDone bool
}

func (f *fakeClaudePromptTelemetry) ListClaudeUserPromptCandidatesForCorrelation(ctx context.Context, _ telemetryrepo.ListClaudeUserPromptCandidatesForCorrelationParams) ([]telemetryrepo.ClaudeUserPromptCandidate, error) {
	if f.waitForContextDone {
		<-ctx.Done()
		return nil, fmt.Errorf("context done: %w", ctx.Err())
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.candidates, nil
}
