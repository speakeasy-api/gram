package activities

import (
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/stretchr/testify/require"
)

func TestStripMessageContextRemovesAdapterFraming(t *testing.T) {
	t.Parallel()

	input := "<message-context>\nEventID: abc-123\nUserID: user-9\n</message-context>\n\nHow do I reduce my token usage?"
	require.Equal(t, "How do I reduce my token usage?", stripMessageContext(input))
}

func TestStripMessageContextLeavesPlainTextUntouched(t *testing.T) {
	t.Parallel()

	require.Equal(t, "just a normal message", stripMessageContext("just a normal message"))
}

func TestBuildTitleContextStripsMessageContextFraming(t *testing.T) {
	t.Parallel()

	messages := []repo.ChatMessage{
		{
			Role:    "user",
			Content: "<message-context>\nEventID: evt-1\nUserID: user-1\n</message-context>\n\nWhich agents call the weather tool most often?",
		},
		{
			Role:    "assistant",
			Content: "The travel-planner agent leads with 1,204 calls this week.",
		},
	}

	got := buildTitleContext(messages)

	require.NotContains(t, got, "message-context")
	require.NotContains(t, got, "EventID")
	require.NotContains(t, got, "UserID")
	require.Contains(t, got, "Which agents call the weather tool most often?")
	require.Contains(t, got, "travel-planner agent")
}

func TestBuildTitleContextSkipsPureFramingMessages(t *testing.T) {
	t.Parallel()

	// A turn that is *only* framing (e.g. an MCP auth event with no human text)
	// must not contribute an empty "user: " line to the title context.
	messages := []repo.ChatMessage{
		{
			Role:    "user",
			Content: "<message-context>\nEventType: assistant_mcp_auth_required\nAuthURL: https://example.test/oauth\n</message-context>\n",
		},
	}

	require.Empty(t, strings.TrimSpace(buildTitleContext(messages)))
}
