/**
 * Tool limits for a role's Use access, and the server inventory the role
 * editor's access grid reads.
 */

import type { Disposition } from "@gram/client/models/components/selector.js";

import type { Server, ServerGroup, ServerTool } from "./serverMerge";

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
