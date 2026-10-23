import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessions } from "@gram/client/sdk/remotesessions.js";
import type { RemoteSessionClients } from "@gram/client/sdk/remotesessionclients.js";
import { collectPageItems } from "@/components/sessions/collectPageItems";
import { connectionLifetime } from "@/components/sessions/connection-lifetime";
import type { InventoryServer } from "./useServerInventory";

/**
 * How an agent would reach one server that authorizes each person
 * separately, using its owner's own connection to it.
 *
 * Only the owner's connections are ever used: the runtime serves an agent
 * the connections its owner attached and nobody else's, so an agent never
 * acts as a mix of people. A server is ready when the owner holds a working
 * connection through every upstream sign-in it asks for.
 */
export type ServerConnection =
  | { status: "checking" }
  | { status: "error" }
  /** The server asks for no personal upstream sign-in, so a key alone works. */
  | { status: "not-required" }
  /** At least one upstream sign-in has no connection of the owner's. */
  | { status: "missing" }
  /** Every sign-in is connected, but one of them no longer works. */
  | { status: "ended"; sessions: RemoteSession[] }
  | { status: "ready"; sessions: RemoteSession[] };

/** The subject a person's own remote sessions are recorded under. */
export function userSubject(userId: string): string {
  return `user:${userId}`;
}

/**
 * The owner's connection to a server, decided from the upstream sign-ins its
 * issuer asks for and the owner's own sessions in the server's project.
 *
 * A session counts only when it was made through this server's issuer, which
 * is what the attachment endpoint will accept. A client whose credential is
 * its own (`credential_owner: self`) is shared by every caller and needs no
 * person.
 */
export function serverConnection(
  issuerId: string,
  clients: RemoteSessionClient[],
  sessions: RemoteSession[],
  now: number = Date.now(),
): ServerConnection {
  const personal = clients.filter(
    (client) => client.credentialOwner !== "self",
  );
  if (personal.length === 0) return { status: "not-required" };
  const found: RemoteSession[] = [];
  for (const client of personal) {
    const session = sessions.find(
      (candidate) =>
        candidate.remoteSessionClientId === client.id &&
        candidate.userSessionIssuerId === issuerId,
    );
    if (!session) return { status: "missing" };
    found.push(session);
  }
  if (
    found.some((session) => connectionLifetime(session, now).kind === "ended")
  )
    return { status: "ended", sessions: found };
  return { status: "ready", sessions: found };
}

/** Whether a row can be chosen: the key will reach it without anyone signing in. */
export function connectionSelectable(
  connection: ServerConnection | undefined,
): boolean {
  return (
    connection?.status === "ready" || connection?.status === "not-required"
  );
}

export interface ConnectionApi {
  remoteSessions: Pick<
    RemoteSessions,
    "list" | "listBindings" | "attachBinding" | "detachBinding"
  >;
  remoteSessionClients: Pick<RemoteSessionClients, "list">;
}

/**
 * Let a freshly provisioned agent use its owner's connections to the chosen
 * servers that authorize each person separately: one attachment per upstream
 * sign-in, made by the owner and naming the owner's own session.
 *
 * Reads the authoritative candidates for the agent rather than trusting the
 * listing the picker showed, so a connection that changed in between is
 * reported instead of attached. An attachment already serving the agent is
 * kept; one whose connection was replaced, revoked or has ended is the
 * owner's own, so it is detached and the current connection attached in its
 * place. Any failure
 * throws, naming the server, and nothing after it is attempted.
 */
export async function attachOwnerConnections(
  api: ConnectionApi,
  agentId: string,
  servers: InventoryServer[],
  signal?: AbortSignal,
): Promise<void> {
  const issuers = [
    ...new Map(
      servers
        .filter((server) => server.issuerId)
        .map((server) => [`${server.projectId}:${server.issuerId}`, server]),
    ).values(),
  ];
  for (const server of issuers) {
    const gramProject = server.projectSlug;
    const userSessionIssuerId = server.issuerId!;
    const scope = { gramProject, principalId: agentId, userSessionIssuerId };
    const [clients, candidates, bindings] = await Promise.all([
      collectPageItems(
        api.remoteSessionClients.list(
          { gramProject, userSessionIssuerId },
          undefined,
          { signal },
        ),
      ),
      collectPageItems(api.remoteSessions.list(scope, undefined, { signal })),
      api.remoteSessions.listBindings(scope, undefined, { signal }),
    ]);
    for (const client of clients) {
      if (client.credentialOwner === "self") continue;
      const current = bindings.items.filter(
        (binding) => binding.remoteSessionClientId === client.id,
      );
      // An attachment whose connection has ended serves no call, so it is
      // replaced like one whose connection was revoked.
      if (
        current.some(
          (binding) =>
            binding.remoteSession !== undefined &&
            connectionLifetime(binding.remoteSession).kind !== "ended",
        )
      )
        continue;
      const session = candidates.find(
        (candidate) =>
          candidate.remoteSessionClientId === client.id &&
          connectionLifetime(candidate).kind !== "ended",
      );
      if (!session)
        throw new Error(
          `Your connection to ${server.name} is no longer available. Sign in to it again, then retry.`,
        );
      for (const stale of current)
        await api.remoteSessions.detachBinding(
          {
            gramProject,
            detachBindingRequestBody: {
              principalId: agentId,
              userSessionIssuerId,
              id: stale.id,
            },
          },
          undefined,
          { signal },
        );
      await api.remoteSessions.attachBinding(
        {
          gramProject,
          attachBindingRequestBody: {
            principalId: agentId,
            userSessionIssuerId,
            remoteSessionId: session.id,
          },
        },
        undefined,
        { signal },
      );
    }
  }
}
