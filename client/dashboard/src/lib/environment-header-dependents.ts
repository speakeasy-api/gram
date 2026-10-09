import { mutationKeyCreateRemoteMcpServerHeader } from "@gram/client/react-query/createRemoteMcpServerHeader.js";
import { mutationKeyCreateTunneledMcpServerHeader } from "@gram/client/react-query/createTunneledMcpServerHeader.js";
import { mutationKeyDeleteEnvironment } from "@gram/client/react-query/deleteEnvironment.js";
import { mutationKeyDeleteRemoteMcpServerHeader } from "@gram/client/react-query/deleteRemoteMcpServerHeader.js";
import { mutationKeyDeleteTunneledMcpServerHeader } from "@gram/client/react-query/deleteTunneledMcpServerHeader.js";
import { invalidateAllGetMcpServerEnvironmentHeaders } from "@gram/client/react-query/getMcpServerEnvironmentHeaders.js";
import { mutationKeyUpdateEnvironment } from "@gram/client/react-query/updateEnvironment.js";
import { mutationKeyUpdateMcpServer } from "@gram/client/react-query/updateMcpServer.js";
import { mutationKeyUpdateRemoteMcpServerHeader } from "@gram/client/react-query/updateRemoteMcpServerHeader.js";
import { mutationKeyUpdateTunneledMcpServerHeader } from "@gram/client/react-query/updateTunneledMcpServerHeader.js";
import type { MutationKey, QueryClient } from "@tanstack/react-query";

// Writes that change what a remote or tunneled MCP server sends upstream: its
// environment link, the entries of an environment it may be linked to, or its
// source's headers. The upstream's tool listing and the environment header
// preview both depend on them, and neither query's key changes when they do.
const DEPENDENCY_MUTATION_KEYS: readonly MutationKey[] = [
  mutationKeyUpdateEnvironment(),
  mutationKeyDeleteEnvironment(),
  mutationKeyUpdateMcpServer(),
  mutationKeyCreateRemoteMcpServerHeader(),
  mutationKeyUpdateRemoteMcpServerHeader(),
  mutationKeyDeleteRemoteMcpServerHeader(),
  mutationKeyCreateTunneledMcpServerHeader(),
  mutationKeyUpdateTunneledMcpServerHeader(),
  mutationKeyDeleteTunneledMcpServerHeader(),
];

const keysEqual = (a: MutationKey, b: MutationKey): boolean =>
  a.length === b.length && a.every((part, i) => part === b[i]);

/**
 * Reports whether a successful mutation can change the headers a proxied MCP
 * server sends upstream.
 */
export function changesUpstreamHeaders(
  mutationKey: MutationKey | undefined,
): boolean {
  if (!mutationKey) return false;
  return DEPENDENCY_MUTATION_KEYS.some((key) => keysEqual(key, mutationKey));
}

/**
 * Marks every cached upstream tool listing and environment header preview
 * stale, so the next view lists and previews against the stored
 * configuration rather than the one cached before the write.
 */
export async function invalidateUpstreamHeaderDependents(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["proxiedMcpTools"] }),
    invalidateAllGetMcpServerEnvironmentHeaders(queryClient),
  ]);
}
