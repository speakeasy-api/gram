/**
 * Apps that drive Elements often prepend a machine-only context block to the
 * outgoing user turn — e.g. the Gram dashboard prefixes `<dashboard_context>…`
 * so the model knows which chart/date-range the user was looking at. The block
 * is meant for the model, not the human, but it rides along in the persisted
 * turn and reappears verbatim when the thread is reopened from history.
 *
 * These helpers split such blocks off the front of a user message so the UI can
 * fold them into a collapsed disclosure and render the human's text below.
 *
 * Matching is intentionally generic: any leading tag whose name ends in
 * `context` (`dashboard_context`, `message-context`, …) is folded, so this needs
 * no knowledge of a specific host app's naming.
 */
const CONTEXT_BLOCK_RE = /^\s*<([\w-]*context)>([\s\S]*?)<\/\1>\s*/i;

export interface ContextBlock {
  tag: string;
  body: string;
}

export interface SplitText {
  blocks: ContextBlock[];
  rest: string;
}

/** Peel consecutive leading `<…context>` blocks off the front of the text. */
export function splitContextBlocks(text: string): SplitText {
  const blocks: ContextBlock[] = [];
  let rest = text;
  // `CONTEXT_BLOCK_RE` is `^`-anchored and non-global, so each `exec` scans from
  // index 0 — we peel one leading block per pass by re-slicing `rest`.
  // `String.matchAll` doesn't fit: it requires a global regex and walks
  // `lastIndex` forward, which the `^` anchor never matches past position 0.
  let match = CONTEXT_BLOCK_RE.exec(rest);
  while (match) {
    blocks.push({ tag: match[1]!, body: match[2]!.trim() });
    rest = rest.slice(match[0].length);
    match = CONTEXT_BLOCK_RE.exec(rest);
  }
  return { blocks, rest };
}
