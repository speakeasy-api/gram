package businessmemory

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.July, 29, 20, 15, 42, 123, time.UTC)
	id := uuid.MustParse("01984d09-2bc0-7000-8000-000000000001")

	encoded := encodeCursor(createdAt, id)
	gotCreatedAt, gotID, err := decodeCursor(&encoded)

	require.NoError(t, err)
	require.Equal(t, createdAt, *gotCreatedAt)
	require.Equal(t, id, *gotID)
}

func TestDecodeCursorRejectsMalformedValue(t *testing.T) {
	t.Parallel()

	cursor := "not-base64!"
	_, _, err := decodeCursor(&cursor)
	require.Error(t, err)
}
