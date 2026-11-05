/**
 * The role editor's access matrix: a role's grants read as rows (All servers,
 * a project, a server, a platform area) by levels (Use, View, Manage) plus a
 * Blocked column.
 *
 * For MCP servers a cell is exactly one stored row: `(mcp:connect, server)` is
 * Use on that server, `(mcp:blocked_read, server)` is View blocked. The matrix
 * edits those rows and nothing else. Rows it has no cell for (a tool on every
 * server, a server URL, a block on one tool) are listed as other rules and
 * kept as stored, so opening and saving a role never changes access the
 * admin did not touch.
 */

import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type {
  Disposition,
  Selector,
} from "@gram/client/models/components/selector.js";

import type { ToolLimit } from "./mcpAccessModel";
import type { ResourceType, RoleGrant, ScopeRule } from "./types";

export type Level = "use" | "view" | "manage";

/** Lowest to highest: each level includes the ones before it. */
export const LEVELS: readonly Level[] = ["use", "view", "manage"];

export const LEVEL_LABEL: Record<Level, string> = {
  use: "Connect",
  view: "View",
  manage: "Manage",
};

/** The MCP scope each level stores as (scopeExpansions in authz/scopes.go). */
const MCP_LEVEL_SCOPE: Record<Level, Scope> = {
  use: "mcp:connect" as Scope,
  view: "mcp:read" as Scope,
  manage: "mcp:write" as Scope,
};

export function levelRank(level: Level): number {
  return LEVELS.indexOf(level);
}

// ─── Grant plumbing ─────────────────────────────────────────────────────────

interface Flat {
  allowAll: boolean;
  allow: Selector[];
  denyAll: boolean;
  deny: Selector[];
}

function flatten(grant: RoleGrant | undefined): Flat {
  const flat: Flat = { allowAll: false, allow: [], denyAll: false, deny: [] };
  for (const rule of grant?.rules ?? []) {
    if (rule.effect === "allow") {
      if (rule.selectors === null) flat.allowAll = true;
      else flat.allow.push(...rule.selectors);
    } else if (rule.selectors === null) {
      flat.denyAll = true;
    } else {
      flat.deny.push(...rule.selectors);
    }
  }
  return flat;
}

function unflatten(scope: Scope, flat: Flat): RoleGrant | undefined {
  const rules: ScopeRule[] = [];
  if (flat.allowAll || flat.allow.length > 0) {
    rules.push({
      id: crypto.randomUUID(),
      effect: "allow",
      selectors: flat.allowAll ? null : flat.allow,
    });
  }
  if (flat.denyAll || flat.deny.length > 0) {
    rules.push({
      id: crypto.randomUUID(),
      effect: "deny",
      selectors: flat.denyAll ? null : flat.deny,
    });
  }
  return rules.length > 0 ? { scope, rules } : undefined;
}

function withGrant(
  grants: Record<string, RoleGrant>,
  scope: Scope,
  grant: RoleGrant | undefined,
): Record<string, RoleGrant> {
  const next = { ...grants };
  if (grant) next[scope] = grant;
  else delete next[scope];
  return next;
}

function editFlat(
  grants: Record<string, RoleGrant>,
  scope: Scope,
  edit: (flat: Flat) => Flat,
): Record<string, RoleGrant> {
  return withGrant(
    grants,
    scope,
    unflatten(scope, edit(flatten(grants[scope]))),
  );
}

// ─── MCP rows ───────────────────────────────────────────────────────────────

export type McpRow =
  | { kind: "all" }
  | { kind: "project"; projectId: string }
  | { kind: "server"; serverId: string; projectId: string };

export function mcpRowKey(row: McpRow): string {
  switch (row.kind) {
    case "all":
      return "all";
    case "project":
      return `project:${row.projectId}`;
    case "server":
      return `server:${row.serverId}`;
  }
}

/** The row above this one: a server sits in its project, a project in All. */
export function parentRow(row: McpRow): McpRow | null {
  switch (row.kind) {
    case "all":
      return null;
    case "project":
      return { kind: "all" };
    case "server":
      return { kind: "project", projectId: row.projectId };
  }
}

