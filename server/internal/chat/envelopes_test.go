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
