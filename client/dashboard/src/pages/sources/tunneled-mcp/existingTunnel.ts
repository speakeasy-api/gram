import { getHttpStatusCode, isNotFoundError } from "@/lib/route-errors";
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
const HTTP_CONFLICT = 409;

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
  return getHttpStatusCode(error) === HTTP_CONFLICT;
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
// user started from may already be gone.
export class TunnelDeleteIncompleteError extends Error {
  readonly progressed: boolean;

  constructor(message: string, progressed: boolean) {
    super(message);
    this.name = "TunnelDeleteIncompleteError";
    this.progressed = progressed;
  }
}

const RECOVERY_HINT =
  "To finish, open Add MCP server, choose Existing tunnel, and delete the unused tunnel there.";

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
  let uncertain = 0;
  let firstFailure: unknown;
  for (const result of results) {
    if (result.status === "fulfilled" || isNotFoundError(result.reason)) {
      deleted++;
      continue;
    }
    firstFailure ??= result.reason;
    if (!isDefiniteRejection(result.reason)) uncertain++;
  }
  const total = confirmedIds.length;
  const progressed = deleted > 0 || uncertain > 0;

  if (deleted < total) {
    throw new TunnelDeleteIncompleteError(
      `Deleted ${deleted} of ${total} MCP servers; ${total - deleted} could not be deleted (${reasonOf(firstFailure)}). The tunnel was kept.${progressed ? ` ${RECOVERY_HINT}` : ""}`,
      progressed,
    );
  }

  try {
    await deleteTunnel();
  } catch (error) {
    if (isTunnelInUseError(error)) {
      throw new TunnelDeleteIncompleteError(
        `Deleted ${deleted} of the MCP servers you confirmed, but the tunnel was kept because other MCP servers still use it. Some may not be visible to you.`,
        progressed,
      );
    }
    throw new TunnelDeleteIncompleteError(
      `Deleted ${deleted} MCP servers, but the tunnel could not be confirmed deleted (${reasonOf(error)}).${progressed ? ` ${RECOVERY_HINT}` : " Retry to finish."}`,
      progressed,
    );
  }
}
