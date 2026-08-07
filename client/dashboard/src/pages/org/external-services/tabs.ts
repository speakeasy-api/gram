// URL-driven tab state for the external credential detail page: the active tab
// is the last path segment when it is a known tab, mirroring the Remote Identity
// Provider detail pages. A credential may later gain a "kms-keys" tab alongside
// the organization Encryption Keys UI (AGE-3067).

export const EXTERNAL_CREDENTIAL_TABS = ["overview", "settings"] as const;
export type ExternalCredentialTab = (typeof EXTERNAL_CREDENTIAL_TABS)[number];

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
