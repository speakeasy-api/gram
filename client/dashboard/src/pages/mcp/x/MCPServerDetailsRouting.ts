const VALID_TABS = ["overview", "team-access", "settings"] as const;
const LEGACY_AUTHENTICATION_TAB = "authentication";

export type TabValue = (typeof VALID_TABS)[number];

function isValidTab(value: string): value is TabValue {
  return (VALID_TABS as readonly string[]).includes(value);
}

function decodePathSegment(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

function tabSegmentFromPath(
  pathname: string,
  mcpServerSlug: string,
): string | undefined {
  if (!mcpServerSlug) {
    return undefined;
  }

  const segments = pathname.split("/").filter(Boolean).map(decodePathSegment);
  const serverSlugIndex = segments.findIndex(
    (segment, index) =>
      segment === mcpServerSlug &&
      segments[index - 1] === "x" &&
      segments[index - 2] === "mcp",
  );

  if (serverSlugIndex === -1) {
    return undefined;
  }

  return segments[serverSlugIndex + 1];
}

export function activeTabFromPath(
  pathname: string,
  mcpServerSlug: string,
): TabValue | undefined {
  const tabSegment = tabSegmentFromPath(pathname, mcpServerSlug);
  return tabSegment && isValidTab(tabSegment) ? tabSegment : undefined;
}

export function isLegacyAuthenticationTabPath(
  pathname: string,
  mcpServerSlug: string,
): boolean {
  return (
    tabSegmentFromPath(pathname, mcpServerSlug) === LEGACY_AUTHENTICATION_TAB
  );
}

export function initialTabFromHash(
  hash: string,
  isRbacEnabled: boolean,
): TabValue {
  const hashValue = hash.replace("#", "");
  if (hashValue === LEGACY_AUTHENTICATION_TAB) return "settings";
  if (!isValidTab(hashValue)) return "overview";
  if (hashValue === "team-access" && !isRbacEnabled) return "overview";
  return hashValue;
}
