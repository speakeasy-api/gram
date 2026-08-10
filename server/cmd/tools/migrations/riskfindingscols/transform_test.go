package riskfindingscols

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTransformPassesThrough(t *testing.T) {
	t.Parallel()

	tf := NewTransformer()
	createdAt := time.Now().UTC()
	messageCreatedAt := createdAt.Add(-time.Hour)
	in := SourceRow{
		ID:               uuid.Must(uuid.NewV7()),
		CreatedAt:        createdAt,
		MessageCreatedAt: messageCreatedAt,
		AssistantID:      uuid.NewString(),
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, in.ID, out[0].ID)
	require.True(t, createdAt.Equal(out[0].CreatedAt))
	require.True(t, messageCreatedAt.Equal(out[0].MessageCreatedAt))
	require.Equal(t, in.AssistantID, out[0].AssistantID)
}

func TestTransformZeroMessageTimeFallsBackToCreatedAt(t *testing.T) {
	t.Parallel()

	// A zero MessageCreatedAt must never reach ClickHouse: it falls back to
	// the finding's created_at, matching both the source's SQL COALESCE and
	// the ClickHouse column DEFAULT.
	tf := NewTransformer()
	createdAt := time.Now().UTC()
	in := SourceRow{
		ID:               uuid.Must(uuid.NewV7()),
		CreatedAt:        createdAt,
		MessageCreatedAt: time.Time{},
		AssistantID:      "",
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.True(t, createdAt.Equal(out[0].MessageCreatedAt))
	require.Empty(t, out[0].AssistantID)
}
