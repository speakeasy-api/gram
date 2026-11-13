package openrouter

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestForChatCaptureKeepsChatOnlyForCustomerChatTurns(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	cases := []struct {
		name    string
		keyType KeyType
		chatID  uuid.UUID
		records bool
	}{
		{name: "chat key with chat", keyType: KeyTypeChat, chatID: chatID, records: true},
		{name: "default key with chat", keyType: "", chatID: chatID, records: true},
		{name: "internal key with chat", keyType: KeyTypeInternal, chatID: chatID, records: false},
		{name: "chat key without chat", keyType: KeyTypeChat, chatID: uuid.Nil, records: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var req CompletionRequest
			req.KeyType = tc.keyType
			req.ChatID = tc.chatID

			require.Equal(t, tc.records, req.recordsChat())
			if tc.records {
				require.Equal(t, tc.chatID, req.forChatCapture().ChatID)
			} else {
				require.Equal(t, uuid.Nil, req.forChatCapture().ChatID)
			}
			// The original request keeps its chat id for usage and telemetry.
			require.Equal(t, tc.chatID, req.ChatID)
		})
	}
}
