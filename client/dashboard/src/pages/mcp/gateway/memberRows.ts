import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpMember } from "@gram/client/models/components/metamcpmember.js";

/**
 * Backend-attested member classification. "hosted" (toolset-backed) members
 * execute in-process and are always available; "proxied" (remote/tunneled)
 * members report unknown until the runtime holds live sessions (AGE-3291
 * PR 2); "unproxied" and slugless members have no gateway dispatch path and
 * are excluded from serving.
 */
export type MemberClassification =
  | "hosted"
  | "proxied"
  | "unproxied"
  | "slugless"
  | "unknown";

export interface MemberRow {
  member: MetaMcpMember;
  server: McpServer | undefined;
  classification: MemberClassification;
}

export function classifyMemberServer(
  server: McpServer | undefined,
): MemberClassification {
  if (!server) return "unknown";
  if (server.unproxiedMcpServerId) return "unproxied";
  if (!server.slug) return "slugless";
  if (server.toolsetId) return "hosted";
  if (server.remoteMcpServerId || server.tunneledMcpServerId) return "proxied";
  return "unknown";
}

/** Members joined with their servers, in sort order. */
export function buildMemberRows(
  members: MetaMcpMember[],
  servers: McpServer[],
): MemberRow[] {
  const serversById = new Map(servers.map((s) => [s.id, s]));
  return [...members]
    .sort(
      (a, b) =>
        a.sortOrder - b.sortOrder || a.mcpServerId.localeCompare(b.mcpServerId),
    )
    .map((member) => {
      const server = serversById.get(member.mcpServerId);
      return { member, server, classification: classifyMemberServer(server) };
    });
}

/**
 * Plan the updateMember calls for moving one row. Recomputes sequential
 * sortOrder over the whole target order and returns only the rows whose
 * stored value differs — robust to duplicate sortOrder values (all rows
 * default to 0).
 */
export function planReorder(
  orderedMembers: MetaMcpMember[],
  fromIndex: number,
  toIndex: number,
): { id: string; sortOrder: number }[] {
  if (
    fromIndex === toIndex ||
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= orderedMembers.length ||
    toIndex >= orderedMembers.length
  ) {
    return [];
  }
  const next = [...orderedMembers];
  const [moved] = next.splice(fromIndex, 1);
  next.splice(toIndex, 0, moved!);
  return next
    .map((member, index) => ({ id: member.id, sortOrder: index }))
    .filter(
      ({ id, sortOrder }) =>
        orderedMembers.find((m) => m.id === id)?.sortOrder !== sortOrder,
    );
}

/** sortOrder for a newly added member: after every existing member. */
export function nextSortOrder(members: MetaMcpMember[]): number {
  return members.reduce((max, m) => Math.max(max, m.sortOrder + 1), 0);
}
