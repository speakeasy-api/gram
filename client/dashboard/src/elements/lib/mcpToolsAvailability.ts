export type McpToolsAvailability = "loading" | "ready" | "unavailable";

/** Shown when tools/list settled empty or failed — Playground must not claim access. */
export const NO_MCP_TOOLS_MESSAGE =
  "No tools loaded from this server — connect authentication to test it.";

/** Shown in the composer @-picker when tools/list settled empty or failed. */
export const NO_CONTEXT_TOOLS_MESSAGE =
  "No tools loaded from attached servers.";

/**
 * Distinguishes in-flight tools/list from a settled empty/error result so
 * callers can avoid flashing the empty state during normal load.
 */
export function mcpToolsAvailability(
  loading: boolean,
  tools: Record<string, unknown> | undefined,
  error: unknown,
): McpToolsAvailability {
  // Disabled/idle queries (auth settling, no server yet) report not-loading
  // with undefined data. Treat that as in-flight so we don't flash the
  // settled-empty warning before tools/list has run.
  if (loading || (!error && tools === undefined)) return "loading";
  if (error || !tools || Object.keys(tools).length === 0) {
    return "unavailable";
  }
  return "ready";
}

/** True when a host opted into `requireMcpTools` and tools are not ready. */
export function mcpToolsSendBlocked(
  requireMcpTools: boolean | undefined,
  loading: boolean,
  tools: Record<string, unknown> | undefined,
  error: unknown,
): boolean {
  return (
    requireMcpTools === true &&
    mcpToolsAvailability(loading, tools, error) !== "ready"
  );
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

export function composerContextToolsEmptyMessage(loading: boolean): string {
  return loading ? "Loading tools…" : NO_CONTEXT_TOOLS_MESSAGE;
}