function isRowSelector(s: Selector, row: McpRow): boolean {
  if (s.resourceKind !== "mcp" || s.serverUrl) return false;
  switch (row.kind) {
    case "all":
      return s.resourceId === "*" && !s.projectId;
    case "project":
      return s.resourceId === "*" && s.projectId === row.projectId;
    case "server":
      // Platform-provisioned access names the server's project too.
      return (
        s.resourceId === row.serverId &&
        (!s.projectId || s.projectId === row.projectId)
      );
  }
}

/** A selector the row's cell stores as: no tool, no annotation. */
function isPlainRowSelector(s: Selector, row: McpRow): boolean {
  return isRowSelector(s, row) && !s.tool && !s.disposition;
}

/** Selectors Use on this row is limited to: tools or annotations on it. */
function isToolLimitSelector(s: Selector, row: McpRow): boolean {
  if (!isRowSelector(s, row) || (s.tool && s.disposition)) return false;
  if (row.kind === "server") return !!s.tool || !!s.disposition;
  // Every server has no tool list to pick from; annotations only.
  return row.kind === "all" && !!s.disposition;
}

/**
 * Use on a server limited both to tools and to annotations, which the tool
 * sheet cannot show at once. The tools are the cell; the annotation rows stay
 * as stored and are listed as other rules.
 */
function isHiddenAnnotationLimit(
  allow: Selector[],
  s: Selector,
  row: McpRow,
): boolean {
  return (
    row.kind === "server" &&
    !!s.disposition &&
    isToolLimitSelector(s, row) &&
    allow.some((t) => !!t.tool && isToolLimitSelector(t, row))
  );
}

function cellSelectors(row: McpRow, level: Level): (s: Selector) => boolean {
  return (s) =>
    isPlainRowSelector(s, row) ||
    (level === "use" && isToolLimitSelector(s, row));
}

function rowSelector(row: McpRow): Selector {
  switch (row.kind) {
    case "all":
      return { resourceKind: "mcp", resourceId: "*" };
    case "project":
      return { resourceKind: "mcp", resourceId: "*", projectId: row.projectId };
    case "server":
      return { resourceKind: "mcp", resourceId: row.serverId };
  }
}

/** Whether the role itself grants this level on this row. */
export function mcpAllows(
  grants: Record<string, RoleGrant>,
  row: McpRow,
  level: Level,
): boolean {
  const flat = flatten(grants[MCP_LEVEL_SCOPE[level]]);
  if (row.kind === "all" && flat.allowAll) return true;
  return flat.allow.some(cellSelectors(row, level));
}

/** Whether the role itself blocks this level on this row. */
export function mcpBlocks(
  grants: Record<string, RoleGrant>,
  row: McpRow,
  level: Level,
): boolean {
  const flat = flatten(grants[MCP_LEVEL_SCOPE[level]]);
  if (row.kind === "all" && flat.denyAll) return true;
  return flat.deny.some((s) => isPlainRowSelector(s, row));
}

/** Grant or take back one level on one row. Tool limits on Use go with it. */
export function setMcpAllow(
  grants: Record<string, RoleGrant>,
  row: McpRow,
  level: Level,
  on: boolean,
): Record<string, RoleGrant> {
  return editFlat(grants, MCP_LEVEL_SCOPE[level], (flat) => {
    const matches = cellSelectors(row, level);
    const allow = flat.allow.filter((s) => !matches(s));
    if (row.kind === "all") return { ...flat, allowAll: on, allow };
    return { ...flat, allow: on ? [...allow, rowSelector(row)] : allow };
  });
}

/** Block or unblock one level on one row. */
export function setMcpBlock(
  grants: Record<string, RoleGrant>,
  row: McpRow,
  level: Level,
  on: boolean,
): Record<string, RoleGrant> {
  return editFlat(grants, MCP_LEVEL_SCOPE[level], (flat) => {
    const deny = flat.deny.filter((s) => !isPlainRowSelector(s, row));
    if (row.kind === "all") return { ...flat, denyAll: on, deny };
    return { ...flat, deny: on ? [...deny, rowSelector(row)] : deny };
  });
}

