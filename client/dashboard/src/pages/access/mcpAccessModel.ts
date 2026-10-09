/**
 * The MCP access tab's view of a role's `mcp:connect` grant: one tool limit
 * per server instead of a list of rules.
 *
 * The grant model already stores this shape — each `(scope, selector)` pair is
 * its own row, rows for one scope are unioned, and `mcp:blocked_connect` rows
 * subtract — so the tab reads the selectors into per-server limits and writes
 * them back as selectors. Selectors this view cannot show (project-wide rows,
 * a tool paired with an annotation, a server URL) are kept as they were stored
 * rather than dropped, so opening and saving a role never widens or narrows
 * access it did not touch.
 */

import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type {
  Disposition,
  Selector,
} from "@gram/client/models/components/selector.js";

import type { RoleGrant } from "./types";
import type { Server, ServerGroup, ServerTool } from "./serverMerge";

export const MCP_CONNECT_SCOPE = "mcp:connect" as Scope;

/** The four tool annotations the backend checks, in the order shown. */
export const DISPOSITIONS: readonly Disposition[] = [
  "read_only",
  "idempotent",
  "destructive",
  "open_world",
];

export const DISPOSITION_COPY: Record<
  Disposition,
  { label: string; badge: string; description: string }
> = {
  read_only: {
    label: "Read-only",
    badge: "Read-Only Tools",
    description: "Only read data",
  },
  idempotent: {
    label: "Idempotent",
    badge: "Idempotent Tools",
    description: "Safe to repeat with the same result",
  },
  destructive: {
    label: "Destructive",
    badge: "Destructive Tools",
    description: "May delete or overwrite data",
  },
  open_world: {
    label: "Open-world",
    badge: "Open World Tools",
    description: "Reach systems outside the server",
  },
};

/** Which tools of a server (or of every server) a role can call. */
export type ToolLimit =
  | { kind: "all" }
  | { kind: "tools"; tools: string[] }
  | { kind: "annotations"; dispositions: Disposition[] };

export type ToolLimitKind = ToolLimit["kind"];

export interface McpConnectAccess {
  /**
   * Set when the role reaches every server, including ones added later; null
   * when it names servers one by one.
   */
  allServers: ToolLimit | null;
  /** Per-server limits, keyed by grant resource id. Saved only while `allServers` is null. */
  servers: Record<string, ToolLimit>;
  /** Servers an `mcp:blocked_connect` row names, keyed by grant resource id. */
  forbidden: string[];
  /** Allow selectors this view cannot show, kept as stored. */
  preservedAllow: Selector[];
  /** Exception selectors this view cannot show, kept as stored. */
  preservedDeny: Selector[];
  /** An unrestricted exception: the role is blocked from every server. */
  denyAll: boolean;
}

function isPlainMcpSelector(s: Selector): boolean {
  return s.resourceKind === "mcp" && !s.projectId && !s.serverUrl;
}

function collect(
  grant: RoleGrant | undefined,
  effect: "allow" | "deny",
): { unrestricted: boolean; selectors: Selector[] } {
  const rules = (grant?.rules ?? []).filter((r) => r.effect === effect);
  return {
    unrestricted: rules.some((r) => r.selectors === null),
    selectors: rules.flatMap((r) => r.selectors ?? []),
  };
}

function limitFromParts(
  full: boolean,
  tools: string[],
  dispositions: Disposition[],
): ToolLimit | null {
  if (full) return { kind: "all" };
  if (tools.length > 0) return { kind: "tools", tools };
  if (dispositions.length > 0) return { kind: "annotations", dispositions };
  return null;
}

function selectorsForLimit(resourceId: string, limit: ToolLimit): Selector[] {
  switch (limit.kind) {
    case "all":
      return [{ resourceKind: "mcp", resourceId }];
    case "tools":
      return limit.tools.map((tool) => ({
        resourceKind: "mcp",
        resourceId,
        tool,
      }));
    case "annotations":
      return limit.dispositions.map((disposition) => ({
        resourceKind: "mcp",
        resourceId,
        disposition,
      }));
  }
}

