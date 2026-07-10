import type { useRoutes } from "@/routes";

const VALID_TABS = ["overview", "tools"] as const;

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
  builtInSlug: string,
): string | undefined {
  if (!builtInSlug) {
    return undefined;
  }

  const segments = pathname.split("/").filter(Boolean).map(decodePathSegment);
  const slugIndex = segments.findIndex(
    (segment, index) =>
      segment === builtInSlug &&
      segments[index - 1] === "built-in" &&
      segments[index - 2] === "mcp",
  );

  if (slugIndex === -1) {
    return undefined;
  }

  return segments[slugIndex + 1];
}

export function activeTabFromPath(
  pathname: string,
  builtInSlug: string,
): TabValue | undefined {
  const tabSegment = tabSegmentFromPath(pathname, builtInSlug);
  return tabSegment && isValidTab(tabSegment) ? tabSegment : undefined;
}

export function builtInTabHref(
  routes: ReturnType<typeof useRoutes>,
  builtInSlug: string,
  tab: TabValue,
): string {
  switch (tab) {
    case "overview":
      return routes.mcp.builtIn.overview.href(builtInSlug);
    case "tools":
      return routes.mcp.builtIn.tools.href(builtInSlug);
  }
}