/**
 * The Blocked column: block every level on the row, or none. Blocking also
 * takes back what the role grants on the row; a role that both grants and
 * blocks the same server says nothing an admin can act on.
 */
export function setMcpRowBlocked(
  grants: Record<string, RoleGrant>,
  row: McpRow,
  on: boolean,
): Record<string, RoleGrant> {
  let next = grants;
  for (const level of LEVELS) {
    next = setMcpBlock(next, row, level, on);
    if (on) next = setMcpAllow(next, row, level, false);
  }
  return next;
}

/** Which tools Use on this row reaches; null when the row has no Use. */
export function mcpToolLimit(
  grants: Record<string, RoleGrant>,
  row: McpRow,
): ToolLimit | null {
  const flat = flatten(grants[MCP_LEVEL_SCOPE.use]);
  if (
    (row.kind === "all" && flat.allowAll) ||
    flat.allow.some((s) => isPlainRowSelector(s, row))
  ) {
    return { kind: "all" };
  }
  const limited = flat.allow.filter((s) => isToolLimitSelector(s, row));
  const tools = limited.flatMap((s) => (s.tool ? [s.tool] : []));
  if (tools.length > 0) return { kind: "tools", tools };
  const dispositions = limited.flatMap((s) =>
    s.disposition ? [s.disposition] : [],
  );
  if (dispositions.length > 0) return { kind: "annotations", dispositions };
  return null;
}

/** Limit Use on a row to some tools; the row keeps Use whatever the limit. */
export function setMcpToolLimit(
  grants: Record<string, RoleGrant>,
  row: McpRow,
  limit: ToolLimit,
): Record<string, RoleGrant> {
  return editFlat(grants, MCP_LEVEL_SCOPE.use, (flat) => {
    const inCell = cellSelectors(row, "use");
    const matches = (s: Selector) =>
      inCell(s) && !isHiddenAnnotationLimit(flat.allow, s, row);
    const allow = flat.allow.filter((s) => !matches(s));
    const base = rowSelector(row);
    switch (limit.kind) {
      case "all":
        if (row.kind === "all") return { ...flat, allowAll: true, allow };
        return { ...flat, allow: [...allow, base] };
      case "tools":
        return {
          ...flat,
          allowAll: row.kind === "all" ? false : flat.allowAll,
          allow: [...allow, ...limit.tools.map((tool) => ({ ...base, tool }))],
        };
      case "annotations":
        return {
          ...flat,
          allowAll: row.kind === "all" ? false : flat.allowAll,
          allow: [
            ...allow,
            ...limit.dispositions.map((disposition: Disposition) => ({
              ...base,
              disposition,
            })),
          ],
        };
    }
  });
}

/**
 * Limit Use on a server that gets Use from a broader row. Grants add up, so a
 * limit on the server alone could not take tools away. Instead the broader
 * Use is split: All servers becomes Use on each project (or on each server,
 * when it carries an annotation limit), a project becomes Use on each of its
 * servers, and this server gets the limit. Servers added later to a split
 * scope are no longer covered.
 */
