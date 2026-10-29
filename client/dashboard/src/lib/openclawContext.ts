// Display-time parsing of the context wrapper OpenClaw puts around every user
// turn it sends through the gateway. The turn opens with a
// "Conversation info: [openclaw:ctx]" JSON block (the verified requester
// profile), optionally followed by a replay of the earlier conversation and a
// "Current user request:" label. The request the person actually typed comes
// last, so a session's first message can bury it under hours of replayed chat.
//
// The stored message keeps the wrapper. This parser only splits it so the
// transcript can show the request as the message and fold the rest away.

export interface OpenClawContext {
  /** The text the person typed this turn. */
  request: string;
  /** The JSON block under the marker, which carries the requester profile.
   * Pretty-printed when it parses; null when the block is empty. */
  conversationInfo: string | null;
  /** The replayed earlier conversation (inside `<conversation_context>`), or
   * null when the turn carries none. */
  conversationContext: string | null;
}

const HEADER_RE =
  /^Conversation info: \[openclaw:ctx\][ \t]*\n```json[ \t]*\n([\s\S]*?)\n```[ \t]*(?:\n|$)/;
const ASSEMBLED_PREFIX = "OpenClaw assembled context for this turn:";
const CONTEXT_OPEN = "<conversation_context>";
const CONTEXT_CLOSE_RE =
  /<\/conversation_context>\s*Current user request:[ \t]*\n/g;
const REQUEST_LABEL_RE = /^Current user request:[ \t]*\n/;

function formatInfo(raw: string): string | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2);
  } catch {
    return trimmed;
  }
}

/** Splits an OpenClaw-wrapped user turn into the real request and the context
 * around it. Returns null when the text is not wrapped, or when the split is
 * ambiguous, so the caller can show the raw text unchanged. */
export function parseOpenClawContext(text: string): OpenClawContext | null {
  const source = text.replace(/\r\n/g, "\n").trimStart();
  const header = HEADER_RE.exec(source);
  if (!header) return null;

  const conversationInfo = formatInfo(header[1] ?? "");
  const rest = source.slice(header[0].length).trim();

  if (!rest.startsWith(ASSEMBLED_PREFIX)) {
    // No replayed context: only the request (sometimes labeled) follows. A
    // stray context tag means a shape we do not know, so leave it alone.
    if (rest.includes(CONTEXT_OPEN)) return null;
    const request = rest.replace(REQUEST_LABEL_RE, "").trim();
    if (!request) return null;
    return { request, conversationInfo, conversationContext: null };
  }

  const open = rest.indexOf(CONTEXT_OPEN);
  if (open === -1) return null;
  // The close tag + label must occur exactly once. More than one means the
  // replay or the request itself quotes the wrapper, and any split could put
  // part of the request in the context or the reverse.
  const closes = [...rest.matchAll(CONTEXT_CLOSE_RE)];
  if (closes.length !== 1) return null;
  const close = closes[0];
  if (!close || close.index === undefined || close.index < open) return null;

  const conversationContext = rest
    .slice(open + CONTEXT_OPEN.length, close.index)
    .trim();
  const request = rest.slice(close.index + close[0].length).trim();
  if (!request) return null;
  return {
    request,
    conversationInfo,
    conversationContext: conversationContext || null,
  };
}
