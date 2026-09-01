/**
 * Builds a section meta line from counted terms, dropping the zeros.
 *
 * Printing every term unconditionally left sparse identities with a row of
 * mono zeros in the top-right of every tab ("0 findings · 0 denied · 0 shadow
 * servers"), which reads as a broken panel rather than a quiet one. A term
 * only earns its place once it has something to report; when nothing does,
 * the whole line goes away and the panels' own empty states carry the answer.
 */
export function sectionMeta(
  terms: { count: number; singular: string; plural?: string }[],
): string | undefined {
  const parts = terms
    .filter((term) => term.count > 0)
    .map(
      (term) =>
        `${term.count.toLocaleString()} ${
          term.count === 1
            ? term.singular
            : (term.plural ?? `${term.singular}s`)
        }`,
    );
  return parts.length > 0 ? parts.join(" · ") : undefined;
}