export function splitMcpToolLimit(
  grants: Record<string, RoleGrant>,
  target: McpRow & { kind: "server" },
  limit: ToolLimit,
  servers: (McpRow & { kind: "server" })[],
): Record<string, RoleGrant> {
  const all: McpRow = { kind: "all" };
  const project: McpRow = { kind: "project", projectId: target.projectId };
  const fromAll = mcpAllows(grants, all, "use");
  const fromProject = mcpAllows(grants, project, "use");
  const allLimit = mcpToolLimit(grants, all) ?? { kind: "all" };

  let next = grants;
  if (fromAll) next = setMcpAllow(next, all, "use", false);
  if (fromProject) next = setMcpAllow(next, project, "use", false);

  /** Give a row Use with a limit, keeping any Use it already had. */
  const grantUse = (row: McpRow, rowLimit: ToolLimit) => {
    const own = mcpToolLimit(next, row);
    if (own?.kind === "all") return;
    if (!own || rowLimit.kind === "all") {
      next = setMcpToolLimit(next, row, rowLimit);
      return;
    }
    // Both limited: keep the server's own rows and add the broader ones.
    const base = rowSelector(row);
    const extra: Selector[] =
      rowLimit.kind === "tools"
        ? rowLimit.tools.map((tool) => ({ ...base, tool }))
        : rowLimit.dispositions.map((disposition) => ({
            ...base,
            disposition,
          }));
    next = editFlat(next, MCP_LEVEL_SCOPE.use, (flat) => ({
      ...flat,
      allow: [...flat.allow, ...extra],
    }));
  };

  if (fromAll) {
    const projectIds = new Set(servers.map((row) => row.projectId));
    for (const projectId of projectIds) {
      // A project row can only hold Use on every tool.
      if (projectId !== target.projectId && allLimit.kind === "all") {
        grantUse({ kind: "project", projectId }, allLimit);
      }
    }
  }
  for (const row of servers) {
    if (row.serverId === target.serverId) continue;
    if (mcpAllows(next, { kind: "project", projectId: row.projectId }, "use")) {
      continue;
    }
    const inProject = row.projectId === target.projectId;
    if (inProject && fromProject) grantUse(row, { kind: "all" });
    else if (fromAll && (inProject || allLimit.kind !== "all")) {
      grantUse(row, allLimit);
    }
  }
  return setMcpToolLimit(next, target, limit);
}

/**
 * Limit Use on a row. A server that also gets Use from a row above splits
 * that row, whether or not it has Use of its own; otherwise the broader Use
 * would still reach every tool.
 */
export function limitMcpUse(
  grants: Record<string, RoleGrant>,
  row: McpRow,
  limit: ToolLimit,
  servers: (McpRow & { kind: "server" })[],
): Record<string, RoleGrant> {
  if (row.kind !== "server") return setMcpToolLimit(grants, row, limit);
  const inherited =
    mcpAllows(grants, { kind: "all" }, "use") ||
    mcpAllows(grants, { kind: "project", projectId: row.projectId }, "use");
  return inherited
    ? splitMcpToolLimit(grants, row, limit, servers)
    : setMcpToolLimit(grants, row, limit);
}

/** Take back every MCP grant and block, including other rules. */
export function clearMcp(
  grants: Record<string, RoleGrant>,
): Record<string, RoleGrant> {
  let next = grants;
  for (const level of LEVELS) {
    next = withGrant(next, MCP_LEVEL_SCOPE[level], undefined);
  }
  return next;
}

/** A stored MCP row the matrix has no cell for. */
export interface OtherMcpRule {
  key: string;
  level: Level;
  blocked: boolean;
  selector: Selector;
}

/**
 * Every MCP row the matrix cannot show, given the rows it does show. A row for
 * a server no longer listed (deleted, or its MCP switched off) is one of them.
 */
export function otherMcpRules(
  grants: Record<string, RoleGrant>,
  rows: McpRow[],
): OtherMcpRule[] {
  const result: OtherMcpRule[] = [];
  for (const level of LEVELS) {
    const flat = flatten(grants[MCP_LEVEL_SCOPE[level]]);
    const shown = (s: Selector, blocked: boolean) =>
      rows.some((row) =>
        blocked
          ? isPlainRowSelector(s, row)
          : cellSelectors(row, level)(s) &&
            !isHiddenAnnotationLimit(flat.allow, s, row),
      );
    for (const [blocked, list] of [
      [false, flat.allow],
      [true, flat.deny],
    ] as const) {
      for (const selector of list) {
        if (shown(selector, blocked)) continue;
        result.push({
          key: JSON.stringify([level, blocked, selector]),
          level,
          blocked,
          selector,
        });
      }
    }
  }
  return result;
}

