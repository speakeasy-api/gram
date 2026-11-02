import { useQueries } from "@tanstack/react-query";
import { queryKeyMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { collectPageItems } from "@/components/sessions/collectPageItems";
import {
  offersPersonalSignIn,
  serverConnection,
  userSubject,
  type ServerConnection,
} from "./ownerConnections";
import type { InventoryServer } from "./useServerInventory";

export interface OwnerConnections {
  /** Per server id, for the servers that authorize each person separately. */
  byServer: Map<string, ServerConnection>;
  /** Actual assignments only; candidates must not appear connected in edit summaries. */
  assignedByServer?: Map<string, ServerConnection>;
  /** Re-read assignments and the caller's connections, after they sign in elsewhere. */
  refetch: () => Promise<void>;
}

/**
 * The signed-in person's own connections to the servers that authorize each
 * person separately, read for a new agent they are about to own.
 *
 * The agent does not exist yet, so this reads the person's sessions in each
 * server's project rather than the agent's attachment candidates: one listing
 * of their sessions per project, and the upstream sign-ins each issuer asks
 * for. Provisioning checks the agent's real candidates again before attaching.
 *
 * A server with an issuer still needs nobody when its own auth clients offer
 * no personal sign-in: the issuer may be shared with servers that do.
 */
export function useOwnerConnections(
  servers: InventoryServer[],
  enabled: boolean,
  agentId?: string,
): OwnerConnections {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const { user } = useSession();
  const personal = servers.filter((server) => server.issuerId);
  const projects = [...new Set(personal.map((server) => server.projectSlug))];
  const issuers = [
    ...new Map(
      personal.map((server) => [
        `${server.projectSlug}:${server.issuerId}`,
        {
          gramProject: server.projectSlug,
          userSessionIssuerId: server.issuerId!,
        },
      ]),
    ).values(),
  ];
  const ownerScope = { organizationId: organization.id, userId: user.id };
  // Shares its cache with the server's info popover, which reads the same.
  const auth = useQueries({
    queries: personal.map((server) => ({
      queryKey: queryKeyMcpServerAuthentication({ mcpServerId: server.id }),
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        sdk.authClients.getMcpServerAuthentication(
          { mcpServerId: server.id },
          undefined,
          { signal },
        ),
      enabled,
      retry: false,
      throwOnError: false,
    })),
  });
  /** Whether a server's own auth clients settle that it needs no person. */
  const shared = (server: InventoryServer): boolean => {
    const read = auth[personal.indexOf(server)];
    return !!read?.data && !offersPersonalSignIn(read.data);
  };
  const sessions = useQueries({
    queries: projects.map((gramProject) => ({
      queryKey: ["owner-connections", "sessions", ownerScope, gramProject],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        collectPageItems(
          sdk.remoteSessions.list(
            { gramProject, subjectUrn: userSubject(user.id) },
            undefined,
            { signal },
          ),
        ),
      enabled,
      retry: false,
      throwOnError: false,
    })),
  });
  const clients = useQueries({
    queries: issuers.map((request) => ({
      queryKey: ["owner-connections", "clients", ownerScope, request],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        collectPageItems(
          sdk.remoteSessionClients.list(request, undefined, { signal }),
        ),
      enabled,
      retry: false,
      throwOnError: false,
    })),
  });

  const bindings = useQueries({
    queries: issuers.map((request) => ({
      queryKey: ["owner-connections", "bindings", ownerScope, agentId, request],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        sdk.remoteSessions.listBindings(
          { ...request, principalId: agentId! },
          undefined,
          { signal },
        ),
      enabled: enabled && !!agentId,
      retry: false,
      throwOnError: false,
    })),
  });
  const byServer = new Map<string, ServerConnection>();
  for (const server of personal) {
    if (shared(server)) {
      byServer.set(server.id, { status: "not-required" });
      continue;
    }
    const authRead = auth[personal.indexOf(server)];
    const sessionRead = sessions[projects.indexOf(server.projectSlug)];
    const clientRead =
      clients[
        issuers.findIndex(
          (issuer) =>
            issuer.gramProject === server.projectSlug &&
            issuer.userSessionIssuerId === server.issuerId,
        )
      ];
    const bindingRead =
      bindings[
        issuers.findIndex(
          (issuer) =>
            issuer.gramProject === server.projectSlug &&
            issuer.userSessionIssuerId === server.issuerId,
        )
      ];
    if (
      authRead?.isError ||
      sessionRead?.isError ||
      clientRead?.isError ||
      (agentId && bindingRead?.isError)
    ) {
      byServer.set(server.id, { status: "error" });
      continue;
    }
    if (
      !authRead?.data ||
      !sessionRead?.data ||
      !clientRead?.data ||
      (agentId && !bindingRead?.data)
    ) {
      byServer.set(server.id, { status: "checking" });
      continue;
    }
    byServer.set(
      server.id,
      serverConnection(server.issuerId!, clientRead.data, [
        ...(bindingRead?.data?.items ?? []).flatMap((binding) =>
          binding.remoteSession
            ? [
                {
                  ...binding.remoteSession,
                  userSessionIssuerId: server.issuerId!,
                },
              ]
            : [],
        ),
        ...sessionRead.data,
      ]),
    );
  }
  const assignedByServer = agentId ? new Map(byServer) : undefined;
  if (assignedByServer) {
    for (const server of personal) {
      // Shared needs no assignment, and until the server's auth is read its
      // checking or error state stands.
      if (shared(server) || !auth[personal.indexOf(server)]?.data) continue;
      const index = issuers.findIndex(
        (issuer) =>
          issuer.gramProject === server.projectSlug &&
          issuer.userSessionIssuerId === server.issuerId,
      );
      const clientsRead = clients[index];
      const bindingRead = bindings[index];
      if (
        !clientsRead?.data ||
        !bindingRead?.data ||
        clientsRead.isError ||
        bindingRead.isError
      )
        continue;
      if (bindingRead.data.items.some((binding) => !binding.remoteSession)) {
        assignedByServer.set(server.id, { status: "missing" });
        continue;
      }
      assignedByServer.set(
        server.id,
        serverConnection(
          server.issuerId!,
          clientsRead.data,
          bindingRead.data.items.flatMap((binding) =>
            binding.remoteSession
              ? [
                  {
                    ...binding.remoteSession,
                    userSessionIssuerId: server.issuerId!,
                  },
                ]
              : [],
          ),
        ),
      );
    }
  }
  return {
    byServer,
    assignedByServer,
    refetch: async () => {
      await Promise.all(
        [
          ...sessions,
          ...clients,
          ...(agentId ? bindings : []),
          // A server's auth does not change when someone signs in, but a
          // failed read gets another try.
          ...auth.filter((read) => read.isError),
        ].map((read) => read.refetch()),
      );
    },
  };
}
