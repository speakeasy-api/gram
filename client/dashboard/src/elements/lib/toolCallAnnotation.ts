/**
 * The dashboard assistant is instructed to precede tool calls with a terse
 * activity phrase (e.g. "Investigating failures in the last 30 days"). When a
 * text part looks like such a phrase and is immediately followed by tool
 * calls, the UI renders it as the tool group's heading instead of as message
 * prose.
 */

// Instructed phrases are 3–8 words; anything longer is commentary, not an
// annotation, and renders as prose.
export const TOOL_CALL_ANNOTATION_MAX_LENGTH = 100;

/**
 * Strict shape gate: a heading must read like "Investigating X" / "Deep
 * diving into Y". Length alone is not enough — the model sometimes narrates
 * setbacks in a single short sentence, and promoting those to the heading
 * looks broken. Require a present-participle opener and reject sentence or
 * Markdown punctuation.
 */
export function isToolCallAnnotation(text: string): boolean {
  const trimmed = text.trim();
  if (!hasAnnotationShape(trimmed)) return false;
  // Doing-phrase opener: one of the first two words is a present participle
  // ("Investigating…", "Deep diving…", "Cross-referencing…").
  const words = trimmed.split(/\s+/);
  return isGerund(words[0]) || isGerund(words[1]);
}

/**
 * Mid-stream an annotation arrives a character at a time, so the strict test
 * above rejects its own prefixes — "Inv" is not a gerund yet. Judging a
 * still-streaming part with it renders the opening as prose and then retracts
 * it once the word completes. Accept a prefix whose first two words could
 * still turn into a doing-phrase; a third word settles the question.
 */
export function isPartialToolCallAnnotation(text: string): boolean {
  const trimmed = text.trim();
  if (!hasAnnotationShape(trimmed)) return false;
  const words = trimmed.split(/\s+/);
  return isGerund(words[0]) || isGerund(words[1]) || words.length <= 2;
}

/** Length, single-line and punctuation gates shared by both tests. */
function hasAnnotationShape(trimmed: string): boolean {
  if (
    trimmed.length === 0 ||
    trimmed.length > TOOL_CALL_ANNOTATION_MAX_LENGTH ||
    trimmed.includes("\n")
  ) {
    return false;
  }
  // Multi-sentence commentary or Markdown formatting → prose.
  return !/[.!?] /.test(trimmed) && !/[`*_#[\]]/.test(trimmed);
}

const isGerund = (word: string | undefined) =>
  !!word && /ing$/i.test(word.replace(/[^a-zA-Z-]/g, ""));

/** Normalize an annotation for display as a group heading. */
export function toolCallAnnotationTitle(text: string): string {
  return text.trim().replace(/[.…:]+$/, "");
}

/**
 * The model sometimes emits prose and the activity phrase in one text block.
 * The last line still works as the annotation when it is terse; the remainder
 * renders as regular prose (see stripTrailingAnnotationLine).
 */
export function trailingAnnotationLine(
  text: string,
  { streaming = false }: { streaming?: boolean } = {},
): string | undefined {
  const lines = text.trim().split("\n");
  const last = lines[lines.length - 1]?.trim() ?? "";
  const matches = streaming
    ? isPartialToolCallAnnotation(last)
    : isToolCallAnnotation(last);
  return matches ? last : undefined;
}

/** The text minus its trailing annotation line (empty for a pure annotation). */
export function stripTrailingAnnotationLine(text: string): string {
  const trimmed = text.trimEnd();
  const idx = trimmed.lastIndexOf("\n");
  return idx === -1 ? "" : trimmed.slice(0, idx).trimEnd();
}
