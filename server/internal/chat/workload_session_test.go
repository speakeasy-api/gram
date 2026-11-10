package chat

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestKitCompletionSessionID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body, session string }{
		{"opaque ACP session", `{"user":"kit-session"}`, "kit-session"},
		{"UUID ACP session", `{"user":"550e8400-e29b-41d4-a716-446655440000"}`, "550e8400-e29b-41d4-a716-446655440000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, err := kitCompletionSessionID([]byte(tc.body))
			require.NoError(t, err)
			require.Equal(t, SessionIDToChatID(tc.session), id)
		})
	}
	for _, body := range []string{`{}`, `{"user":""}`, `{"user":null}`, `{"user":42}`, `{"metadata":{"session_id":"untrusted"}}`, `invalid`} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			id, err := kitCompletionSessionID([]byte(body))
			require.Error(t, err)
			require.Equal(t, uuid.Nil, id)
		})
	}
}
