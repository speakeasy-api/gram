import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { KeyServer } from "./AgentKeyServers";

const ACTIONS: Record<string, string> = {
  "mcp:connect": "Use tools",
  "mcp:read": "View server",
  "mcp:write": "Manage server",
};
const DISPOSITIONS: Record<string, string> = {
  read_only: "Read-only tools",
  destructive: "Tools that may delete or overwrite data",
  idempotent: "Tools with the same effect when repeated",
  open_world: "Tools that interact with external systems",
};

export interface ReviewAccess {
  action: string;
  condition: string | undefined;
  allTools: boolean;
  categoryTools?: true;
  tools: string[];
}

/** Inventory occasionally falls back to an ID. Never repeat that fallback in review. */
export function reviewLabel(
  value: string | undefined,
  ids: string[] = [],
): string | undefined {
  const label = value?.trim();
  if (
    !label ||
    ids.includes(label) ||
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
      label,
    )
  )
    return undefined;
  return label;
}

function matches(grant: AgentPolicyGrantForm, server: KeyServer): boolean {
  const selector = grant.selector;
  return (
    server.kind !== "Unproxied" &&
    selector.resourceKind === "mcp" &&
    (selector.resourceId === "*" ||
      selector.resourceId === server.resourceId) &&
    (selector.projectId === undefined ||
      selector.projectId === "*" ||
      selector.projectId === server.projectId)
  );
}

function supported(grant: AgentPolicyGrantForm): boolean {
  return (
    grant.effect === "allow" &&
    grant.scope in ACTIONS &&
    (grant.selector.disposition === undefined ||
      grant.selector.disposition in DISPOSITIONS) &&
    Object.entries(grant.selector).every(
      ([key, value]) =>
        value === undefined ||
        [
          "resourceId",
          "resourceKind",
          "projectId",
          "tool",
          "disposition",
        ].includes(key),
    )
  );
}

export function summarizeKeyReview(
  servers: KeyServer[],
  grants: AgentPolicyGrantForm[],
): {
  hasUnmappedAccess: boolean;
  hasBroadAccess: boolean;
  servers: Array<{ server: KeyServer; access: ReviewAccess[] }>;
} {
  const hasUnmappedAccess = grants.some(
    (grant) =>
      !supported(grant) || !servers.some((server) => matches(grant, server)),
  );
  const hasBroadAccess = grants.some(
    (grant) =>
      grant.selector.resourceId === "*" ||
      grant.selector.projectId === undefined ||
      grant.selector.projectId === "*",
  );
  return {
    hasUnmappedAccess,
    hasBroadAccess,
    servers: servers.map((server) => {
      const groups = new Map<string, ReviewAccess>();
      for (const grant of grants) {
        if (!supported(grant) || !matches(grant, server)) continue;
        const { disposition, tool } = grant.selector;
        // Union tools only within the SAME action and condition. Combining
        // dispositions independently would claim a Cartesian product of access.
        const key = JSON.stringify([grant.scope, disposition]);
        let group = groups.get(key);
        if (!group) {
          group = {
            action: ACTIONS[grant.scope]!,
            condition: disposition ? DISPOSITIONS[disposition] : undefined,
            allTools: false,
            tools: [],
          };
          groups.set(key, group);
        }
        if (tool === undefined || tool === "*") {
          if (disposition === undefined) group.allTools = true;
          else group.categoryTools = true;
        } else if (!group.tools.includes(tool)) group.tools.push(tool);
      }
      return {
        server,
        access: [...groups.values()].map((group) => ({
          ...group,
          tools: group.allTools || group.categoryTools ? [] : group.tools,
        })),
      };
    }),
  };
}
