// Parsing helpers for the retired /mcp/:toolsetSlug detail URLs. The route
// now redirects to the mcp_servers-backed details page (/mcp/x/:serverSlug),
// and these helpers recover which tab an old link pointed at.

const VALID_TABS = [
  "overview",
  "tools",
  "resources",
  "prompts",
  "authentication",
  "performance",
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

function tabSegmentFromPath(
  pathname: string,
  toolsetSlug: string,
): string | undefined {
  if (!toolsetSlug) {
    return undefined;
  }

  const segments = pathname.split("/").filter(Boolean).map(decodePathSegment);
  const slugIndex = segments.findIndex(
    (segment, index) =>
      segment === toolsetSlug && segments[index - 1] === "mcp",
  );

  if (slugIndex === -1) {
    return undefined;
  }

  return segments[slugIndex + 1];
}

export function activeTabFromPath(
  pathname: string,
  toolsetSlug: string,
): TabValue | undefined {
  const tabSegment = tabSegmentFromPath(pathname, toolsetSlug);
  return tabSegment && isValidTab(tabSegment) ? tabSegment : undefined;
}

export function initialTabFromHash(hash: string): TabValue {
  const hashValue = hash.replace("#", "");
  if (!isValidTab(hashValue)) return "overview";
  return hashValue;
}
