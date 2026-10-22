import type { TabValue as ServerTabValue } from "./x/MCPServerDetailsRouting";

function decodePathSegment(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

/** The segment after `/mcp/<toolsetSlug>` in a legacy toolset page URL. */
export function toolsetTabSegment(
  pathname: string,
  toolsetSlug: string,
): string | undefined {
  if (!toolsetSlug) return undefined;
  const segments = pathname.split("/").filter(Boolean).map(decodePathSegment);
  const slugIndex = segments.findIndex(
    (segment, index) => segment === toolsetSlug && segments[index - 1] === "mcp",
  );
  return slugIndex === -1 ? undefined : segments[slugIndex + 1];
}

/** Where a legacy toolset page tab (path segment or `#tab` hash) lives on the MCP server page. */
export function serverTabForToolsetTab(
  segment: string | undefined,
  hash: string,
): { tab: ServerTabValue; hash: string } {
  const tab = segment ?? hash.replace("#", "");
  switch (tab) {
    case "tools":
    case "resources":
    case "prompts":
    case "team-access":
    case "sessions":
      return { tab, hash: "" };
    case "authentication":
      return { tab: "settings", hash: "#authentication" };
    case "performance":
      return { tab: "settings", hash: "#performance" };
    case "settings":
      // Keep a section anchor the settings tab may know (#authentication, ...).
      return { tab: "settings", hash: segment ? hash : "" };
    default:
      return { tab: "overview", hash: "" };
  }
}
