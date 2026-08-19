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
 * it once the word completes. Accept a prefix that could still turn into a
 * doing-phrase: the strict test only inspects the first two words, so a third
 * word settles the question, and an opener that cannot be a verb settles it
 * immediately.
 */
export function isPartialToolCallAnnotation(text: string): boolean {
  const trimmed = text.trim();
  if (!hasAnnotationShape(trimmed)) return false;
  const words = trimmed.split(/\s+/);
  if (isGerund(words[0]) || isGerund(words[1])) return true;
  if (words.length > 2) return false;
  // Only the last word is still growing, so it could still gain its "ing" —
  // "I" is prose on its own but also the first character of "Investigating".
  // Judge the opener only once a following word has settled it.
  return words.length === 1 || !isNonVerbOpener(words[0]);
}

/**
 * Ordinary replies open with a pronoun, determiner or discourse marker far
 * more often than with a verb, and none of those can grow into a participle.
 * Without this, every prose answer's first two words are withheld until a
 * third arrives. A word that already ends in "ing" is matched as a gerund
 * before we get here, so "Letting" is unaffected by "let" appearing below.
 */
const NON_VERB_OPENERS = new Set([
  "a",
  "all",
  "also",
  "an",
  "and",
  "any",
  "based",
  "both",
  "but",
  "done",
  "each",
  "first",
  "for",
  "from",
  "great",
  "here",
  "how",
  "however",
  "i",
  "if",
  "in",
  "it",
  "its",
  "let",
  "looks",
  "my",
  "next",
  "no",
  "not",
  "note",
  "of",
  "ok",
  "okay",
  "on",
  "or",
  "our",
  "perfect",
  "seems",
  "so",
  "some",
  "sorry",
  "sure",
  "thanks",
  "that",
  "the",
  "their",
  "then",
  "there",
  "these",
  "they",
  "this",
  "those",
  "to",
  "we",
  "what",
  "when",
  "where",
  "which",
  "why",
  "with",
  "yes",
  "you",
  "your",
]);

const isNonVerbOpener = (word: string | undefined) =>
  !!word &&
  NON_VERB_OPENERS.has(
    word
      .replace(/[^a-zA-Z'-]/g, "")
      .split("'")[0]!
      .toLowerCase(),
  );

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
export function trailingAnnotationLine(text: string): string | undefined {
  const lines = text.trim().split("\n");
  const last = lines[lines.length - 1]?.trim() ?? "";
  return isToolCallAnnotation(last) ? last : undefined;
}

/** The text minus its trailing annotation line (empty for a pure annotation). */
export function stripTrailingAnnotationLine(text: string): string {
  const trimmed = text.trimEnd();
  const idx = trimmed.lastIndexOf("\n");
  return idx === -1 ? "" : trimmed.slice(0, idx).trimEnd();
}
