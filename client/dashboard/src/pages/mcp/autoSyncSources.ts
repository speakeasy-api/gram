// Pure helpers for the AutoSyncSourcesCard. Kept in a separate module
// from the React component so unit tests can import them without pulling
// the design-system / icon dependency chain.

export const FUNCTION_PREFIX = "function:";

// extractFunctionSubscriptions reads the toolset's auto_sync_sources
// column and returns the unprefixed function slugs the toolset is
// subscribed to. Non-function: entries are ignored at the UI layer; the
// server validator rejects them on writes today, so they should never
// appear here, but the guard keeps the UI honest if/when other kinds
// are introduced.
export function extractFunctionSubscriptions(
  autoSyncSources: string[],
): Set<string> {
  return new Set(
    autoSyncSources
      .filter((entry) => entry.startsWith(FUNCTION_PREFIX))
      .map((entry) => entry.slice(FUNCTION_PREFIX.length)),
  );
}

// applyFunctionToggle returns the next value for auto_sync_sources after
// flipping the named slug. Non-function entries are preserved verbatim so
// future polymorphic subscriptions don't get clobbered.
export function applyFunctionToggle(
  current: string[],
  slug: string,
  next: boolean,
): string[] {
  const nonFunction = current.filter(
    (entry) => !entry.startsWith(FUNCTION_PREFIX),
  );
  const subscribed = extractFunctionSubscriptions(current);
  if (next) {
    subscribed.add(slug);
  } else {
    subscribed.delete(slug);
  }
  return [
    ...nonFunction,
    ...Array.from(subscribed).map((s) => `${FUNCTION_PREFIX}${s}`),
  ];
}
