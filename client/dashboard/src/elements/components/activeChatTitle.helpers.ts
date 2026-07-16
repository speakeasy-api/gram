export const MAX_TITLE_LENGTH = 200;
export const FALLBACK_TITLE = "New Chat";

/**
 * Decides what a title edit should persist. Trims the draft; reports `changed`
 * false when it matches the current (already-trimmed) title so an untouched
 * edit — including the empty "New Chat" fallback — saves nothing. An empty
 * trimmed value is a deliberate reset to automatic naming.
 */
export function resolveTitleEdit(
  draft: string,
  currentTitle: string,
): { changed: boolean; value: string } {
  const value = draft.trim();
  return { changed: value !== currentTitle, value };
}
