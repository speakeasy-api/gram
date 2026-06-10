const VALID_TABS = [
  "overview",
  "authentication",
  "team-access",
  "settings",
] as const;

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

export function activeTabFromPath(
  pathname: string,
  mcpServerSlug: string,
): TabValue | undefined {
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

  const tabSegment = segments[serverSlugIndex + 1];
  return tabSegment && isValidTab(tabSegment) ? tabSegment : undefined;
}

export function initialTabFromHash(
  hash: string,
  isRbacEnabled: boolean,
): TabValue {
  const hashValue = hash.replace("#", "");
  if (!isValidTab(hashValue)) return "overview";
  if (hashValue === "team-access" && !isRbacEnabled) return "overview";
  return hashValue;
}
