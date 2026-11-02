import type { TabValue as ServerTabValue } from "./x/MCPServerDetailsRouting";

/** Where a tab segment or `#tab` hash, current or retired, lives on the MCP server page. */
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
    case "inspect":
    case "guardrails":
      return { tab, hash: "" };
    case "authentication":
      return { tab: "settings", hash: "#authentication" };
    case "performance":
      return { tab: "settings", hash: "#performance" };
    case "environment":
      return { tab: "settings", hash: "#environment" };
    case "settings":
      // Keep a section anchor the settings tab may know (#authentication, ...).
      return { tab: "settings", hash: segment ? hash : "" };
    default:
      return { tab: "overview", hash: "" };
  }
}
