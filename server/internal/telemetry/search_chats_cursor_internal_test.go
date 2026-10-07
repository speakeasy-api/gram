package telemetry

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatSearchCursorRoundTrips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		start  int64
		chatID string
	}{
		{name: "it round-trips a uuid chat id", start: 1_700_000_000_123_456_789, chatID: "0199a0e4-7c1e-7d4a-9b8e-2f6c1d3e4a5b"},
		{name: "it round-trips a chat id containing colons", start: 42, chatID: "chat:session:7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			chatID, start := decodeChatSearchCursor(encodeChatSearchCursor(tc.start, tc.chatID))
			require.Equal(t, tc.chatID, chatID)
			require.Equal(t, tc.start, start)
		})
	}
}

// A cursor that is not a well-formed sealed cursor is the bare chat id a caller
// was handed before the boundary was sealed. It must resolve to that id with no
// sealed boundary, never to an error.
func TestChatSearchCursorTreatsAnythingElseAsABareChatID(t *testing.T) {
	t.Parallel()

	sealedPayload := func(payload string) string {
		return chatSearchCursorPrefix + base64.RawURLEncoding.EncodeToString([]byte(payload))
	}

	tests := []struct {
		name   string
		cursor string
	}{
		{name: "it keeps a uuid chat id", cursor: "0199a0e4-7c1e-7d4a-9b8e-2f6c1d3e4a5b"},
		{name: "it keeps a prefixed id that is not base64", cursor: chatSearchCursorPrefix + "not base64!"},
		{name: "it keeps a sealed payload without a separator", cursor: sealedPayload("12345")},
		{name: "it keeps a sealed payload without a chat id", cursor: sealedPayload("12345:")},
		{name: "it keeps a sealed payload with a non-numeric boundary", cursor: sealedPayload("soon:chat-42")},
		{name: "it keeps a sealed payload with a zero boundary", cursor: sealedPayload("0:chat-42")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			chatID, start := decodeChatSearchCursor(tc.cursor)
			require.Equal(t, tc.cursor, chatID)
			require.Zero(t, start)
		})
	}
}
