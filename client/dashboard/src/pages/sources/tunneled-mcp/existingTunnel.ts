import { isConflictError, isNotFoundError } from "@/lib/route-errors";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";

// Query parameter on the tunneled create page that preselects an existing
// tunnel, so "Add MCP server on this tunnel" lands on the right choice.
export const EXISTING_TUNNEL_SEARCH_PARAM = "tunnel";

export function addMcpServerOnTunnelHref(
  createHref: string,
  tunneledMcpServerId: string,
): string {
  return `${createHref}?${EXISTING_TUNNEL_SEARCH_PARAM}=${encodeURIComponent(tunneledMcpServerId)}`;
}

const HTTP_REQUEST_TIMEOUT = 408;

// The server answered and refused, so the request changed nothing. Anything
// else (a network failure, a timeout, a server fault) leaves open whether the
// write committed before the response was lost.
export function isDefiniteRejection(error: unknown): boolean {
  if (!(error instanceof ServiceError)) return false;
  if (error.timeout || error.fault) return false;
  const status = error.statusCode;
  return status >= 400 && status < 500 && status !== HTTP_REQUEST_TIMEOUT;
}

// The tunnel delete refused because MCP servers still use the tunnel.
export function isTunnelInUseError(error: unknown): boolean {
  return isConflictError(error);
}

function sameIds(a: readonly string[], b: readonly string[]): boolean {
  if (a.length !== b.length) return false;
  const set = new Set(a);
  return b.every((id) => set.has(id));
}

function reasonOf(error: unknown): string {
  return error instanceof Error && error.message
    ? error.message
    : "request failed";
}

// The MCP servers on the tunnel differ from the set the user confirmed. No
// write was made; the dialog must show the current set and ask again.
export class TunnelServersChangedError extends Error {
  constructor() {
    super(
      "The MCP servers on this tunnel changed since you reviewed them. Nothing was deleted. Review the current list and confirm again.",
    );
    this.name = "TunnelServersChangedError";
  }
}

// Part of the delete may have been applied. `progressed` says whether any MCP
// server delete committed or may have committed, in which case the page the
// user started from may already be gone and the caller has to work out what
// is left before offering a way to finish.
export class TunnelDeleteIncompleteError extends Error {
  readonly progressed: boolean;

  constructor(message: string, progressed: boolean) {
    super(message);
    this.name = "TunnelDeleteIncompleteError";
    this.progressed = progressed;
  }
}

function plural(count: number, noun: string): string {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
}

type TunnelDeleteDeps = {
  /** The MCP server ids the user reviewed and confirmed. */
  confirmedIds: readonly string[];
  /** The MCP servers on the tunnel right now, bypassing any cache. */
  listLinked: () => Promise<McpServer[]>;
  deleteMcpServer: (id: string) => Promise<unknown>;
  deleteTunnel: () => Promise<unknown>;
};

// Deletes exactly the MCP servers the user confirmed, then the tunnel. It
// first re-reads the tunnel's servers and makes no write if they differ from
// the confirmed set, so a server added (or moved away) after review is never
// deleted unseen. A server added after that re-read survives: the backend
// refuses to delete a tunnel still in use, and this reports it.
export async function deleteTunnelAndConfirmedServers({
  confirmedIds,
  listLinked,
  deleteMcpServer,
  deleteTunnel,
}: TunnelDeleteDeps): Promise<void> {
  const current = (await listLinked()).map((server) => server.id);
  if (!sameIds(current, confirmedIds)) {
    throw new TunnelServersChangedError();
  }

  const results = await Promise.allSettled(
    confirmedIds.map((id) => deleteMcpServer(id)),
  );
  let deleted = 0;
  let refused = 0;
  let uncertain = 0;
  let firstRefusal: unknown;
  for (const result of results) {
    if (result.status === "fulfilled" || isNotFoundError(result.reason)) {
      deleted++;
    } else if (isDefiniteRejection(result.reason)) {
      refused++;
      firstRefusal ??= result.reason;
    } else {
      uncertain++;
    }
  }
  const total = confirmedIds.length;
  const progressed = deleted > 0 || uncertain > 0;

  if (deleted < total) {
    const parts = [`Deleted ${deleted} of ${plural(total, "MCP server")}.`];
    if (refused > 0) {
      parts.push(
        `${plural(refused, "MCP server")} could not be deleted (${reasonOf(firstRefusal)}).`,
      );
    }
    if (uncertain > 0) {
      parts.push(
        `The outcome for ${plural(uncertain, "MCP server")} is unknown because no definite response was received.`,
      );
    }
    parts.push("The tunnel was kept.");
    throw new TunnelDeleteIncompleteError(parts.join(" "), progressed);
  }

  try {
    await deleteTunnel();
  } catch (error) {
    if (isTunnelInUseError(error)) {
      throw new TunnelDeleteIncompleteError(
        `Deleted all ${plural(deleted, "MCP server")} you confirmed, but the tunnel was kept because other MCP servers still use it. Some may not be visible to you.`,
        progressed,
      );
    }
    throw new TunnelDeleteIncompleteError(
      `Deleted ${plural(deleted, "MCP server")}, but the tunnel could not be confirmed deleted (${reasonOf(error)}).`,
      progressed,
    );
  }
}

// Where to go after a tunnel delete stopped partway, judged from the MCP
// servers still on the tunnel. The page the user started from survives only
// if its own server does.
export type TunnelDeleteRecovery =
  | { kind: "stay" }
  | { kind: "server"; mcpServer: McpServer }
  | { kind: "tunnel" };

export function tunnelDeleteRecovery(
  remaining: readonly McpServer[],
  currentMcpServerId: string,
): TunnelDeleteRecovery {
  if (remaining.some((server) => server.id === currentMcpServerId)) {
    return { kind: "stay" };
  }
  const survivor = remaining[0];
  if (survivor) return { kind: "server", mcpServer: survivor };
  return { kind: "tunnel" };
}

// Order-insensitive identity of a set of MCP servers, to tell whether the set
// a confirmation was given for is still the one on screen.
export function serverSetKey(servers: readonly { id: string }[]): string {
  return servers
    .map((server) => server.id)
    .sort()
    .join(",");
}
