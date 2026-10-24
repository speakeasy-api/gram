import { useQueries } from "@tanstack/react-query";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { collectPageItems } from "@/components/sessions/collectPageItems";
import {
  serverConnection,
  userSubject,
  type ServerConnection,
} from "./ownerConnections";
import type { InventoryServer } from "./useServerInventory";

export interface OwnerConnections {
  /** Per server id, for the servers that authorize each person separately. */
  byServer: Map<string, ServerConnection>;
  /** Re-read the owner's connections, after they sign in elsewhere. */
  refetch: () => void;
}

/**
 * The signed-in person's own connections to the servers that authorize each
 * person separately, read for a new agent they are about to own.
 *
 * The agent does not exist yet, so this reads the person's sessions in each
 * server's project rather than the agent's attachment candidates: one listing
 * of their sessions per project, and the upstream sign-ins each issuer asks
 * for. Provisioning checks the agent's real candidates again before attaching.
 */
export function useOwnerConnections(
  servers: InventoryServer[],
  enabled: boolean,
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

  const byServer = new Map<string, ServerConnection>();
  for (const server of personal) {
    const sessionRead = sessions[projects.indexOf(server.projectSlug)];
    const clientRead =
      clients[
        issuers.findIndex(
          (issuer) =>
            issuer.gramProject === server.projectSlug &&
            issuer.userSessionIssuerId === server.issuerId,
        )
      ];
    if (sessionRead?.isError || clientRead?.isError) {
      byServer.set(server.id, { status: "error" });
      continue;
    }
    if (!sessionRead?.data || !clientRead?.data) {
      byServer.set(server.id, { status: "checking" });
      continue;
    }
    byServer.set(
      server.id,
      serverConnection(server.issuerId!, clientRead.data, sessionRead.data),
    );
  }
  return {
    byServer,
    refetch: () => {
      for (const read of [...sessions, ...clients]) void read.refetch();
    },
  };
}
