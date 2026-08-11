package chat

import (
	"regexp"
	"strings"
)

// leadingEnvelopeRE matches one or more leading "envelope" blocks that agent
// harnesses prepend to a turn to steer the assistant toward the right channel —
// e.g. <message-context>…</message-context> from our assistant runtime (which
// source/surface the turn came from, MCP auth events) or
// <notification>…</notification> from Claude Code background tasks. The harness
// needs the block, but it is noise for title generation and session summaries —
// left in, the model fixates on the structured boilerplate.
//
// The tag is an allowlist of envelopes we know about rather than any
// <tag>…</tag>: a fully-generic match would also eat legitimate leading user
// markup (a message that starts with <details> or a pasted code block). Add new
// harnesses as another `<tag>…</tag>` alternative. Each alternative pairs an
// open tag with its own close tag (RE2 has no backreferences), so a mismatched
// `<message-context>…</notification>` is left alone. Anchored to the start, so
// a tag a user types mid-message is preserved; the non-greedy body stops at the
// first close tag.
var leadingEnvelopeRE = regexp.MustCompile(`(?s)^(?:\s*<message-context>.*?</message-context>\s*|\s*<notification>.*?</notification>\s*)+`)

// StripLeadingEnvelopes removes any leading harness framing so downstream LLM
// prompts (titles, session summaries) see only the human-authored turn text.
// When no allowlisted envelope is present the input is returned unchanged so
// intentional leading/trailing whitespace (e.g. indented pasted code) is kept.
func StripLeadingEnvelopes(s string) string {
	if !leadingEnvelopeRE.MatchString(s) {
		return s
	}
	return strings.TrimSpace(leadingEnvelopeRE.ReplaceAllString(s, ""))
}
