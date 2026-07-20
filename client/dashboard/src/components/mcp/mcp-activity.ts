import type { McpServerActivity } from "@gram/client/models/components/mcpserveractivity.js";

// Activity flags surfaced on the Distribute MCP listing. Both are deliberately
// low-key: `never` marks a server that has not received a single tool call
// within the telemetry retention window; `stale` marks one that was used at some
// point but has gone quiet inside the recent window (two weeks by default).
export type McpActivityStatus = "never" | "stale";

// The MCP server target types the activity endpoint reports. Hosted toolset and
// remote MCP servers both attribute as "hosted_mcp_server"; tunnelled servers as
// "tunneled_mcp_server".
export type McpActivityTargetType = "hosted_mcp_server" | "tunneled_mcp_server";

// A hosted toolset and a tunnelled/remote MCP server can share a slug, so the
// identifier alone is ambiguous. Compose the target type with the identifier so
// each server keys uniquely and can't overwrite or shadow the other.
function activityKey(targetType: string, targetId: string): string {
  return `${targetType} ${targetId}`;
}

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
 * Index an activity list by its (targetType, targetId) pair. Keying on the pair
 * rather than the identifier alone prevents a hosted toolset and a
 * tunnelled/remote server that happen to share a slug from overwriting each
 * other and mis-labelling a card.
 */
export function indexMcpActivity(
  activity: McpServerActivity[] | undefined,
): Map<string, McpServerActivity> {
  const byTarget = new Map<string, McpServerActivity>();
  for (const entry of activity ?? []) {
    byTarget.set(activityKey(entry.targetType, entry.targetId), entry);
  }
  return byTarget;
}

/**
 * Look up a listing item's activity by its target type and identifier (toolset
 * slug for hosted servers, MCP server slug for tunnelled/remote servers).
 */
export function lookupMcpActivity(
  index: Map<string, McpServerActivity>,
  targetType: McpActivityTargetType,
  targetId: string,
): McpServerActivity | undefined {
  return index.get(activityKey(targetType, targetId));
}
