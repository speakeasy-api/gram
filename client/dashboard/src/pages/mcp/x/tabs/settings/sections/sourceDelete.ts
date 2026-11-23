import { isNotFoundError } from "@/lib/route-errors";
import type { Gram } from "@gram/client";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import type { UnproxiedMcpServer } from "@gram/client/models/components/unproxiedmcpserver.js";
import { buildMcpServersQuery } from "@gram/client/react-query/mcpServers.js";
import type { QueryClient } from "@tanstack/react-query";

// The source row behind an mcp_servers row. Hosted (toolset) and gateway
// servers have none: deleting them deletes only the wrapper.
export type SourceBackedDeleteTarget =
  | { kind: "remote"; source: RemoteMcpServer }
  | { kind: "tunneled"; source: TunneledMcpServer }
  | { kind: "unproxied"; source: UnproxiedMcpServer };

export type SourceDeleteSpec = {
  title: string;
  entityDescription: string;
  successMessage: string;
  failureMessage: string;
};

// Copy for the cascade dialog. The user confirms by typing the server's own
// name, the same as every other delete.
export function sourceDeleteSpec(
  target: SourceBackedDeleteTarget,
): SourceDeleteSpec {
  switch (target.kind) {
    case "remote":
      return {
        title: "Delete remote MCP server",
        entityDescription: "the remote MCP source",
        successMessage: "Remote MCP server deleted",
        failureMessage: "Failed to delete remote MCP server",
      };
    case "tunneled":
      return {
        title: "Delete tunneled MCP server",
        entityDescription: "the tunneled MCP source",
        successMessage: "Tunneled MCP server deleted",
        failureMessage: "Failed to delete tunneled MCP server",
      };
    case "unproxied":
      return {
        title: "Delete unproxied MCP server",
        entityDescription: "the unproxied MCP source",
        successMessage: "Unproxied MCP server deleted",
        failureMessage: "Failed to delete unproxied MCP server",
      };
  }
}

/** What a server with no name is called wherever a delete lists it. */
const UNNAMED_SERVER_LABEL = "Unnamed server";

// "A", "A and B", "A, B, and C", "A, B, and 2 more": the servers a cascade
// takes with it, short enough for a card. The dialog lists every one.
export function nameList(servers: Pick<McpServer, "name">[]): string {
  const names = servers.map((server) => server.name || UNNAMED_SERVER_LABEL);
  if (names.length <= 2) return names.join(" and ");
  if (names.length === 3) return `${names[0]}, ${names[1]}, and ${names[2]}`;
  return `${names[0]}, ${names[1]}, and ${names.length - 2} more`;
}

// What the user types to delete a source-backed server: its name, or for an
// unnamed one the source's own address or name, never a generic word.
export function sourceBackedConfirmName(
  serverName: string | undefined,
  target: SourceBackedDeleteTarget,
): string {
  const name = serverName?.trim();
  if (name) return name;
  switch (target.kind) {
    case "remote":
    case "unproxied":
      return target.source.url;
    case "tunneled":
      return target.source.name;
  }
}

type LinkedMcpServersFilter =
  | { remoteMcpServerId: string }
  | { tunneledMcpServerId: string }
  | { unproxiedMcpServerId: string };

// The mcpServers.list filter that returns every server backed by the same
// source, or null for servers with no source row.
export function linkedMcpServersFilter(
  mcpServer: Pick<
    McpServer,
    "remoteMcpServerId" | "tunneledMcpServerId" | "unproxiedMcpServerId"
  >,
): LinkedMcpServersFilter | null {
  if (mcpServer.remoteMcpServerId) {
    return { remoteMcpServerId: mcpServer.remoteMcpServerId };
  }
  if (mcpServer.tunneledMcpServerId) {
    return { tunneledMcpServerId: mcpServer.tunneledMcpServerId };
  }
  if (mcpServer.unproxiedMcpServerId) {
    return { unproxiedMcpServerId: mcpServer.unproxiedMcpServerId };
  }
  return null;
}

