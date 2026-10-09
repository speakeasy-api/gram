package telemetry

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmployeeSearchCursorRoundTrips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lastSeen int64
		groupKey string
	}{
		{name: "it round-trips an email key", lastSeen: 1_700_000_000_123_456_789, groupKey: "pat.rivera@example.com"},
		{name: "it round-trips a key containing colons", lastSeen: 42, groupKey: "ext:user:7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			key, lastSeen := decodeEmployeeSearchCursor(encodeEmployeeSearchCursor(tc.lastSeen, tc.groupKey))
			require.Equal(t, tc.groupKey, key)
			require.Equal(t, tc.lastSeen, lastSeen)
		})
	}
}

// The cursor is handed to dashboards and held across deploys, so its wire
// format is pinned literally: a round trip alone would pass after coordinated
// changes to both helpers that break cursors already in flight.
func TestEmployeeSearchCursorWireFormat(t *testing.T) {
	t.Parallel()

	cursor := encodeEmployeeSearchCursor(42, "ext:user:7")
	require.Equal(t, "ls1:NDI6ZXh0OnVzZXI6Nw", cursor)

	key, lastSeen := decodeEmployeeSearchCursor("ls1:NDI6ZXh0OnVzZXI6Nw")
	require.Equal(t, "ext:user:7", key)
	require.Equal(t, int64(42), lastSeen)
}

// A cursor that is not a well-formed sealed cursor is the bare group key a
// dashboard was handed before the boundary was sealed. It must resolve to that
// key with no sealed boundary, never to an error.
func TestEmployeeSearchCursorTreatsAnythingElseAsABareGroupKey(t *testing.T) {
	t.Parallel()

	sealedPayload := func(payload string) string {
		return employeeSearchCursorPrefix + base64.RawURLEncoding.EncodeToString([]byte(payload))
	}

	tests := []struct {
		name   string
		cursor string
	}{
		{name: "it keeps an email key", cursor: "pat.rivera@example.com"},
		{name: "it keeps a user id key", cursor: "user-42"},
		{name: "it keeps a prefixed key that is not base64", cursor: employeeSearchCursorPrefix + "not base64!"},
		{name: "it keeps a sealed payload without a separator", cursor: sealedPayload("12345")},
		{name: "it keeps a sealed payload without a key", cursor: sealedPayload("12345:")},
		{name: "it keeps a sealed payload with a non-numeric boundary", cursor: sealedPayload("soon:user-42")},
		{name: "it keeps a sealed payload with a zero boundary", cursor: sealedPayload("0:user-42")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			key, lastSeen := decodeEmployeeSearchCursor(tc.cursor)
			require.Equal(t, tc.cursor, key)
			require.Zero(t, lastSeen)
		})
	}
}
