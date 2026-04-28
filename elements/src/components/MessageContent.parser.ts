/** @internal Sibling of MessageContent.tsx so component file can stay component-only (Fast Refresh). */

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
      // Unrecognised language: keep the original fence verbatim as text.
      segments.push({ type: "text", text: match[0] });
    }
    lastIndex = match.index + match[0].length;
  }
  if (lastIndex < content.length) {
    segments.push({ type: "text", text: content.slice(lastIndex) });
  }
  return segments;
}
