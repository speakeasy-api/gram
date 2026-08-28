// Display-time stripping of the machine framing agent harnesses prepend to a
// user turn. The stored message keeps the framing — the model needed it and
// risk findings may point into it — but it is plumbing, not conversation, so
// transcripts render only the human-authored text. Mirrors the server's
// `chat.StripLeadingEnvelopes`, which applies the same allowlist before title
// and summary generation.
//
// Envelopes recognized, all anchored to the start of the text:
// - `<message-context>…</message-context>` (Gram assistant runtime) and
//   `<notification>…</notification>` (Claude Code background tasks).
// - OpenClaw's inbound metadata on channel-originated turns (Discord / Slack /
//   Telegram): a "Delivery: …" hint line, "<Label> (untrusted…):" headers over a
//   ```json fence (Conversation info, Sender, Reply target, …), and
//   chat-history / chat-window paragraphs whose rows are
//   "#id 2026-08-27 13:51:33 EDT [reply target] ->#id sender: text". OpenClaw
//   also stamps every turn with a leading "[Thu 2026-08-27 13:51 EDT] " that
//   always carries a short zone name; a human date-time bracket without a
//   zone is not the stamp and is left alone.
const STAMP = String.raw`\s*\[(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun) \d{4}-\d{2}-\d{2} \d{2}:\d{2}(?::\d{2})? [A-Z]{1,5}(?:[+-]\d{1,2}(?::\d{2})?)?\] *`;
const ROW_TIME = String.raw`\d{4}-\d{2}-\d{2} \d{2}:\d{2}(?::\d{2})?(?: [A-Z]{1,5})? `;
const HISTORY_ROW = String.raw`(?:#\S+ (?:${ROW_TIME})?|${ROW_TIME})(?:\[reply target\] )?(?:->#\S+ )?[^\n]*: [^\n]*(?:\n|$)`;
const ENVELOPE = [
  STAMP,
  String.raw`\s*<message-context>[\s\S]*?<\/message-context>\s*`,
  String.raw`\s*<notification>[\s\S]*?<\/notification>\s*`,
  String.raw`\s*Delivery: (?:to send a message, use the \x60message\x60 tool\.|Final assistant text is not automatically delivered in this run\.[^\n]*|No visible reply is delivered automatically in this run[^\n]*)\s*`,
  String.raw`\s*[^\n]* \(untrusted[^)\n]*\):\n\x60\x60\x60json\n[\s\S]*?\n\x60\x60\x60\s*`,
  String.raw`\s*[^\n]* \(untrusted(?:, chronological[^)\n]*|, for context)\):\n(?:${HISTORY_ROW})*\s*`,
].join("|");
const LEADING_ENVELOPE_RE = new RegExp(`^(?:${ENVELOPE})+`, "i");

/** Splits a persisted user turn into the leading harness envelope and the
 * human-authored body. `envelope` is "" when there is none, and `body` is then
 * the input unchanged. */
export function splitLeadingEnvelopes(text: string): {
  envelope: string;
  body: string;
} {
  const match = LEADING_ENVELOPE_RE.exec(text);
  if (!match) return { envelope: "", body: text };
  return { envelope: match[0], body: text.slice(match[0].length) };
}

/** Removes leading harness envelopes from a persisted user turn for display.
 * Text with no envelope is returned unchanged. The envelope is not lost — the
 * transcript folds it into a collapsed disclosure via `splitLeadingEnvelopes`,
 * so a spoofed envelope inside a prompt stays inspectable. */
export function stripLeadingEnvelopes(text: string): string {
  return splitLeadingEnvelopes(text).body;
}