/** Read a role's `mcp:connect` grant into per-server limits. */
export function parseMcpConnectGrant(
  grant: RoleGrant | undefined,
): McpConnectAccess {
  const allow = collect(grant, "allow");
  const deny = collect(grant, "deny");
  const preservedAllow: Selector[] = [];
  const preservedDeny: Selector[] = [];

  let allFull = allow.unrestricted;
  const allDispositions: Disposition[] = [];
  const parts = new Map<
    string,
    { full: boolean; tools: string[]; dispositions: Disposition[] }
  >();
  const partsFor = (id: string) => {
    let entry = parts.get(id);
    if (!entry) {
      entry = { full: false, tools: [], dispositions: [] };
      parts.set(id, entry);
    }
    return entry;
  };

  for (const s of allow.selectors) {
    if (!isPlainMcpSelector(s) || (s.tool && s.disposition)) {
      preservedAllow.push(s);
    } else if (s.resourceId === "*") {
      if (s.tool) preservedAllow.push(s);
      else if (s.disposition) allDispositions.push(s.disposition);
      else allFull = true;
    } else if (s.tool) {
      partsFor(s.resourceId).tools.push(s.tool);
    } else if (s.disposition) {
      partsFor(s.resourceId).dispositions.push(s.disposition);
    } else {
      partsFor(s.resourceId).full = true;
    }
  }

  const allServers = limitFromParts(allFull, [], allDispositions);
  const servers: Record<string, ToolLimit> = {};
  for (const [id, part] of parts) {
    const limit = limitFromParts(part.full, part.tools, part.dispositions);
    if (!limit) continue;
    // A server is limited one way or the other, and a full row covers both.
    // The rows the limit does not show are kept as stored, so saving never
    // drops one.
    if (limit.kind !== "tools") {
      for (const tool of part.tools) {
        preservedAllow.push({ resourceKind: "mcp", resourceId: id, tool });
      }
    }
    if (limit.kind !== "annotations") {
      for (const disposition of part.dispositions) {
        preservedAllow.push({
          resourceKind: "mcp",
          resourceId: id,
          disposition,
        });
      }
    }
    if (allServers) {
      // With every server chosen there is no picker to show these in.
      preservedAllow.push(...selectorsForLimit(id, limit));
    } else {
      servers[id] = limit;
    }
  }

  const forbidden: string[] = [];
  for (const s of deny.selectors) {
    const serverLevel =
      isPlainMcpSelector(s) &&
      s.resourceId !== "*" &&
      !s.tool &&
      !s.disposition;
    if (serverLevel) {
      if (!forbidden.includes(s.resourceId)) forbidden.push(s.resourceId);
    } else {
      preservedDeny.push(s);
    }
  }

  return {
    allServers,
    servers,
    forbidden,
    preservedAllow,
    preservedDeny,
    denyAll: deny.unrestricted,
  };
}

/**
 * Write per-server limits back as an `mcp:connect` grant. Returns undefined
 * when nothing is granted or forbidden, so the scope leaves the role.
 */
export function serializeMcpConnectAccess(
  access: McpConnectAccess,
): RoleGrant | undefined {
  let allowUnrestricted = false;
  const allow: Selector[] = [...access.preservedAllow];
  if (access.allServers?.kind === "all") {
    allowUnrestricted = true;
  } else if (access.allServers) {
    allow.push(...selectorsForLimit("*", access.allServers));
  } else {
    for (const [id, limit] of Object.entries(access.servers)) {
      allow.push(...selectorsForLimit(id, limit));
    }
  }

  const deny: Selector[] = [
    ...access.preservedDeny,
    ...access.forbidden.map((resourceId): Selector => ({
      resourceKind: "mcp",
      resourceId,
    })),
  ];

  const rules: RoleGrant["rules"] = [];
  if (allowUnrestricted || allow.length > 0) {
    rules.push({
      id: crypto.randomUUID(),
      effect: "allow",
      selectors: allowUnrestricted ? null : allow,
    });
  }
  if (access.denyAll || deny.length > 0) {
    rules.push({
      id: crypto.randomUUID(),
      effect: "deny",
      selectors: access.denyAll ? null : deny,
    });
  }
  if (rules.length === 0) return undefined;
  return { scope: MCP_CONNECT_SCOPE, rules };
}

/**
 * Take a server out of the role entirely: its limit and every stored row that
 * names it, so unticking a server never leaves a row still granting it.
 */
export function withoutServer(
  access: McpConnectAccess,
  id: string,
): McpConnectAccess {
  const servers = { ...access.servers };
  delete servers[id];
  return {
    ...access,
    servers,
    preservedAllow: access.preservedAllow.filter((s) => s.resourceId !== id),
  };
}

/** Whether a role holding this limit can call at least one tool. */
export function limitGrantsAnything(limit: ToolLimit): boolean {
  switch (limit.kind) {
    case "all":
      return true;
    case "tools":
      return limit.tools.length > 0;
    case "annotations":
      return limit.dispositions.length > 0;
  }
}

