package chat

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMessageTextKeyIgnoresWhitespaceAndLineEndings(t *testing.T) {
	t.Parallel()
	want := MessageTextKey("session", "user", "fix the build\nthen rerun")
	require.NotEmpty(t, want)
	require.Len(t, want, 64)
	for _, variant := range []string{
		"  fix the build\nthen rerun  ",
		"fix the build\r\nthen rerun",
		"\n\nfix the build\nthen rerun\n",
	} {
		require.Equal(t, want, MessageTextKey("session", "user", variant), "%q", variant)
	}
	require.NotEqual(t, want, MessageTextKey("session", "assistant", "fix the build\nthen rerun"))
	require.NotEqual(t, want, MessageTextKey("other", "user", "fix the build\nthen rerun"))
}

func TestMessageTextKeyStripsLeadingHarnessFraming(t *testing.T) {
	t.Parallel()
	want := MessageTextKey("session", "user", "what changed?")
	framed := "<system-reminder>\nThe user opened a file.\n</system-reminder>\n\nwhat changed?"
	require.Equal(t, want, MessageTextKey("session", "user", framed))
}

func TestMessageTextKeyIsEmptyWhenOnlyFramingRemains(t *testing.T) {
	t.Parallel()
	require.Empty(t, MessageTextKey("session", "user", "<system-reminder>context only</system-reminder>"))
	require.Empty(t, MessageTextKey("session", "user", "   \r\n "))
	require.Empty(t, MessageTextKey("session", "user", ""))
}

func TestAgentSessionIDPrefersWhatTheLaneAsserted(t *testing.T) {
	t.Parallel()
	chatID := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	require.Equal(t, "ses_123", AgentSessionID(chatID, "anthropic-inference:"+chatID.String(), "ses_123"))
	require.Equal(t, "ses_hook", AgentSessionID(chatID, "ses_hook", ""))
	require.Equal(t, chatID.String(), AgentSessionID(chatID, "anthropic-inference:"+chatID.String(), ""))
	require.Equal(t, chatID.String(), AgentSessionID(chatID, "", ""))
}