export function removeOtherMcpRule(
  grants: Record<string, RoleGrant>,
  rule: OtherMcpRule,
): Record<string, RoleGrant> {
  const key = (s: Selector) => JSON.stringify([rule.level, rule.blocked, s]);
  return editFlat(grants, MCP_LEVEL_SCOPE[rule.level], (flat) =>
    rule.blocked
      ? { ...flat, deny: flat.deny.filter((s) => key(s) !== rule.key) }
      : { ...flat, allow: flat.allow.filter((s) => key(s) !== rule.key) },
  );
}

// ─── Platform areas ─────────────────────────────────────────────────────────

/** One platform row: what View and Manage store as for one kind of resource. */
export interface PlatformArea {
  resourceType: ResourceType;
  label: string;
  /** What the area covers, in a few words. */
  description: string;
  view?: ScopeDefinition;
  manage?: ScopeDefinition;
  /** Permissions in this area that are not a level, e.g. authorizing agents. */
  extras: ScopeDefinition[];
}

const PLATFORM_AREAS: {
  resourceType: ResourceType;
  label: string;
  description: string;
}[] = [
  {
    resourceType: "org",
    label: "Organization",
    description: "Organization settings and members",
  },
  {
    resourceType: "project",
    label: "Projects",
    description: "Projects and what they contain",
  },
  {
    resourceType: "environment",
    label: "Environments",
    description: "Variables and secrets that servers use",
  },
  {
    resourceType: "skill",
    label: "Skills",
    description: "Skills shared in a project",
  },
  {
    resourceType: "inference",
    label: "Inference",
    description: "Run inference in selected projects",
  },
  {
    resourceType: "assistant",
    label: "Assistants",
    description: "Assistants in a project",
  },
  {
    resourceType: "agent",
    label: "Agents",
    description: "Agents and their settings",
  },
  {
    resourceType: "chat",
    label: "Agent sessions",
    description: "Other members' session transcripts",
  },
  {
    resourceType: "workload",
    label: "Workload identities",
    description: "Which workloads can act as which agents",
  },
];

/**
 * Names for the single permissions listed after the areas, in the same
 * name-plus-summary shape as the area rows. Unknown ones fall back to the
 * server's description.
 */
const EXTRA_COPY: Record<string, { label: string; description: string }> = {
  "inference:execute": {
    label: "Run inference",
    description: "Call the inference API in the selected projects",
  },
  "org:device_agent_sync": {
    label: "Device agent sync",
    description: "Agents fetch the plugins assigned to them",
  },
  "org:hooks_ingest": {
    label: "Hook events",
    description: "Agents send AI-tool hook events and telemetry",
  },
  "plugin:write": {
    label: "Plugins",
    description: "Edit and publish plugins in a project",
  },
  "agent:authorize": {
    label: "Agent credentials",
    description: "Authorize agents and manage their credentials",
  },
  "agent:transfer": {
    label: "Agent ownership",
    description: "Hand an agent to another owner",
  },
  "memory:read": {
    label: "Team memory",
    description: "Search and recall the project's team memory",
  },
  "memory:write": {
    label: "Team memos",
    description: "Save memos to the project's team memory",
  },
};

/**
 * Single permissions that a broader one already grants, so the matrix shows
 * them as included rather than off. Mirrors the server's scope expansions.
 */
export const EXTRA_INCLUDED_BY: Record<string, string> = {
  "memory:read": "memory:write",
};

export function extraCopy(scope: ScopeDefinition): {
  label: string;
  description: string;
} {
  return (
    EXTRA_COPY[scope.slug] ?? {
      label: scope.description ?? scope.slug,
      description: "",
    }
  );
}

const VIEW_ACTIONS = new Set(["read"]);
const MANAGE_ACTIONS = new Set(["write", "admin"]);

function scopeAction(slug: string): string {
  return slug.slice(slug.indexOf(":") + 1);
}

