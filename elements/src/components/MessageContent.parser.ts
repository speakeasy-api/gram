/**
 * Splits chat message content into a sequence of plain-text segments and
 * recognised widget code-fence blocks (`chart`, `ui`). Unsupported
 * languages are preserved verbatim as text so they remain visible.
 *
 * Lives in its own module so React Fast Refresh works for `<MessageContent>`
 * (component files must export only components).
 *
 * @internal
 */

/** @internal */
export type Segment =
  | { type: "text"; text: string }
  | { type: "block"; lang: string; code: string };

const FENCE_RE = /```(\w+)\r?\n([\s\S]*?)```/g;

const SUPPORTED_FENCE_LANGS = new Set(["chart", "ui"]);

/** @internal */
export function parseSegments(content: string): Segment[] {
  const segments: Segment[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;
  FENCE_RE.lastIndex = 0;
  while ((match = FENCE_RE.exec(content)) !== null) {
    if (match.index > lastIndex) {
      segments.push({
        type: "text",
        text: content.slice(lastIndex, match.index),
      });
    }
    const lang = (match[1] ?? "").toLowerCase();
    const code = match[2] ?? "";
    if (SUPPORTED_FENCE_LANGS.has(lang)) {
      segments.push({ type: "block", lang, code });
    } else {
      // Unsupported language: keep original block as text so it's still
      // visible (renders as a normal fenced code block in plain text).
      segments.push({ type: "text", text: match[0] });
    }
    lastIndex = match.index + match[0].length;
  }
  if (lastIndex < content.length) {
    segments.push({ type: "text", text: content.slice(lastIndex) });
  }
  return segments;
}
