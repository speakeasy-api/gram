import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpMember } from "@gram/client/models/components/metamcpmember.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";

/**
 * Backend-attested member classification. "hosted" (toolset-backed) members
 * execute in-process and are always available; "proxied" (remote/tunneled)
 * members report unknown until the runtime holds live sessions (AGE-3291
 * PR 2); "disabled", "unproxied" and "slugless" members have no gateway
 * dispatch path and are excluded from serving.
 */
export type MemberClassification =
  | "hosted"
  | "proxied"
  | "disabled"
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
  // ListServableMetaMCPMembers drops disabled servers whatever their backend.
  if (server.visibility === "disabled") return "disabled";
  if (server.unproxiedMcpServerId) return "unproxied";
  if (!server.slug) return "slugless";
  if (server.toolsetId) return "hosted";
  if (server.remoteMcpServerId || server.tunneledMcpServerId) return "proxied";
  return "unknown";
}

/**
 * Members joined with their backing servers, in the order the API returned
 * them. listMetaMcpMembers already orders by (sort_order, created_at, id) —
 * the same ordering the runtime's servable query uses — so re-sorting here
 * could only disagree with what `list_servers` actually serves.
 */
export function buildMemberRows(
  members: MetaMcpMember[],
  servers: McpServer[],
): MemberRow[] {
  const serversById = new Map(servers.map((s) => [s.id, s]));
  return members.map((member) => {
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

/**
 * Display-only backend kind. Finer-grained than MemberClassification:
 * "proxied" splits into the remote/tunneled story the operator actually
 * configured, without disturbing the classification logic serving depends on.
 */
export type MemberBackendKind = "hosted" | "remote" | "tunneled" | undefined;

export function memberBackendKind(
  server: McpServer | undefined,
): MemberBackendKind {
  if (!server) return undefined;
  // Unproxied trumps any backend id, matching classifyMemberServer: such a
  // member is not gateway-dispatchable, so naming a kind would mislead.
  if (server.unproxiedMcpServerId) return undefined;
  if (server.toolsetId) return "hosted";
  if (server.tunneledMcpServerId) return "tunneled";
  if (server.remoteMcpServerId) return "remote";
  return undefined;
}

/**
 * Add member candidates: existing mcp_servers rows, plus toolsets with no
 * toolset-backed row yet (adding one mints the row, then attaches it).
 */
export type AddCandidate =
  | { kind: "server"; server: McpServer }
  | { kind: "toolset"; toolset: ToolsetEntry };

export function buildAddCandidates(
  servers: McpServer[],
  toolsets: ToolsetEntry[],
  memberServerIds: Set<string>,
  search: string,
): AddCandidate[] {
  const query = search.trim().toLowerCase();
  const matches = (...fields: (string | undefined)[]) =>
    !query || fields.some((f) => f?.toLowerCase().includes(query));
  const wrappedToolsetIds = new Set(
    servers.flatMap((s) => (s.toolsetId ? [s.toolsetId] : [])),
  );

  const serverCandidates: AddCandidate[] = servers
    .filter((s) => !memberServerIds.has(s.id))
    .filter((s) => matches(s.name, s.slug))
    .map((server) => ({ kind: "server", server }));
  const toolsetCandidates: AddCandidate[] = toolsets
    .filter((t) => !wrappedToolsetIds.has(t.id))
    .filter((t) => matches(t.name, t.slug))
    .map((toolset) => ({ kind: "toolset", toolset }));

  return [...serverCandidates, ...toolsetCandidates].sort((a, b) =>
    candidateName(a).localeCompare(candidateName(b)),
  );
}

function candidateName(candidate: AddCandidate): string {
  return candidate.kind === "server"
    ? (candidate.server.name ?? "")
    : candidate.toolset.name;
}
