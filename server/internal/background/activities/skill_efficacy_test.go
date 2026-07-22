package activities_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
)

// stubEfficacyPublisher answers one publication with a pinned outcome and
// records the batch it was handed.
type stubEfficacyPublisher struct {
	result     efficacy.PublishResult
	err        error
	ids        []uuid.UUID
	heartbeats int
}

func (s *stubEfficacyPublisher) Publish(_ context.Context, _ uuid.UUID, ids []uuid.UUID, heartbeat func()) (efficacy.PublishResult, error) {
	s.ids = ids
	if heartbeat != nil {
		heartbeat()
		s.heartbeats++
	}
	return s.result, s.err
}

// stubEfficacySignaler records the projects it was asked to wake and can refuse.
type stubEfficacySignaler struct {
	err      error
	projects []uuid.UUID
}

func (s *stubEfficacySignaler) Signal(_ context.Context, projectID uuid.UUID) error {
	s.projects = append(s.projects, projectID)
	return s.err
}

func TestSkillEfficacyScorerPublishesABatchAndReportsWhatItDid(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	publisher := &stubEfficacyPublisher{
		result: efficacy.PublishResult{
			Loaded: 2, AlreadyPublished: 1, Scored: 2, ModelFailures: 0, Failed: 0, Retryable: 0,
		},
		err: nil,
		ids: nil, heartbeats: 0,
	}
	scorer := activities.NewSkillEfficacyScorer(nil, nil, publisher, &stubEfficacySignaler{err: nil, projects: nil})

	result, err := scorer.PublishSkillEfficacyBatch(t.Context(), activities.PublishSkillEfficacyBatchParams{
		ProjectID: projectID,
		IDs:       ids,
	})

	require.NoError(t, err)
	require.Equal(t, ids, publisher.ids)
	require.Equal(t, publisher.result, *result)
}

func TestSkillEfficacyScorerRetriesNonTerminalModelFailures(t *testing.T) {
	t.Parallel()
	// A model failure is charged to its own evaluation's attempt counter inside
	// the publication, so it reaches the workflow as a count rather than as an
	// error that would retry the whole batch and pay for inference again.
	publisher := &stubEfficacyPublisher{
		result: efficacy.PublishResult{
			Loaded: 3, AlreadyPublished: 0, Scored: 1, ModelFailures: 1, Failed: 1, Retryable: 0,
		},
		err: nil,
		ids: nil, heartbeats: 0,
	}
	scorer := activities.NewSkillEfficacyScorer(nil, nil, publisher, &stubEfficacySignaler{err: nil, projects: nil})

	result, err := scorer.PublishSkillEfficacyBatch(t.Context(), activities.PublishSkillEfficacyBatchParams{
		ProjectID: uuid.New(),
		IDs:       []uuid.UUID{uuid.New(), uuid.New(), uuid.New()},
	})

	require.ErrorIs(t, err, efficacy.ErrRetryable)
	require.Nil(t, result)
}

func TestSkillEfficacyScorerCompletesAfterTerminalModelFailure(t *testing.T) {
	t.Parallel()
	publisher := &stubEfficacyPublisher{
		result: efficacy.PublishResult{
			Loaded: 1, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 1, Retryable: 0,
		},
		err: nil,
		ids: nil, heartbeats: 0,
	}
	scorer := activities.NewSkillEfficacyScorer(nil, nil, publisher, &stubEfficacySignaler{err: nil, projects: nil})

	result, err := scorer.PublishSkillEfficacyBatch(t.Context(), activities.PublishSkillEfficacyBatchParams{
		ProjectID: uuid.New(),
		IDs:       []uuid.UUID{uuid.New()},
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
}

func TestSkillEfficacyScorerRetriesInfrastructureFailures(t *testing.T) {
	t.Parallel()
	publisher := &stubEfficacyPublisher{
		result: efficacy.PublishResult{
			Loaded: 1, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 1,
		},
		err: fmt.Errorf("insert skill efficacy score: %w: %w", efficacy.ErrRetryable, errors.New("clickhouse unavailable")),
		ids: nil, heartbeats: 0,
	}
	scorer := activities.NewSkillEfficacyScorer(nil, nil, publisher, &stubEfficacySignaler{err: nil, projects: nil})

	_, err := scorer.PublishSkillEfficacyBatch(t.Context(), activities.PublishSkillEfficacyBatchParams{
		ProjectID: uuid.New(),
		IDs:       []uuid.UUID{uuid.New()},
	})

	require.Error(t, err)
	require.ErrorIs(t, err, efficacy.ErrRetryable)
	var applicationErr *temporal.ApplicationError
	require.False(t, errors.As(err, &applicationErr) && applicationErr.NonRetryable(),
		"the same reserved rows are worth another attempt")
}

func TestSkillEfficacyScorerDoesNotRetryFailuresARetryWouldRepeat(t *testing.T) {
	t.Parallel()
	// Nothing the domain declines to class as infrastructure changes on a retry:
	// the batch is re-read from the same project-scoped rows and reaches the same
	// conclusion, so burning the policy's attempts on it buys nothing.
	publisher := &stubEfficacyPublisher{
		result: efficacy.PublishResult{
			Loaded: 2, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 0,
		},
		err: errors.New("skill efficacy guard window: project resolved evaluations across organizations"),
		ids: nil, heartbeats: 0,
	}
	scorer := activities.NewSkillEfficacyScorer(nil, nil, publisher, &stubEfficacySignaler{err: nil, projects: nil})

	_, err := scorer.PublishSkillEfficacyBatch(t.Context(), activities.PublishSkillEfficacyBatchParams{
		ProjectID: uuid.New(),
		IDs:       []uuid.UUID{uuid.New(), uuid.New()},
	})

	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, err, &applicationErr)
	require.True(t, applicationErr.NonRetryable())
}

func TestSkillEfficacyScorerSignalsTheProjectCoordinator(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	signaler := &stubEfficacySignaler{err: nil, projects: nil}
	scorer := activities.NewSkillEfficacyScorer(nil, nil, &stubEfficacyPublisher{
		result: efficacy.PublishResult{Loaded: 0, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 0},
		err:    nil,
		ids:    nil, heartbeats: 0,
	}, signaler)

	require.NoError(t, scorer.SignalSkillEfficacyCoordinator(t.Context(), activities.SignalSkillEfficacyCoordinatorParams{
		ProjectID: projectID,
	}))
	require.Equal(t, []uuid.UUID{projectID}, signaler.projects)

	signaler.err = errors.New("temporal unavailable")
	require.ErrorContains(t, scorer.SignalSkillEfficacyCoordinator(t.Context(), activities.SignalSkillEfficacyCoordinatorParams{
		ProjectID: projectID,
	}), "signal skill efficacy coordinator")
}
