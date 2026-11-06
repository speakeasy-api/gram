import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { ToolUsageTargetSummary } from "@gram/client/models/components/toolusagetargetsummary.js";
import { ANY_RESOURCE } from "@/pages/agents/agent-api-key-grants";
import { CONNECT_SCOPE } from "@/pages/agents/agentPolicyEdit";
import type { InventoryServer } from "@/pages/agents/credentials/useServerInventory";

/** Target types for calls a Speakeasy MCP server served and signed in. */
const SERVED_TARGET_TYPES = new Set([
  "hosted_mcp_server",
  "tunneled_mcp_server",
]);

/** One MCP server on the agent's Overview, with its calls in the period. */
export type AgentServerUse = {
  key: string;
  /**
   * The id to look the server's credential up by: the id its grant names,
   * or the server id a call recorded. Undefined when neither is known.
   */
  resourceId: string | undefined;
  name: string;
  /**
   * Calls in the period. Undefined when they cannot be counted here: the
   * usage read failed or is not allowed, or the server lives in another
   * project than the page reads telemetry for.
   */
  calls: number | undefined;
};

export type AgentServerUseSummary = {
  rows: AgentServerUse[];
  /** A grant lets the agent connect to every MCP server. */
  anyServer: boolean;
};

/**
 * The MCP servers an agent can connect to (its `mcp:connect` grants) and the
 * calls it made to each in the period, busiest first. A server it can reach
 * but did not call is listed with no calls: whose credential it holds there
 * matters whether or not it was used this week. A server it called but no
 * longer has a grant for (or one a grant on every server reaches) is listed
 * from the calls.
 *
 * `grants` is undefined when the viewer cannot read the agent's policy, and
 * `targets` when they cannot read its calls.
 */
export function summarizeAgentServerUse({
  grants,
  servers,
  targets,
  projectId,
}: {
  grants: AgentPolicyGrant[] | undefined;
  servers: InventoryServer[];
  targets: ToolUsageTargetSummary[] | undefined;
  /** The project the page reads telemetry in. */
  projectId: string;
}): AgentServerUseSummary {
  const connect = (grants ?? []).filter(
    (grant) =>
      grant.scope === CONNECT_SCOPE &&
      grant.effect === "allow" &&
      grant.selector.resourceKind === "mcp",
  );
  const anyServer = connect.some(
    (grant) => grant.selector.resourceId === ANY_RESOURCE,
  );
  const served = (targets ?? []).filter((target) =>
    SERVED_TARGET_TYPES.has(target.targetType),
  );
  const counted = new Set<ToolUsageTargetSummary>();
  const rows: AgentServerUse[] = [];
  const seen = new Set<string>();
  for (const grant of connect) {
    const resourceId = grant.selector.resourceId;
    if (resourceId === ANY_RESOURCE || seen.has(resourceId)) continue;
    seen.add(resourceId);
    // A grant on a toolset reaches every server built on it.
    const matched = servers.filter(
      (server) => server.resourceId === resourceId || server.id === resourceId,
    );
    const ids = new Set([resourceId, ...matched.map((server) => server.id)]);
    const activity = new Set(
      matched.flatMap((server) =>
        server.activityTarget ? [server.activityTarget] : [],
      ),
    );
    let calls = 0;
    let label: string | undefined;
    for (const target of served) {
      const hit =
        (target.mcpServerId && ids.has(target.mcpServerId)) ||
        activity.has(`${target.targetType}:${target.targetId}`);
      if (!hit) continue;
      counted.add(target);
      calls += target.eventCount;
      label ??= target.targetLabel || target.targetId;
    }
    const server = matched[0];
    const elsewhere = !!server && server.projectId !== projectId;
    rows.push({
      key: `grant:${resourceId}`,
      resourceId,
      name: server?.name ?? label ?? "Unlisted MCP server",
      calls: targets && !elsewhere ? calls : undefined,
    });
  }
  for (const target of served) {
    if (counted.has(target)) continue;
    rows.push({
      key: `target:${target.targetType}:${target.targetId}`,
      resourceId: target.mcpServerId,
      name: target.targetLabel || target.targetId,
      calls: target.eventCount,
    });
  }
  rows.sort(
    (a, b) => (b.calls ?? -1) - (a.calls ?? -1) || a.name.localeCompare(b.name),
  );
  return { rows, anyServer };
}
