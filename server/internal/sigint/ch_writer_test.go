package sigint_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/server/internal/sigint"
	"github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type readingInsertCapture struct {
	rows []chrepo.ReadingRow
	err  error
}

func (c *readingInsertCapture) InsertReadings(_ context.Context, rows []chrepo.ReadingRow) error {
	c.rows = append(c.rows, rows...)
	return c.err
}

func writerReading(t *testing.T) *sigintv1.Reading {
	t.Helper()
	at := time.Now().UTC().Format(time.RFC3339Nano)
	return sigintv1.Reading_builder{
		Id: new(uuid.NewString()), EvaluationAttemptId: new(uuid.NewString()),
		TriggerKind: new("stream"), EvaluationRequestedAt: new(at),
		OrganizationId: new("example-organization"), ProjectId: new(uuid.NewString()),
		SensorId: new(uuid.NewString()), SensorSlug: new("example-sensor"),
		DefinitionHash: new("example-definition"), EvaluatedAt: new(at),
		Event: sigintv1.Reading_Event_builder{Kind: new("example.event"), Id: new("event-1"), OccurredAt: new(at)}.Build(),
		MultiLabel: sigintv1.Reading_MultiLabel_builder{Signals: []*sigintv1.Reading_Probability{
			sigintv1.Reading_Probability_builder{SignalId: new(uuid.NewString()), SignalSlug: new("example-signal"), Probability: new(0.5)}.Build(),
		}}.Build(),
	}.Build()
}

func TestReadingCHWriterPreservesDuplicateDeliveriesAndDropsPoison(t *testing.T) {
	t.Parallel()
	capture := &readingInsertCapture{}
	writer := sigint.NewReadingCHWriter(testenv.NewLogger(t), capture)
	message := writerReading(t)
	require.NoError(t, writer.HandleBatch(t.Context(), []*sigintv1.Reading{message, nil, message}, nil))
	require.Len(t, capture.rows, 2)
	require.Equal(t, capture.rows[0].ID, capture.rows[1].ID)
	require.Equal(t, capture.rows[0].EvaluationAttemptID, capture.rows[1].EvaluationAttemptID)
}

func TestReadingCHWriterRetriesStorageFailures(t *testing.T) {
	t.Parallel()
	storageErr := errors.New("storage unavailable")
	capture := &readingInsertCapture{err: storageErr}
	writer := sigint.NewReadingCHWriter(testenv.NewLogger(t), capture)
	require.ErrorIs(t, writer.HandleBatch(t.Context(), []*sigintv1.Reading{writerReading(t)}, nil), storageErr)
}

func TestOutcomeCHWriterLandsOutcomesAndDropsPoison(t *testing.T) {
	t.Parallel()
	capture := &readingInsertCapture{}
	writer := sigint.NewOutcomeCHWriter(testenv.NewLogger(t), capture)
	reading := writerReading(t)
	outcome := sigintv1.Outcome_builder{
		Id: new(reading.GetId()), EvaluationAttemptId: new(uuid.NewString()),
		TriggerKind: new(reading.GetTriggerKind()), EvaluationRequestedAt: new(reading.GetEvaluationRequestedAt()),
		OrganizationId: new(reading.GetOrganizationId()), ProjectId: new(reading.GetProjectId()),
		SensorId: new(reading.GetSensorId()), SensorSlug: new(reading.GetSensorSlug()),
		EvaluatedAt: new(reading.GetEvaluatedAt()), Status: new(sigintv1.Outcome_STATUS_NOT_APPLICABLE),
		Event: sigintv1.Outcome_Event_builder{Kind: new("example.event"), Id: new("event-1"), OccurredAt: new(reading.GetEvent().GetOccurredAt())}.Build(),
	}.Build()
	require.NoError(t, writer.HandleBatch(t.Context(), []*sigintv1.Outcome{outcome, nil}, nil))
	require.Len(t, capture.rows, 1)
	require.Equal(t, chrepo.StatusNotApplicable, capture.rows[0].Status)
	require.Equal(t, reading.GetId(), capture.rows[0].ID.String())
}
