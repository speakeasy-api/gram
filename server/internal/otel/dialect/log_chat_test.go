package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
)

func chatLogRecord(role string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	record := accessorTestRecord(ChatLogScope, "gram.chat.message", append([]*otelv1.InboundLogRecord_KeyValue{
		accessorTestKV("gram.chat.role", role),
		accessorTestKV("gram.chat.id", "chat-id"),
		accessorTestKV("gram.chat.message.id", "message-id"),
		accessorTestKV("gram.chat.message.key", "content-key"),
		accessorTestKV("gram.session.id", "ses_123"),
		accessorTestKV("gram.hook.turn_id", "prompt_01"),
		accessorTestKV("gram.hook.source", "claude-code"),
		accessorTestKV("gen_ai.response.model", "claude-sonnet-4-5"),
		accessorTestKV("user.email", "person@example.invalid"),
		accessorTestKV("gram.external_user.id", "external-id"),
	}, attributes...)...)
	body := "fix the build"
	record.SetBody((&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &body}).Build())
	return record
}

func TestChatLogAppliesOnlyToTheBridgeScope(t *testing.T) {
	t.Parallel()

	selected := ForLog(chatLogRecord("user"))
	fallback, ok := selected.(LogFallback)
	require.True(t, ok)
	require.Equal(t, []LogDialect{ChatLog{}, SemconvLog{}}, fallback.Candidates)

	other := accessorTestRecord("com.anthropic.claude_code.events", "gram.chat.message", accessorTestKV("gram.chat.role", "user"))
	require.False(t, ChatLog{}.AppliesTo(other))
}

func TestChatLogReadsIdentityFromTheBridgeAttributes(t *testing.T) {
	t.Parallel()

	d := ChatLog{}
	cases := []struct {
		role      string
		eventType string
	}{
		{role: "user", eventType: EventTypePrompt},
		{role: "assistant", eventType: EventTypeAPIResponse},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			t.Parallel()
			record := chatLogRecord(tc.role)

			key, eventType, err := d.EventType(record)
			require.NoError(t, err)
			require.Equal(t, "gram.chat.role", key)
			require.Equal(t, tc.eventType, eventType)

			key, name, err := d.EventName(record)
			require.NoError(t, err)
			require.Equal(t, "event_name", key)
			require.Equal(t, "gram.chat.message", name)

			key, subject, err := d.SubjectID(record)
			require.NoError(t, err)
			require.Equal(t, "gram.chat.message.key", key)
			require.Equal(t, "content-key", subject)

			_, session, err := d.SessionID(record)
			require.NoError(t, err)
			require.Equal(t, "ses_123", session)

			_, turn, err := d.TurnID(record)
			require.NoError(t, err)
			require.Equal(t, "prompt_01", turn)

			key, text, err := d.Text(record)
			require.NoError(t, err)
			require.Equal(t, "body", key)
			require.Equal(t, "fix the build", text)

			_, model, err := d.Model(record)
			require.NoError(t, err)
			require.Equal(t, "claude-sonnet-4-5", model)

			_, surface, err := d.Surface(record)
			require.NoError(t, err)
			require.Equal(t, "claude-code", surface)

			_, email, err := d.ExternalUserEmail(record)
			require.NoError(t, err)
			require.Equal(t, "person@example.invalid", email)

			_, externalUserID, err := d.ExternalUserID(record)
			require.NoError(t, err)
			require.Equal(t, "external-id", externalUserID)

			key, provider, err := d.Provider(record)
			require.NoError(t, err)
			require.Empty(t, key)
			require.Empty(t, provider)
		})
	}
}

func TestChatLogLeavesOtherRolesUnclassified(t *testing.T) {
	t.Parallel()

	for _, role := range []string{"tool", "system", ""} {
		key, eventType, err := ChatLog{}.EventType(chatLogRecord(role))
		require.NoError(t, err)
		require.Empty(t, key, role)
		require.Equal(t, EventTypeUnclassified, eventType, role)
	}
}
