export type McpToolsAvailability = "loading" | "ready" | "unavailable";

/** Shown when tools/list settled empty or failed — Playground must not claim access. */
export const NO_MCP_TOOLS_MESSAGE =
  "No tools loaded from this server — connect authentication to test it.";

/**
 * Distinguishes in-flight tools/list from a settled empty/error result so
 * callers can avoid flashing the empty state during normal load.
 */
export function mcpToolsAvailability(
  loading: boolean,
  tools: Record<string, unknown> | undefined,
  error: unknown,
): McpToolsAvailability {
  if (loading) return "loading";
  if (error || !tools || Object.keys(tools).length === 0) {
    return "unavailable";
  }
  return "ready";
}

export function mcpToolsSendTooltip(
  availability: McpToolsAvailability,
): string {
  switch (availability) {
    case "loading":
      return "Loading tools…";
    case "unavailable":
      return NO_MCP_TOOLS_MESSAGE;
    case "ready":
      return "Send message";
  }
}

export function mcpToolsWelcomeSubtitle(
  availability: McpToolsAvailability,
  readySubtitle: string | undefined,
): string {
  switch (availability) {
    case "loading":
      return "Loading tools from this server…";
    case "unavailable":
      return NO_MCP_TOOLS_MESSAGE;
    case "ready":
      return readySubtitle ?? "";
  }
}
