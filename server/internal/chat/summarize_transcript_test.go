package chat

import (
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/stretchr/testify/require"
)

func TestBuildSummarizeTranscriptStripsLeadingEnvelopes(t *testing.T) {
	t.Parallel()

	messages := []repo.ChatMessage{
		{
			Role:    "user",
			Content: "<message-context>\nEventID: evt-1\nUserID: user-1\n</message-context>\n\nPlease deploy the API to staging",
		},
		{
			Role:    "assistant",
			Content: "<notification>background task completed</notification>\nDeployed revision 42 successfully.",
		},
	}

	got := buildSummarizeTranscript(messages)

	require.NotContains(t, got, "message-context")
	require.NotContains(t, got, "notification")
	require.NotContains(t, got, "EventID")
	require.Contains(t, got, "Please deploy the API to staging")
	require.Contains(t, got, "Deployed revision 42 successfully.")
}

func TestBuildSummarizeTranscriptSkipsPureEnvelopeMessages(t *testing.T) {
	t.Parallel()

	messages := []repo.ChatMessage{
		{
			Role:    "user",
			Content: "<message-context>\nEventType: assistant_mcp_auth_required\n</message-context>\n",
		},
	}

	require.Empty(t, strings.TrimSpace(buildSummarizeTranscript(messages)))
}
