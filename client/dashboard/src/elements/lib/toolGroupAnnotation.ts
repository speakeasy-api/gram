/** The minimal shape of a message part this module needs. */
export interface AnnotatablePart {
  type?: string;
  text?: string;
}

/** Longest annotation kept as a tool-group label; longer text is prose. */
const MAX_ANNOTATION_CHARS = 200;

/**
 * toolGroupAnnotation returns the assistant's own description of what a tool
 * group is doing: the text part immediately preceding the group in the same
 * message.
 *
 * Models narrate before they act ("Now let me get per-model cost from the
 * logs"), so the description of a tool call is already in the stream — no
 * second model call is needed to produce one, and it is available the moment
 * the call is dispatched. The paired text is shown as the group's label
 * instead of as prose above it, which is why the same predicate has to decide
 * both (see `isToolGroupAnnotation`).
 *
 * Returns "" when the preceding part isn't a usable annotation: a group that
 * opens a message, one that follows a tool result, or a passage long enough to
 * be real prose rather than a label.
 */
export function toolGroupAnnotation(
  parts: readonly AnnotatablePart[],
  startIndex: number,
): string {
  const previous = parts[startIndex - 1];
  if (previous?.type !== "text") return "";

  const text = (previous.text ?? "").trim();
  if (text === "" || text.length > MAX_ANNOTATION_CHARS) return "";
  // Multi-paragraph text is an answer, not a label for what comes next.
  if (text.includes("\n\n")) return "";

  // A heading doesn't end in a full stop. Models write these as sentences even
  // when asked for labels, and one trailing period is the cheapest half of that
  // to fix here rather than in the prompt.
  return text.replace(/\.$/, "");
}

/**
 * isToolGroupAnnotation reports whether a text part is serving as the label of
 * the tool group that follows it, and so should not also be rendered as prose.
 * The counterpart to {@link toolGroupAnnotation} — they must agree, or the
 * text either appears twice or vanishes.
 */
export function isToolGroupAnnotation(
  parts: readonly AnnotatablePart[],
  index: number,
): boolean {
  if (parts[index]?.type !== "text") return false;
  if (parts[index + 1]?.type !== "tool-call") return false;
  return toolGroupAnnotation(parts, index + 1) !== "";
}
