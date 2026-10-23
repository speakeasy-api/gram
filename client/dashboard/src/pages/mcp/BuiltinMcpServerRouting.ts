import type { useRoutes } from "@/routes";

const VALID_TABS = ["overview", "tools", "authentication"] as const;

export const BUILTIN_MCP_TAB_URLS: string[] = [...VALID_TABS];

export type BuiltinMcpTab = (typeof VALID_TABS)[number];

function isValidTab(value: string): value is BuiltinMcpTab {
  return (VALID_TABS as readonly string[]).includes(value);
}

function decodePathSegment(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

/**
 * The key is matched against the segment after `builtin`, so a project whose
 * own server is named "slack" can never be read as the built-in one.
 */
export function activeBuiltinTabFromPath(
  pathname: string,
  builtinKey: string,
): BuiltinMcpTab | undefined {
  if (!builtinKey) return undefined;
  const segments = pathname.split("/").filter(Boolean).map(decodePathSegment);
  const keyIndex = segments.findIndex(
    (segment, index) =>
      segment === builtinKey && segments[index - 1] === "builtin",
  );
  if (keyIndex === -1) return undefined;
  const tab = segments[keyIndex + 1];
  return tab && isValidTab(tab) ? tab : undefined;
}

export function builtinMcpTabHref(
  routes: ReturnType<typeof useRoutes>,
  builtinKey: string,
  tab: BuiltinMcpTab,
): string {
  switch (tab) {
    case "overview":
      return routes.mcp.builtin.overview.href(builtinKey);
    case "tools":
      return routes.mcp.builtin.tools.href(builtinKey);
    case "authentication":
      return routes.mcp.builtin.authentication.href(builtinKey);
  }
}
