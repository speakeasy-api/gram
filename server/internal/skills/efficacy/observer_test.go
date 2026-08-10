package efficacy

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type recordingSignaler struct {
	err     error
	signals []uuid.UUID
}

func (r *recordingSignaler) Signal(_ context.Context, projectID uuid.UUID) error {
	r.signals = append(r.signals, projectID)
	return r.err
}

func TestObserver_StoredMessagesWakeProjectAndRepeatSafely(t *testing.T) {
	t.Parallel()

	signaler := &recordingSignaler{err: nil, signals: nil}
	observer := NewObserver(testenv.NewLogger(t), signaler)
	projectID := uuid.New()

	observer.OnMessagesStored(t.Context(), projectID)
	observer.OnMessagesStored(t.Context(), projectID)

	require.Equal(t, []uuid.UUID{projectID, projectID}, signaler.signals,
		"every durable persistence callback wakes the project; wakes are idempotent so repeats are safe")
}

func TestObserver_SignalFailureIsLoggedAndNotPropagated(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	signaler := &recordingSignaler{err: errors.New("coordinator unreachable"), signals: nil}
	observer := NewObserver(slog.New(slog.NewTextHandler(&logs, nil)), signaler)
	projectID := uuid.New()

	require.NotPanics(t, func() { observer.OnMessagesStored(t.Context(), projectID) },
		"a wake failure must never disturb the persistence path")

	logged := logs.String()
	require.Contains(t, logged, projectID.String())
	require.Contains(t, logged, "coordinator unreachable")
}
