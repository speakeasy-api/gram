import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { ToolUsageTargetSummary } from "@gram/client/models/components/toolusagetargetsummary.js";
import { ANY_RESOURCE } from "@/pages/agents/agent-api-key-grants";
import { isAgentPolicyConstraint } from "@/pages/agents/agent-policy-grants";
import { CONNECT_SCOPE } from "@/pages/agents/agentPolicyEdit";
import type { InventoryServer } from "@/pages/agents/credentials/useServerInventory";

/** Target types for calls a Speakeasy MCP server served and signed in. */
const SERVED_TARGET_TYPES = new Set([
  "hosted_mcp_server",
  "tunneled_mcp_server",
]);

/**
 * The most targets the targets read returns (the server's TargetLimit). A
 * response this long may have left a server out, so a server missing from it
 * has an unknown count rather than none.
 */
const TARGETS_READ_LIMIT = 200;

/** One MCP server on the agent's Overview, with its calls in the period. */
type AgentServerUse = {
  key: string;
  /**
   * The id to look the server's credential up by: the id its grant names,
   * or the server id a call recorded. Undefined when neither is known.
   */
  resourceId: string | undefined;
  name: string;
  /**
   * Calls in the period. Undefined when they cannot be counted here: the
   * usage read failed or is not allowed, the server lives in another
   * project than the page reads telemetry for, or the read hit its target
   * limit without this server.
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
  // Top sources lists at most TARGETS_READ_LIMIT targets, so a full list may
  // have left out a server the agent did call.
  const capped = (targets?.length ?? 0) >= TARGETS_READ_LIMIT;
  const counted = new Set<ToolUsageTargetSummary>();
  const rows: AgentServerUse[] = [];
  const seen = new Set<string>();
  const grantRow = (
    key: string,
    resourceId: string,
    server: InventoryServer | undefined,
    ids: Set<string>,
  ): AgentServerUse => {
    const activity = server?.activityTarget;
    let calls = 0;
    let label: string | undefined;
    for (const target of served) {
      const hit =
        (target.mcpServerId && ids.has(target.mcpServerId)) ||
        activity === `${target.targetType}:${target.targetId}`;
      if (!hit) continue;
      counted.add(target);
      calls += target.eventCount;
      label ??= target.targetLabel || target.targetId;
    }
    const elsewhere = !!server && server.projectId !== projectId;
    const countable = !!targets && !elsewhere && (calls > 0 || !capped);
    return {
      key,
      resourceId,
      name: server?.name ?? label ?? "Unlisted MCP server",
      calls: countable ? calls : undefined,
    };
  };
  for (const grant of connect) {
    const resourceId = grant.selector.resourceId;
    if (resourceId === ANY_RESOURCE || seen.has(resourceId)) continue;
    seen.add(resourceId);
    // A grant narrowed to a project reaches only the servers in it.
    const project = isAgentPolicyConstraint(grant.selector.projectId)
      ? grant.selector.projectId
      : undefined;
    // A grant on a toolset reaches every server built on it. Each is its own
    // row, with its own name, credential and calls.
    const matched = servers.filter(
      (server) =>
        (server.resourceId === resourceId || server.id === resourceId) &&
        (!project || server.projectId === project),
    );
    if (matched.length === 0) {
      rows.push(
        grantRow(
          `grant:${resourceId}`,
          resourceId,
          undefined,
          new Set([resourceId]),
        ),
      );
      continue;
    }
    for (const server of matched) {
      // A call recorded against the grant's own id belongs to a server only
      // when the grant reaches no other.
      const ids = new Set(
        matched.length === 1 ? [resourceId, server.id] : [server.id],
      );
      rows.push(
        grantRow(
          matched.length === 1
            ? `grant:${resourceId}`
            : `grant:${resourceId}:${server.id}`,
          matched.length === 1 ? resourceId : server.id,
          server,
          ids,
        ),
      );
    }
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
