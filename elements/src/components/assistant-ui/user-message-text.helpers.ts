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
  // `CONTEXT_BLOCK_RE` is not global, so `exec` always scans from index 0 with
  // no `lastIndex` state — we advance by re-slicing `rest`, never by the regex.
  for (
    let match = CONTEXT_BLOCK_RE.exec(rest);
    match;
    match = CONTEXT_BLOCK_RE.exec(rest)
  ) {
    blocks.push({ tag: match[1]!, body: match[2]!.trim() });
    rest = rest.slice(match[0].length);
  }
  return { blocks, rest };
}
