// URL-driven tab state for detail pages: the active tab is the last path
// segment when it is a known tab. Each detail component only mounts for its own
// route subtree, so the trailing segment is always either the resource id (no
// tab selected) or one of that page's tab segments.
//
// Pages keep their own tab list — the set differs per resource — and share this
// resolver.

function decodeSegment(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

// activeDetailTab returns the trailing path segment when it matches one of the
// supplied tabs, else undefined (the base detail URL, which callers redirect to
// the default tab).
export function activeDetailTab<T extends string>(
  pathname: string,
  validTabs: readonly T[],
): T | undefined {
  const segments = pathname.split("/").filter(Boolean).map(decodeSegment);
  const last = segments[segments.length - 1];
  return last && (validTabs as readonly string[]).includes(last)
    ? (last as T)
    : undefined;
}
