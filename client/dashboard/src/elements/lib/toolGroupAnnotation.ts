/** The minimal shape of a message part this module needs. */
export interface AnnotatablePart {
  type?: string;
  text?: string;
}

/**
 * toolGroupAnnotation returns the assistant's own description of what a tool
 * group is doing: the text of the message part immediately preceding it.
 *
 * A response that calls tools carries its narration in the same message —
 * `{ text: "Pulling the last 30 days of usage", tool_calls: [...] }` — so that
 * text IS the description of those calls. It is shown as the group's label
 * rather than as prose above it, which is why the same predicate has to decide
 * both (see `isToolGroupAnnotation`).
 *
 * Returns "" when there is no such text: a group that opens a message, or one
 * that follows another group's results.
 */
export function toolGroupAnnotation(
  parts: readonly AnnotatablePart[],
  startIndex: number,
): string {
  const previous = parts[startIndex - 1];
  if (previous?.type !== "text") return "";

  // A heading doesn't end in a full stop. Models write these as sentences even
  // when asked for labels, and one trailing period is the cheapest half of that
  // to fix here rather than in the prompt.
  return (previous.text ?? "").trim().replace(/\.$/, "");
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
