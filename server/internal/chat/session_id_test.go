package chat_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
)

func TestSessionIDToChatID(t *testing.T) {
	t.Parallel()

	t.Run("uuid session id is the chat id", func(t *testing.T) {
		t.Parallel()

		sessionID := uuid.NewString()
		require.Equal(t, uuid.MustParse(sessionID), chat.SessionIDToChatID(sessionID))
	})

	t.Run("non-uuid session id hashes into the namespace", func(t *testing.T) {
		t.Parallel()

		expected := uuid.NewSHA1(uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"), []byte("claude-session-1"))
		require.Equal(t, expected, chat.SessionIDToChatID("claude-session-1"))
	})

	t.Run("distinct session ids do not collide", func(t *testing.T) {
		t.Parallel()

		require.NotEqual(t, chat.SessionIDToChatID("session"), chat.SessionIDToChatID("session-2"))
	})
}
