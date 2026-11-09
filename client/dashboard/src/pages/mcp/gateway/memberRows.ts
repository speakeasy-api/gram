import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpMember } from "@gram/client/models/components/metamcpmember.js";

/**
 * Backend-attested member classification. "proxied" (remote/tunneled) members
 * report unknown until the runtime holds live sessions (AGE-3291 PR 2);
 * "hosted" members are servers Speakeasy hosts, which gateways refuse, so they
 * appear only as memberships written before that rule; "disabled",
 * "unproxied" and "slugless" members have no gateway dispatch path either.
 * Every class but "proxied" is excluded from serving.
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

/** Add member candidates: existing servers a gateway can serve or show why not. */
export type AddCandidate = McpServer;

export function buildAddCandidates(
  servers: McpServer[],
  memberServerIds: Set<string>,
  search: string,
): AddCandidate[] {
  const query = search.trim().toLowerCase();
  const matches = (...fields: (string | undefined)[]) =>
    !query || fields.some((f) => f?.toLowerCase().includes(query));
  // Gateways never hold a hosted server, so none is offered.
  return servers
    .filter((s) => !s.toolsetId)
    .filter((s) => !memberServerIds.has(s.id))
    .filter((s) => matches(s.name, s.slug))
    .sort(
      (a, b) =>
        candidateName(a).localeCompare(candidateName(b)) ||
        candidateKey(a).localeCompare(candidateKey(b)),
    );
}

export function candidateName(candidate: AddCandidate): string {
  return candidate.name ?? "";
}

export function candidateKey(candidate: AddCandidate): string {
  return `server-${candidate.id}`;
}

export interface AddBatchState {
  orders: Map<string, number>;
  completed: Set<string>;
}

/** Fresh membership supersedes completed writes, not failed retries. */
export function reconcileCompletedMembers(
  state: AddBatchState,
  memberServerIds: ReadonlySet<string>,
): void {
  for (const key of state.completed) {
    if (!memberServerIds.has(key.slice("server-".length))) {
      state.completed.delete(key);
      state.orders.delete(key);
    }
  }
}

export interface AddResult {
  key: string;
  name: string;
  error?: string;
}

/** Allocate order before writing; retries keep their slot and successful writes. */
export async function addCandidateBatch(
  candidates: AddCandidate[],
  startOrder: number,
  state: AddBatchState,
  dependencies: {
    attach: (serverId: string, sortOrder: number) => Promise<void>;
  },
): Promise<AddResult[]> {
  let order = Math.max(
    startOrder,
    ...Array.from(state.orders.values(), (n) => n + 1),
  );
  for (const candidate of candidates) {
    const key = candidateKey(candidate);
    if (!state.orders.has(key)) state.orders.set(key, order++);
  }
  const results: AddResult[] = [];
  for (const candidate of candidates) {
    const key = candidateKey(candidate);
    const result: AddResult = {
      key,
      name: candidateName(candidate) || "MCP server",
    };
    try {
      if (!state.completed.has(key)) {
        await dependencies.attach(candidate.id, state.orders.get(key)!);
        state.completed.add(key);
      }
    } catch (error) {
      result.error =
        error instanceof Error
          ? error.message
          : "Couldn't add this server. Try again.";
    }
    results.push(result);
  }
  return results;
}
