import { invalidateAllGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { invalidateAllGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";
import { invalidateAllRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { invalidateAllTunneledMcpServers } from "@gram/client/react-query/tunneledMcpServers.js";
import { invalidateAllUserSessionIssuerCimdClients } from "@gram/client/react-query/userSessionIssuerCimdClients.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import type {
  InvalidateQueryFilters,
  QueryClient,
} from "@tanstack/react-query";
import { resetAllProtectedResourceMetadata } from "./authentication/useProtectedResourceMetadata";

// A source edit has two consumers: the per-id query the settings sections and
// sidebar read from, and the project-wide list the sources shelf reads from.
// refetchType "all" refreshes the list even while nothing observes it.
export async function invalidateTunneledMcpSourceViews(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    invalidateAllGetTunneledMcpServer(queryClient, { refetchType: "all" }),
    invalidateAllTunneledMcpServers(queryClient, { refetchType: "all" }),
  ]);
}

// Also drops the RFC 9728 probe: Authentication reads it for the upstream
// URL, which is one of the fields edited through here.
export async function invalidateRemoteMcpSourceViews(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    invalidateAllGetRemoteMcpServer(queryClient, { refetchType: "all" }),
    invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
    resetAllProtectedResourceMetadata(queryClient),
  ]);
}

// Every cache the Authentication section reads that a wrapper delete can
// change: the server drops the wrapper's unowned issuer, and with it the
// CIMD clients registered under it and the remote-session client bindings
// that pointed at it. Called from both the success and the partial-failure
// path, since a failed cascade has already deleted some wrappers.
export async function invalidateWrapperDeleteAuthViews(
  queryClient: QueryClient,
  filters: Pick<InvalidateQueryFilters, "refetchType">,
): Promise<void> {
  await Promise.all([
    invalidateAllUserSessionIssuers(queryClient, filters),
    invalidateAllUserSessionIssuerCimdClients(queryClient, filters),
    invalidateAllRemoteSessionIssuers(queryClient, filters),
    invalidateAllRemoteSessionClients(queryClient, filters),
  ]);
}
