import { useQueries } from "@tanstack/react-query";
import { queryKeyMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { collectPageItems } from "@/components/sessions/collectPageItems";
import { useUpstreamAuthEnforcement } from "@/hooks/useUpstreamAuthEnforcement";
import {
  offersPersonalSignIn,
  serverConnection,
  serverSignIns,
  userSubject,
  type ServerConnection,
  type ServerSignIn,
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
 * no personal sign-in: the issuer may be shared with servers that do. Under
 * upstream auth enforcement the sign-ins it asks for come from those auth
 * clients too, since a shared issuer binds another server's registration;
 * otherwise from the issuer, which is what routes the call.
 */
export function useOwnerConnections(
  servers: InventoryServer[],
  enabled: boolean,
  agentId?: string,
): OwnerConnections {
  const sdk = useSdkClient();
  const enforcement = useUpstreamAuthEnforcement();
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
  /**
   * Whether a server's own auth clients settle that it needs no person. A
   * failed re-read does not count, even with an earlier answer cached.
   */
  const shared = (server: InventoryServer): boolean => {
    const read = auth[personal.indexOf(server)];
    return !!read?.data && !read.isError && !offersPersonalSignIn(read.data);
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

  // Which of its issuer's registrations each server needs, by the rule the
  // runtime enforces: its own, not those other servers on a shared issuer
  // bring. Read for every server, as the issuer's list is the fallback.
  const required = useQueries({
    queries: personal.map((server, index) => ({
      queryKey: ["owner-connections", "required", ownerScope, server.id],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        sdk.remoteSessions.listServerClientAssociations(
          { gramProject: server.projectSlug, mcpServerId: server.id },
          undefined,
          { signal },
        ),
      // Only for a server whose own auth asks each person to sign in: a
      // shared one never reads it.
      enabled: (() => {
        const data = auth[index]?.data;
        return enabled && !!data && offersPersonalSignIn(data);
      })(),
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
  const issuerIndex = (server: InventoryServer) =>
    issuers.findIndex(
      (issuer) =>
        issuer.gramProject === server.projectSlug &&
        issuer.userSessionIssuerId === server.issuerId,
    );
  /**
   * What people call each upstream registration, read from the auth clients
   * of every listed server. An issuer shared across the organization lists
   * registrations that belong to other servers, and those servers' own auth
   * clients are where the name a person recognizes, such as "Stripe", lives.
   * One registration can sit under several auth clients; when they disagree
   * on its name none of them wins, and the registration's own display name is
   * used instead, whatever order the reads land in.
   */
  const signInNames = new Map<string, string | null>();
  for (const read of auth) {
    for (const client of read.data?.clients ?? []) {
      if (!client.remoteSessionClientId || !client.name) continue;
      const known = signInNames.get(client.remoteSessionClientId);
      signInNames.set(
        client.remoteSessionClientId,
        known === undefined || known === client.name ? client.name : null,
      );
    }
  }
  /**
   * The upstream sign-ins a server asks for, once its auth is read: from its
   * own auth clients when enforced, else from its issuer's registrations.
   */
  const signIns = (
    server: InventoryServer,
  ): { data?: ServerSignIn[]; isError: boolean } => {
    const authRead = auth[personal.indexOf(server)];
    if (enforcement === "loading") return { isError: false };
    const typed =
      enforcement === "enforced" && authRead?.data
        ? serverSignIns(authRead.data)
        : undefined;
    if (typed) return { data: typed, isError: false };
    const read = clients[issuerIndex(server)];
    const requiredRead = required[personal.indexOf(server)];
    const requiredIds = requiredRead?.data
      ? new Set(requiredRead.data.requiredClientIds)
      : undefined;
    return {
      data: requiredIds
        ? read?.data
            ?.filter((client) => requiredIds.has(client.id))
            .map((client) => ({
              id: client.id,
              credentialOwner: client.credentialOwner,
              name: signInNames.get(client.id) ?? client.displayName,
            }))
        : undefined,
      isError: !!read?.isError || !!requiredRead?.isError,
    };
  };
  const byServer = new Map<string, ServerConnection>();
  for (const server of personal) {
    if (shared(server)) {
      byServer.set(server.id, { status: "not-required" });
      continue;
    }
    const authRead = auth[personal.indexOf(server)];
    const sessionRead = sessions[projects.indexOf(server.projectSlug)];
    const clientRead = signIns(server);
    const bindingRead = bindings[issuerIndex(server)];
    if (
      authRead?.isError ||
      sessionRead?.isError ||
      clientRead.isError ||
      (agentId && bindingRead?.isError)
    ) {
      byServer.set(server.id, { status: "error" });
      continue;
    }
    if (
      !authRead?.data ||
      !sessionRead?.data ||
      !clientRead.data ||
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
      const authRead = auth[personal.indexOf(server)];
      if (shared(server) || !authRead?.data || authRead.isError) continue;
      const clientsRead = signIns(server);
      const bindingRead = bindings[issuerIndex(server)];
      if (
        !clientsRead.data ||
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
          // Which sign-ins a server requires does not change on sign-in, but
          // a failed read gets another try.
          ...required.filter((read) => read.isError),
          ...(agentId ? bindings : []),
          // A server's auth does not change when someone signs in, but a
          // failed read gets another try.
          ...auth.filter((read) => read.isError),
        ].map((read) => read.refetch()),
      );
    },
  };
}
