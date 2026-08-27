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
// OpenClaw prepends its own envelope to channel-originated turns (Discord /
// Slack / Telegram): an optional "[Thu 2026-08-27 13:51 EDT]" timestamp, a
// "Delivery: …" hint line, "<Label> (untrusted…):" headers over a ```json
// fence (Conversation info, Sender, Reply target, …), and blank-line-terminated
// chat-history / chat-window paragraphs. The hooks relay strips these before
// ingest on current installs; this covers turns stored before it did and any
// install still bootstrapping an older hooks binary.
var leadingEnvelopeRE = regexp.MustCompile(`(?s)^(?:` +
	`\s*<message-context>.*?</message-context>\s*` +
	`|\s*<notification>.*?</notification>\s*` +
	`|\s*\[[A-Za-z]{3} \d{4}-\d{2}-\d{2} \d{2}:\d{2}[^\]\n]*\] *` +
	"|\\s*Delivery: (?:to send a message, use the `message` tool\\.|Final assistant text is not automatically delivered in this run\\.[^\\n]*|No visible reply is delivered automatically in this run[^\\n]*)\\s*" +
	"|\\s*[^\\n]* \\(untrusted[^)\\n]*\\):\\n```json\\n.*?\\n```\\s*" +
	`|\s*[^\n]* \(untrusted(?:, chronological[^)\n]*|, for context)\):\n(?:[^\n]+(?:\n|$))*\s*` +
	`)+`)

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
