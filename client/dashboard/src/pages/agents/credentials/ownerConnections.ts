import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessions } from "@gram/client/sdk/remotesessions.js";
import type { RemoteSessionClients } from "@gram/client/sdk/remotesessionclients.js";
import { collectPageItems } from "@/components/sessions/collectPageItems";
import { connectionLifetime } from "@/components/sessions/connection-lifetime";
import type { InventoryServer } from "./useServerInventory";

/**
 * How an agent would reach one server that authorizes each person
 * separately, using an explicitly assigned personal connection.
 *
 * Creation attaches the signed-in creator's connections. Authorized editors
 * can explicitly replace these assignments with their own connections later.
 */
export type ServerConnection =
  | { status: "checking" }
  | { status: "error" }
  /** The server asks for no personal upstream sign-in, so a key alone works. */
  | { status: "not-required" }
  /** At least one upstream sign-in has no usable assigned or personal connection. */
  | { status: "missing" }
  /** Every sign-in is connected, but one of them no longer works. */
  | { status: "ended"; sessions: RemoteSession[] }
  | { status: "ready"; sessions: RemoteSession[] };

/** The subject a person's own remote sessions are recorded under. */
export function userSubject(userId: string): string {
  return `user:${userId}`;
}

/**
 * A server's readiness, using existing assignments and the caller's own
 * sessions for the upstream sign-ins its issuer requests.
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
    const matching = sessions.filter(
      (candidate) =>
        candidate.remoteSessionClientId === client.id &&
        candidate.userSessionIssuerId === issuerId,
    );
    // A working connection wins over one that has ended, whatever the order.
    const session =
      matching.find(
        (candidate) => connectionLifetime(candidate, now).kind !== "ended",
      ) ?? matching[0];
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
 * Attach the caller's own connections, preserving every valid assignment.
 * Editors may replace stale assignments with their own usable session.
 * Bulk selection reports missing servers and continues with the rest.
 */
export async function attachOwnerConnections(
  api: ConnectionApi,
  agentId: string,
  servers: InventoryServer[],
  signal?: AbortSignal,
  skipMissing = false,
): Promise<string[]> {
  const issuers = [
    ...new Map(
      servers
        .filter((server) => server.issuerId)
        .map((server) => [`${server.projectId}:${server.issuerId}`, server]),
    ).values(),
  ];
  const missing = new Set<string>();
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
      if (!session && skipMissing) {
        missing.add(server.name);
        continue;
      }
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
  return [...missing];
}
