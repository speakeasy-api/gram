import type { McpServerActivity } from "@gram/client/models/components/mcpserveractivity.js";

// Activity flags surfaced on the Distribute MCP listing. Both are deliberately
// low-key: `never` marks a server that has not received a single tool call
// within the telemetry retention window; `stale` marks one that was used at some
// point but has gone quiet inside the recent window (two weeks by default).
export type McpActivityStatus = "never" | "stale";

/**
 * Resolve the activity status for a single listing card/row.
 *
 * `activity` is the matched entry from telemetry.getMcpServerActivity, or
 * `undefined` when the server produced no tool calls in the lookback window (the
 * endpoint omits inactive servers entirely, so an absent entry means "never").
 *
 * Returns `null` when the server is healthy (recent activity) so callers can
 * render nothing in the common case.
 */
export function mcpActivityStatus(
  activity: McpServerActivity | undefined,
): McpActivityStatus | null {
  if (!activity || activity.totalToolCalls <= 0) return "never";
  if (activity.recentToolCalls <= 0) return "stale";
  return null;
}

/**
 * Index an activity list by its target identifier. Hosted (toolset-backed)
 * servers key by toolset slug; tunneled/remote servers key by MCP server slug,
 * matching how the backend attributes tool calls.
 */
export function indexMcpActivity(
  activity: McpServerActivity[] | undefined,
): Map<string, McpServerActivity> {
  const byTarget = new Map<string, McpServerActivity>();
  for (const entry of activity ?? []) {
    byTarget.set(entry.targetId, entry);
  }
  return byTarget;
}
