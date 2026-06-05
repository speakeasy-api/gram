package activities

import (
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/stretchr/testify/require"
)

func TestStripLeadingEnvelopesRemovesAssistantRuntimeFraming(t *testing.T) {
	t.Parallel()

	input := "<message-context>\nEventID: abc-123\nUserID: user-9\n</message-context>\n\nHow do I reduce my token usage?"
	require.Equal(t, "How do I reduce my token usage?", stripLeadingEnvelopes(input))
}

// Other harnesses prepend their own envelopes (e.g. Claude Code background
// tasks return <notification>…</notification>); the strip is generic so the
// title model isn't polluted by envelopes we don't control.
func TestStripLeadingEnvelopesRemovesForeignEnvelope(t *testing.T) {
	t.Parallel()

	input := "<notification>background task completed</notification>\nThe migration finished without errors."
	require.Equal(t, "The migration finished without errors.", stripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesRemovesMultipleLeadingBlocks(t *testing.T) {
	t.Parallel()

	input := "<message-context>\nEventID: e1\n</message-context>\n<notification>done</notification>\n\nWhat changed?"
	require.Equal(t, "What changed?", stripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesLeavesPlainTextUntouched(t *testing.T) {
	t.Parallel()

	require.Equal(t, "just a normal message", stripLeadingEnvelopes("just a normal message"))
}

// Only known harness envelopes are stripped. A message that legitimately opens
// with user markup (a <details> block, a pasted snippet, etc.) must survive — a
// fully-generic <tag>…</tag> match would eat it and distort the title.
func TestStripLeadingEnvelopesLeavesUnknownLeadingTag(t *testing.T) {
	t.Parallel()

	input := "<details>my setup</details>\n\nwhy does the build fail?"
	require.Equal(t, input, stripLeadingEnvelopes(input))
}

// The regex is anchored to the start of the message: only leading framing is
// removed. A user who happens to type tags mid-message keeps that text —
// stripping it would distort the title.
func TestStripLeadingEnvelopesOnlyStripsLeadingBlock(t *testing.T) {
	t.Parallel()

	input := "why does my agent emit <message-context>foo</message-context> in its output?"
	require.Equal(t, input, stripLeadingEnvelopes(input))
}

func TestBuildTitleContextStripsLeadingEnvelopeFraming(t *testing.T) {
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

func TestBuildTitleContextSkipsPureEnvelopeMessages(t *testing.T) {
	t.Parallel()

	// A turn that is *only* an envelope (e.g. an MCP auth event with no human
	// text) must not contribute an empty "user: " line to the title context.
	messages := []repo.ChatMessage{
		{
			Role:    "user",
			Content: "<message-context>\nEventType: assistant_mcp_auth_required\nAuthURL: https://example.test/oauth\n</message-context>\n",
		},
	}

	require.Empty(t, strings.TrimSpace(buildTitleContext(messages)))
}
