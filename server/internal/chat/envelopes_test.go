package chat_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/stretchr/testify/require"
)

func TestStripLeadingEnvelopesRemovesAssistantRuntimeFraming(t *testing.T) {
	t.Parallel()

	input := "<message-context>\nEventID: abc-123\nUserID: user-9\n</message-context>\n\nHow do I reduce my token usage?"
	require.Equal(t, "How do I reduce my token usage?", chat.StripLeadingEnvelopes(input))
}

// Other harnesses prepend their own envelopes (e.g. Claude Code background
// tasks return <notification>…</notification>); the strip is generic so the
// model isn't polluted by envelopes we don't control.
func TestStripLeadingEnvelopesRemovesForeignEnvelope(t *testing.T) {
	t.Parallel()

	input := "<notification>background task completed</notification>\nThe migration finished without errors."
	require.Equal(t, "The migration finished without errors.", chat.StripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesRemovesMultipleLeadingBlocks(t *testing.T) {
	t.Parallel()

	input := "<message-context>\nEventID: e1\n</message-context>\n<notification>done</notification>\n\nWhat changed?"
	require.Equal(t, "What changed?", chat.StripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesLeavesPlainTextUntouched(t *testing.T) {
	t.Parallel()

	require.Equal(t, "just a normal message", chat.StripLeadingEnvelopes("just a normal message"))
}

func TestStripLeadingEnvelopesPreservesWhitespaceWhenNoEnvelope(t *testing.T) {
	t.Parallel()

	input := "  indented code\n  next line  "
	require.Equal(t, input, chat.StripLeadingEnvelopes(input))
}

// Only known harness envelopes are stripped. A message that legitimately opens
// with user markup (a <details> block, a pasted snippet, etc.) must survive — a
// fully-generic <tag>…</tag> match would eat it and distort the prompt.
func TestStripLeadingEnvelopesLeavesUnknownLeadingTag(t *testing.T) {
	t.Parallel()

	input := "<details>my setup</details>\n\nwhy does the build fail?"
	require.Equal(t, input, chat.StripLeadingEnvelopes(input))
}

// Each allowlisted envelope must match its own close tag — a mismatched pair
// (open from one envelope, close from another) is not a real envelope and must
// be left intact.
func TestStripLeadingEnvelopesLeavesMismatchedTags(t *testing.T) {
	t.Parallel()

	input := "<message-context>hmm</notification> what is this?"
	require.Equal(t, input, chat.StripLeadingEnvelopes(input))
}

// The regex is anchored to the start of the message: only leading framing is
// removed. A user who happens to type tags mid-message keeps that text.
func TestStripLeadingEnvelopesOnlyStripsLeadingBlock(t *testing.T) {
	t.Parallel()

	input := "why does my agent emit <message-context>foo</message-context> in its output?"
	require.Equal(t, input, chat.StripLeadingEnvelopes(input))
}

const openclawConversationInfo = "Conversation info (untrusted metadata):\n```json\n{\n  \"chat_id\": \"channel:42\",\n  \"sender\": {\n    \"name\": \"Example User\"\n  },\n  \"was_mentioned\": true\n}\n```"

func TestStripLeadingEnvelopesRemovesOpenClawConversationInfo(t *testing.T) {
	t.Parallel()

	input := openclawConversationInfo + "\n\n@Bot what does this command do"
	require.Equal(t, "@Bot what does this command do", chat.StripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesRemovesOpenClawTimestampAndHint(t *testing.T) {
	t.Parallel()

	input := "[Thu 2026-08-27 13:51 EDT] Delivery: to send a message, use the `message` tool.\n\n" + openclawConversationInfo + "\n\nping"
	require.Equal(t, "ping", chat.StripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesRemovesOpenClawStackedBlocks(t *testing.T) {
	t.Parallel()

	input := openclawConversationInfo +
		"\n\nReply target of current user message (untrusted, for context):\n```json\n{\"body\": \"earlier\"}\n```" +
		"\n\nChat history since last reply (untrusted, for context):\n#1 2026-08-27 13:51:33 EDT alice: hi\n#2 2026-08-27 13:52:10 EDT ->#1 bob: yo" +
		"\n\nRecent messages (untrusted, chronological, oldest first):\n#3 2026-08-27 13:53:00 EDT carol: hey" +
		"\n\nsummarize the thread"
	require.Equal(t, "summarize the thread", chat.StripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesLeavesOpenClawSentinelMidMessage(t *testing.T) {
	t.Parallel()

	input := "why does the prompt say (untrusted metadata): here"
	require.Equal(t, input, chat.StripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesLeavesUnterminatedOpenClawFence(t *testing.T) {
	t.Parallel()

	input := "Conversation info (untrusted metadata):\n```json\n{\"chat_id\": \"x\"\n\nping"
	require.Equal(t, input, chat.StripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesKeepsTextRightAfterOpenClawHistory(t *testing.T) {
	t.Parallel()

	input := "Chat history since last reply (untrusted, for context):\n#1 2026-08-27 13:51:33 EDT alice: hi\n#2 2026-08-27 13:52:10 EDT ->#1 bob: yo\ncan you continue"
	require.Equal(t, "can you continue", chat.StripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesLeavesHumanTimestampWithoutEnvelope(t *testing.T) {
	t.Parallel()

	input := "[Mon 2024-05-01 09:30] could we move the sync?"
	require.Equal(t, input, chat.StripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesKeepsColonLineAfterOpenClawHistory(t *testing.T) {
	t.Parallel()

	input := "Chat history since last reply (untrusted, for context):\n#1 2026-08-27 13:51:33 EDT alice: hi\nNote: can you continue"
	require.Equal(t, "Note: can you continue", chat.StripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesKeepsTimestampPhraseAfterOpenClawHistory(t *testing.T) {
	t.Parallel()

	input := "Chat history since last reply (untrusted, for context):\n#1 2026-08-27 13:51:33 EDT alice: hi\n2026-08-27 standup: can you continue"
	require.Equal(t, "2026-08-27 standup: can you continue", chat.StripLeadingEnvelopes(input))
}

func TestStripLeadingEnvelopesRemovesOpenClawStampOnPlainTurn(t *testing.T) {
	t.Parallel()

	require.Equal(t, "hello", chat.StripLeadingEnvelopes("[Thu 2026-08-27 13:51 EDT] hello"))
	require.Equal(t, "hello", chat.StripLeadingEnvelopes("[Thu 2026-08-27 13:51:33 GMT+5:30] hello"))
}

func TestStripLeadingEnvelopesKeepsHashTaggedLineAfterOpenClawHistory(t *testing.T) {
	t.Parallel()

	input := "Chat history since last reply (untrusted, for context):\n#1 2026-08-27 13:51:33 EDT alice: hi\n#note can: continue"
	require.Equal(t, "#note can: continue", chat.StripLeadingEnvelopes(input))
}