// mcpServers.list is filtered server-side, but applying the same predicate
// client-side guards against a stale or unfiltered cache hit invalidating the
// linkage assumption: the dialog must list exactly what the delete removes.
export function serversBackedBySameSource(
  mcpServer: Pick<
    McpServer,
    "remoteMcpServerId" | "tunneledMcpServerId" | "unproxiedMcpServerId"
  >,
  candidates: McpServer[],
): McpServer[] {
  const filter = linkedMcpServersFilter(mcpServer);
  if (!filter) return [];
  return serversMatchingFilter(filter, candidates);
}

export function serversMatchingFilter(
  filter: LinkedMcpServersFilter,
  candidates: McpServer[],
): McpServer[] {
  return candidates.filter((candidate) => {
    if ("remoteMcpServerId" in filter) {
      return candidate.remoteMcpServerId === filter.remoteMcpServerId;
    }
    if ("tunneledMcpServerId" in filter) {
      return candidate.tunneledMcpServerId === filter.tunneledMcpServerId;
    }
    return candidate.unproxiedMcpServerId === filter.unproxiedMcpServerId;
  });
}

// The servers backed by a source as of right now, bypassing the cache. The
// cascade must delete this set rather than the list the page loaded with: a
// sibling created since would otherwise stay live, pointing at a soft-deleted
// source.
export async function fetchLinkedMcpServers(
  client: Gram,
  queryClient: QueryClient,
  filter: LinkedMcpServersFilter,
): Promise<McpServer[]> {
  const { mcpServers } = await queryClient.fetchQuery({
    ...buildMcpServersQuery(client, filter),
    staleTime: 0,
  });
  return serversMatchingFilter(filter, mcpServers);
}

type LinkedDeleteFailure = { id: string; reason: unknown };

// The linked-server deletes that failed for a reason other than "already
// gone". A 404 means an earlier, partly failed run removed that wrapper,
// which is the state this run wants, so a retry never trips over it.
export function failedLinkedDeletes(
  ids: readonly string[],
  results: readonly PromiseSettledResult<unknown>[],
): LinkedDeleteFailure[] {
  return ids.flatMap((id, index) => {
    const result = results[index];
    if (!result || result.status !== "rejected") return [];
    if (isNotFoundError(result.reason)) return [];
    return [{ id, reason: result.reason }];
  });
}

function errorMessage(reason: unknown, fallback: string): string {
  return reason instanceof Error && reason.message ? reason.message : fallback;
}

type SourceCascadeDeps = {
  // Fetches the current set of servers backed by the source.
  listLinked: () => Promise<McpServer[]>;
  deleteMcpServer: (id: string) => Promise<unknown>;
  deleteSource: () => Promise<unknown>;
  // Noun for error copy, e.g. "remote MCP source".
  sourceLabel: string;
};

// Deletes every server backed by a source, then the source. There is no
// server-side cascade, so the two steps cannot be atomic; instead the run is
// safe to repeat: it works from the current sibling set, treats wrappers that
// are already gone as done, and its errors say which step stopped so the
// user knows a retry finishes the job.
export async function deleteSourceCascade({
  listLinked,
  deleteMcpServer,
  deleteSource,
  sourceLabel,
}: SourceCascadeDeps): Promise<void> {
  const ids = (await listLinked()).map((server) => server.id);
  const results = await Promise.allSettled(
    ids.map((id) => deleteMcpServer(id)),
  );
  const failed = failedLinkedDeletes(ids, results);
  if (failed.length > 0) {
    const first = failed[0];
    const reason = first
      ? errorMessage(first.reason, "request failed")
      : "request failed";
    throw new Error(
      `Deleted ${ids.length - failed.length} of ${ids.length} linked MCP servers; ${failed.length} could not be deleted (${reason}). The ${sourceLabel} was left in place. Retry to delete what remains.`,
    );
  }

  try {
    await deleteSource();
  } catch (error) {
    throw new Error(
      `Every linked MCP server was deleted, but the ${sourceLabel} was not: ${errorMessage(error, "request failed")}. Retry to finish.`,
    );
  }
}
