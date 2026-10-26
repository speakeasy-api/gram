import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";
import type { Key } from "@gram/client/models/components/key.js";
import type { ResourceLinkMaps } from "@/pages/access/useResourceLinkMaps";

import { ANY_RESOURCE, delegableGrantKey } from "./agent-api-key-grants";

interface ScopedGrant {
  scope: string;
  selector: AgentPolicySelector;
}

const PERMISSION_LABELS: Record<string, string> = {
  "mcp:connect": "Use tools",
  "mcp:read": "View MCP servers",
  "mcp:write": "Manage MCP servers",
  "mcp:auth": "Use connected accounts",
  "project:read": "View projects",
  "project:write": "Manage projects",
  "environment:read": "View environments",
  "environment:write": "Manage environments",
  "skill:read": "View skills",
  "skill:write": "Manage skills",
  "assistant:read": "View assistants",
  "assistant:write": "Manage assistants",
  "inference:execute": "Run inference",
  "risk_policy:evaluate": "Evaluate risk policies",
  "org:device_agent_sync": "Sync device agent plugins",
  "org:hooks_ingest": "Send hook events",
};

/** A scope in the user's words; an unknown scope falls back to its slug. */
export function permissionLabel(scope: string): string {
  return PERMISSION_LABELS[scope] ?? scope;
}

function isBlock(scope: string): boolean {
  return scope.includes(":blocked_");
}

/**
 * A delegable candidate that is not tied to the servers picked in the key
 * wizard. Server discovery only ever yields `mcp:connect`, so everything else
 * the agent may delegate (viewing projects, running inference…) comes from
 * unscoped discovery and is offered alongside the servers.
 */
export function isAgentWidePermission(grant: {
  scope: string;
  effect?: string;
}): boolean {
  return (
    grant.scope !== "mcp:connect" &&
    !isBlock(grant.scope) &&
    (grant.effect === undefined || grant.effect === "allow")
  );
}

/** The grants a key was issued with, or null when it follows the live policy. */
export function keyGrants(key: Key): ScopedGrant[] | null {
  const requested = key.delegatedGrants?.requested;
  return requested && requested.length > 0 ? requested : null;
}

function holds(grants: ScopedGrant[], wanted: ScopedGrant): boolean {
  return grants.some(
    ({ scope, selector }) =>
      scope === wanted.scope &&
      selector.resourceKind === wanted.selector.resourceKind &&
      (selector.resourceId === ANY_RESOURCE ||
        selector.resourceId === wanted.selector.resourceId),
  );
}

/**
 * Agent permissions added after the key was issued that it does not carry.
 * A key stores its own grants at issuance and every request is limited to
 * what the key, the agent and its owner all allow, so these never reach the
 * key until it is reissued. Permissions the agent already had when the key
 * was issued are left out: leaving them off was the issuer's choice.
 */
export function permissionsMissingFromKey(
  key: Key,
  agentGrants: AgentPolicyGrant[],
): AgentPolicyGrant[] {
  const issued = keyGrants(key);
  if (!issued) return [];
  return agentGrants.filter(
    (grant) =>
      grant.effect === "allow" &&
      !isBlock(grant.scope) &&
      grant.createdAt.getTime() > key.createdAt.getTime() &&
      !holds(issued, grant),
  );
}

function resourceName(
  selector: AgentPolicySelector,
  maps: ResourceLinkMaps,
): string | undefined {
  const { resourceKind, resourceId } = selector;
  if (resourceKind === "project") return maps.projectMap.get(resourceId)?.name;
  if (resourceKind === "mcp")
    return (
      maps.toolsetMap.get(resourceId)?.name ??
      maps.mcpServerMap.get(resourceId)?.name
    );
  return undefined;
}

function everyResourceLabel(resourceKind: string): string {
  switch (resourceKind) {
    case "mcp":
      return "all MCP servers";
    case "project":
      return "all projects";
    case "environment":
      return "all environments";
    case "skill":
      return "all skills";
    case "assistant":
      return "all assistants";
    default:
      return "everything";
  }
}

function countedNoun(resourceKind: string, count: number): string {
  const nouns: Record<string, [string, string]> = {
    mcp: ["MCP server", "MCP servers"],
    project: ["project", "projects"],
    environment: ["environment", "environments"],
    skill: ["skill", "skills"],
    assistant: ["assistant", "assistants"],
  };
  const [one, many] = nouns[resourceKind] ?? ["resource", "resources"];
  return `${count} ${count === 1 ? one : many}`;
}

const NAMED_LIMIT = 3;

/**
 * One phrase per permission, e.g. "Use tools on Linear and GitHub" or
 * "View projects on all projects". Resources that cannot be named are
 * counted rather than shown by ID.
 */
export function summarizePermissions(
  grants: ScopedGrant[],
  maps: ResourceLinkMaps,
): string[] {
  const byScope = new Map<string, ScopedGrant[]>();
  for (const grant of grants) {
    if (isBlock(grant.scope)) continue;
    const group = byScope.get(grant.scope);
    if (group) group.push(grant);
    else byScope.set(grant.scope, [grant]);
  }
  return [...byScope].map(([scope, group]) => {
    const label = permissionLabel(scope);
    const kind = group[0]!.selector.resourceKind;
    if (kind === "org") return label;
    if (group.some(({ selector }) => selector.resourceId === ANY_RESOURCE))
      return `${label} on ${everyResourceLabel(kind)}`;
    const ids = [...new Set(group.map(({ selector }) => selector.resourceId))];
    const named = ids
      .map((id) =>
        resourceName(
          group.find(({ selector }) => selector.resourceId === id)!.selector,
          maps,
        ),
      )
      .filter((name): name is string => !!name);
    if (named.length < ids.length || named.length > NAMED_LIMIT)
      return named.length > 0 && named.length <= NAMED_LIMIT
        ? `${label} on ${named.join(", ")} and ${ids.length - named.length} more`
        : `${label} on ${countedNoun(kind, ids.length)}`;
    return `${label} on ${joinNames(named)}`;
  });
}

function joinNames(names: string[]): string {
  if (names.length <= 1) return names.join("");
  return `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
}

/** Merge candidate lists, keeping the first of any duplicate. */
export function uniqueGrants(
  grants: AgentPolicyGrantForm[],
): AgentPolicyGrantForm[] {
  return [
    ...new Map(
      grants.map((grant) => [delegableGrantKey(grant), grant]),
    ).values(),
  ];
}
