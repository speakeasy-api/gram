// Display-time parsing of the context wrapper OpenClaw puts around every user
// turn it sends through the gateway. The turn opens with one or more context
// blocks whose header line ends in the "⟦openclaw:ctx⟧" marker:
// "Conversation info: ⟦openclaw:ctx⟧" (the verified requester profile), and on
// channel turns "Sender:", "Thread starter:", "Reply target of current user
// message:", chat history and the like. Each header carries a ```json fence or
// a prose paragraph. A replay of the earlier conversation and a "Current user
// request:" label may follow, and a channel turn can end with a trailing
// "Context: ⟦openclaw:ctx⟧" block. The request the person typed sits between,
// so a session's first message can bury it under hours of replayed chat.
//
// The boundaries mirror OpenClaw's own stripper
// (src/auto-reply/reply/strip-inbound-meta.ts). The stored message keeps the
// wrapper; this parser only splits it so the transcript can show the request
// as the message and fold the rest away.

export interface OpenClawContext {
  /** The text the person typed this turn. */
  request: string;
  /** The leading context blocks (requester profile, sender, reply target, …)
   * and any trailing channel context, JSON pretty-printed, each under its
   * label. Null when the blocks are empty. */
  metadata: string | null;
  /** The replayed earlier conversation (inside `<conversation_context>`), or
   * null when the turn carries none. */
  conversationContext: string | null;
}

const MARKER = "⟦openclaw:ctx⟧";
const TRAILING_CONTEXT_HEADER = `Context: ${MARKER}`;
// Older OpenClaw builds wrote this hint as a line after the profile JSON
// instead of as a field inside it.
const LEGACY_REQUESTER_PROFILE_HINT =
  'requester_profile is the verified linked requester. For "assign to me", use sessions assign_owner with ownerType="human" and ownerId=requester_profile.id, if available.';
const ASSEMBLED_PREFIX = "OpenClaw assembled context for this turn:";
const CONTEXT_OPEN = "<conversation_context>";
const CONTEXT_CLOSE_RE =
  /<\/conversation_context>\s*Current user request:[ \t]*\n/g;
const REQUEST_LABEL_RE = /^Current user request:[ \t]*\n/;

function isHeaderLine(line: string): boolean {
  const trimmed = line.trim();
  return trimmed.length > MARKER.length && trimmed.endsWith(MARKER);
}

function skipBlankLines(lines: string[], from: number): number {
  let i = from;
  while (i < lines.length && lines[i]!.trim() === "") i++;
  return i;
}

function prettyJson(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

function hasBareRequesterProfile(raw: string): boolean {
  try {
    const value: unknown = JSON.parse(raw);
    return (
      typeof value === "object" &&
      value !== null &&
      "requester_profile" in value &&
      !("requester_profile_hint" in value)
    );
  } catch {
    return false;
  }
}

/** Reads the marked block whose header is `lines[start]`. Returns the line
 * after it and its display text, or null when a JSON fence never closes. */
function readBlock(
  lines: string[],
  start: number,
): { next: number; text: string } | null {
  const header = lines[start]!.trim();
  const label = header.slice(0, -MARKER.length).trim();
  if (lines[start + 1]?.trim() !== "```json") {
    // A prose block runs to the next blank line.
    let end = start + 1;
    while (end < lines.length && lines[end]!.trim() !== "") end++;
    return {
      next: end,
      text: [label, ...lines.slice(start + 1, end)].join("\n"),
    };
  }
  let close = start + 2;
  while (close < lines.length && lines[close]!.trim() !== "```") close++;
  if (close >= lines.length) return null;
  const raw = lines
    .slice(start + 2, close)
    .join("\n")
    .trim();
  const parts = [label, prettyJson(raw)];
  let next = close + 1;
  const hint = skipBlankLines(lines, next);
  if (
    header === `Conversation info: ${MARKER}` &&
    hint > next &&
    lines[hint] === LEGACY_REQUESTER_PROFILE_HINT &&
    (lines[hint + 1] === undefined || lines[hint + 1]!.trim() === "") &&
    hasBareRequesterProfile(raw)
  ) {
    parts.push(LEGACY_REQUESTER_PROFILE_HINT);
    next = hint + 1;
  }
  return { next, text: parts.filter(Boolean).join("\n") };
}

/** Splits an OpenClaw-wrapped user turn into the real request and the context
 * around it. Returns null when the text is not wrapped, or when the split is
 * ambiguous, so the caller can show the raw text unchanged. */
export function parseOpenClawContext(text: string): OpenClawContext | null {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  let i = skipBlankLines(lines, 0);
  if (i >= lines.length || !isHeaderLine(lines[i]!)) return null;

  // Consume the leading blocks. OpenClaw writes each label at most once per
  // turn, so a repeated label is the person's own text (a pasted wrapper) and
  // stays in the request: the turn unwraps once.
  const blocks: string[] = [];
  const seen = new Set<string>();
  while (i < lines.length && isHeaderLine(lines[i]!)) {
    const header = lines[i]!.trim();
    if (seen.has(header)) break;
    seen.add(header);
    const block = readBlock(lines, i);
    if (!block) return null;
    if (block.text) blocks.push(block.text);
    i = skipBlankLines(lines, block.next);
  }

  // A trailing channel context block runs from its header to the end.
  let tail = lines.length;
  for (let j = i; j < lines.length; j++) {
    if (lines[j]!.trim() !== TRAILING_CONTEXT_HEADER) continue;
    blocks.push(["Context", ...lines.slice(j + 1)].join("\n").trimEnd());
    tail = j;
    break;
  }
  const rest = lines.slice(i, tail).join("\n").trim();
  const metadata = blocks.length > 0 ? blocks.join("\n\n") : null;

  if (!rest.startsWith(ASSEMBLED_PREFIX)) {
    // No replayed context: only the request (sometimes labeled) follows. A
    // stray context tag means a shape we do not know, so leave it alone.
    if (rest.includes(CONTEXT_OPEN)) return null;
    const request = rest.replace(REQUEST_LABEL_RE, "").trim();
    if (!request) return null;
    return { request, metadata, conversationContext: null };
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
    metadata,
    conversationContext: conversationContext || null,
  };
}