/** Group the permissions a role can hold into platform rows. MCP is its own table. */
export function platformAreas(scopes: ScopeDefinition[]): PlatformArea[] {
  return PLATFORM_AREAS.map(({ resourceType, label, description }) => {
    const inArea = scopes.filter(
      (s) =>
        s.resourceType === resourceType &&
        s.visibility === "user_visible" &&
        !s.slug.includes(":blocked_"),
    );
    const view = inArea.find((s) => VIEW_ACTIONS.has(scopeAction(s.slug)));
    const manage = inArea.find((s) => MANAGE_ACTIONS.has(scopeAction(s.slug)));
    // A family whose only level is a write (plugins) is an extra, not a row
    // with an empty View column.
    const levels = view ? [view, manage] : [];
    return {
      resourceType,
      label,
      description,
      view,
      manage: view ? manage : undefined,
      extras: inArea.filter((s) => !levels.includes(s)),
    };
  }).filter((area) => area.view || area.extras.length > 0);
}

export function platformAllows(
  grants: Record<string, RoleGrant>,
  scope: string,
): boolean {
  return (grants[scope]?.rules ?? []).some((r) => r.effect === "allow");
}

/** Grant a permission everywhere, or take every grant of it back. Blocks stay. */
export function setPlatformAllow(
  grants: Record<string, RoleGrant>,
  scope: Scope,
  on: boolean,
): Record<string, RoleGrant> {
  const rules = (grants[scope]?.rules ?? []).filter((r) => r.effect === "deny");
  if (on) {
    rules.unshift({
      id: crypto.randomUUID(),
      effect: "allow",
      selectors: null,
    });
  }
  return withGrant(
    grants,
    scope,
    rules.length > 0 ? { scope, rules } : undefined,
  );
}

/** "all" when the scope is blocked everywhere, "some" when only in places. */
export function platformBlock(
  grants: Record<string, RoleGrant>,
  scope: string | undefined,
): "all" | "some" | null {
  if (!scope) return null;
  const denies = (grants[scope]?.rules ?? []).filter(
    (r) => r.effect === "deny",
  );
  if (denies.some((r) => r.selectors === null)) return "all";
  return denies.length > 0 ? "some" : null;
}

const isBlockEverywhere = (rule: ScopeRule) =>
  rule.effect === "deny" && rule.selectors === null;

/** Take back a block everywhere on one permission. Narrower blocks stay. */
export function unblockPlatformScope(
  grants: Record<string, RoleGrant>,
  scope: Scope,
): Record<string, RoleGrant> {
  const rules = (grants[scope]?.rules ?? []).filter(
    (r) => !isBlockEverywhere(r),
  );
  return withGrant(
    grants,
    scope,
    rules.length > 0 ? { scope, rules } : undefined,
  );
}

/**
 * The Blocked column for a platform row. A block on View also blocks Manage
 * (scopeExpansions), so blocking stores on View alone and takes back what the
 * row grants; unblocking takes back the blocks everywhere on both. Blocks on
 * some projects stay either way.
 */
export function setPlatformBlocked(
  grants: Record<string, RoleGrant>,
  area: PlatformArea,
  on: boolean,
): Record<string, RoleGrant> {
  let next = grants;
  for (const def of [area.view, area.manage]) {
    if (!def) continue;
    const scope = def.slug as Scope;
    next = unblockPlatformScope(next, scope);
    if (!on) continue;
    const rules = (next[scope]?.rules ?? []).filter((r) => r.effect === "deny");
    next = withGrant(
      next,
      scope,
      rules.length > 0 ? { scope, rules } : undefined,
    );
  }
  const target = (area.view ?? area.manage)?.slug as Scope | undefined;
  if (on && target) {
    next = withGrant(next, target, {
      scope: target,
      rules: [
        ...(next[target]?.rules ?? []),
        { id: crypto.randomUUID(), effect: "deny", selectors: null },
      ],
    });
  }
  return next;
}

/** Take back every platform grant and block, extras included. */
export function clearPlatform(
  grants: Record<string, RoleGrant>,
  areas: PlatformArea[],
): Record<string, RoleGrant> {
  let next = grants;
  for (const area of areas) {
    for (const def of [area.view, area.manage, ...area.extras]) {
      if (def) next = withGrant(next, def.slug as Scope, undefined);
    }
  }
  return next;
}
