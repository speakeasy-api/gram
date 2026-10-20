export type TextSegment =
  | { kind: "markdown"; markdown: string }
  | { kind: "code"; code: string; language: string };

const OPENING_FENCE = /^```([\w-]*)\s*$/;

/**
 * Splits markdown at the fenced code blocks that start a line. A fence with no
 * closing line stays in the markdown around it.
 */
export function splitFencedCode(markdown: string): TextSegment[] {
  const segments: TextSegment[] = [];
  const lines = markdown.split("\n");
  let prose: string[] = [];
  const flushProse = () => {
    const text = prose.join("\n").trim();
    if (text !== "") segments.push({ kind: "markdown", markdown: text });
    prose = [];
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    const opening = OPENING_FENCE.exec(line);
    const closing =
      opening === null
        ? -1
        : lines.findIndex((other, j) => j > i && other.trimEnd() === "```");
    if (opening === null || closing === -1) {
      prose.push(line);
      continue;
    }
    flushProse();
    segments.push({
      kind: "code",
      code: lines.slice(i + 1, closing).join("\n"),
      language: opening[1] || "text",
    });
    i = closing;
  }
  flushProse();
  return segments;
}
