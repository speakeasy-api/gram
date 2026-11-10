package sessionlist_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/sessionlist"
	"github.com/stretchr/testify/require"
)

func TestCursorPreservesTimestampAndScope(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 1, 12, 0, 0, 123456000, time.UTC)
	id := uuid.New()
	fingerprint := sessionlist.Fingerprint("organization", "gateway", "expiry")
	cursor := sessionlist.Encode(at, id, fingerprint)
	decodedTime, decodedID, err := sessionlist.Decode(&cursor, fingerprint)
	require.NoError(t, err)
	require.Equal(t, at, decodedTime.Time)
	require.Equal(t, id, decodedID.UUID)
	for _, invalid := range []string{cursor, "not-base64", "eA"} {
		_, _, err := sessionlist.Decode(&invalid, sessionlist.Fingerprint("different-organization"))
		require.Error(t, err)
	}
}