/** Badge text summarizing a tool limit; empty for unlimited access. */
export function toolLimitBadges(limit: ToolLimit): string[] {
  switch (limit.kind) {
    case "all":
      return [];
    case "tools": {
      const count = limit.tools.length;
      if (count === 0) return ["No Tools"];
      return [`${count} ${count === 1 ? "Tool" : "Tools"}`];
    }
    case "annotations": {
      const badges = DISPOSITIONS.filter((d) =>
        limit.dispositions.includes(d),
      ).map((d) => DISPOSITION_COPY[d].badge);
      return badges.length === 0 ? ["No Tools"] : badges;
    }
  }
}

/**
 * The one disposition the runtime assigns a tool: the first of its hints set
 * to true, in the backend's priority order (server/internal/conv/tools.go).
 */
export function toolDisposition(tool: ServerTool): Disposition | null {
  const hints = tool.annotations;
  if (hints?.readOnlyHint) return "read_only";
  if (hints?.destructiveHint) return "destructive";
  if (hints?.idempotentHint) return "idempotent";
  if (hints?.openWorldHint) return "open_world";
  return null;
}

/**
 * Move a limit to another kind, carrying access over where the tools allow:
 * a tool list keeps the tools the annotations covered, and annotations keep
 * the ones the chosen tools carry.
 */
export function convertToolLimit(
  limit: ToolLimit,
  kind: ToolLimitKind,
  tools: ServerTool[],
): ToolLimit {
  if (limit.kind === kind) return limit;
  switch (kind) {
    case "all":
      return { kind: "all" };
    case "tools": {
      const names = tools
        .filter((tool) => {
          if (limit.kind === "all") return true;
          if (limit.kind !== "annotations") return false;
          const disposition = toolDisposition(tool);
          return !!disposition && limit.dispositions.includes(disposition);
        })
        .map((tool) => tool.name);
      return { kind: "tools", tools: names };
    }
    case "annotations": {
      if (limit.kind === "all") {
        return { kind: "annotations", dispositions: [...DISPOSITIONS] };
      }
      const chosen = new Set(limit.kind === "tools" ? limit.tools : []);
      const dispositions = DISPOSITIONS.filter((d) =>
        tools.some(
          (tool) => chosen.has(tool.name) && toolDisposition(tool) === d,
        ),
      );
      return { kind: "annotations", dispositions };
    }
  }
}

export type McpAdminScope = "mcp:read" | "mcp:write";

/**
 * Servers this role administers through `mcp:read` or `mcp:write`. Both
 * satisfy `mcp:connect` with every tool (scopeExpansions in
 * server/internal/authz/scopes.go), so the MCP access tab shows these servers
 * as always on. `mcp:write` is reported when the role holds both.
 */
export function adminCoverage(
  grants: Record<string, RoleGrant>,
  groups: ServerGroup[],
): Map<string, McpAdminScope> {
  const coverage = new Map<string, McpAdminScope>();
  for (const scope of ["mcp:read", "mcp:write"] as const) {
    const allow = collect(grants[scope], "allow");
    if (!grants[scope]) continue;
    for (const group of groups) {
      for (const server of group.servers) {
        const covered =
          allow.unrestricted ||
          allow.selectors.some(
            (s) =>
              s.resourceKind === "mcp" &&
              (s.resourceId === server.id ||
                (s.resourceId === "*" &&
                  (!s.projectId || s.projectId === group.projectId))),
          );
        if (covered) coverage.set(server.id, scope);
      }
    }
  }
  return coverage;
}

/** Subsequence match, so "gdrv" finds "Google Drive". */
export function fuzzyMatch(query: string, text: string): boolean {
  const q = query.toLowerCase().replace(/\s+/g, "");
  if (!q) return true;
  const t = text.toLowerCase();
  let i = 0;
  for (const ch of t) {
    if (ch === q[i]) i++;
    if (i === q.length) return true;
  }
  return false;
}

export interface ServerWithProject {
  server: Server;
  projectId: string;
  projectName: string;
}

/** Every server in the inventory, keyed by grant resource id. */
export function indexServers(
  groups: ServerGroup[],
): Map<string, ServerWithProject> {
  const index = new Map<string, ServerWithProject>();
  for (const group of groups) {
    for (const server of group.servers) {
      index.set(server.id, {
        server,
        projectId: group.projectId,
        projectName: group.projectName,
      });
    }
  }
  return index;
}

/** The short handle shown under a server's name: its MCP slug when it has one. */
export function serverHandle(server: Server): string {
  return server.mcpSlug ?? server.slug;
}
