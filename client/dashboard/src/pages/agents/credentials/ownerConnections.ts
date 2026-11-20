import type { McpServerAuthentication } from "@gram/client/models/components/mcpserverauthentication.js";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { AuthClients } from "@gram/client/sdk/authclients.js";
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
  | { status: "setup-required" }
  /** The server asks for no personal upstream sign-in, so a key alone works. */
  | { status: "not-required" }
  /**
   * At least one upstream sign-in has no usable assigned or personal
   * connection. `signIns` lists each one, and `connected` counts the ones that
   * do have a connection, so the reader is told what is left to connect
   * rather than only that something is. `connectedNames` names the connected
   * ones, so a sentence never guesses what they are. All are absent where the
   * cause is not a sign-in, such as an assignment whose session is gone.
   */
  | {
      status: "missing";
      signIns?: ServerSignIn[];
      connected?: number;
      connectedNames?: (string | undefined)[];
    }
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
 * A session counts when its client is one the server asks for, whichever
 * issuer the person signed in through: a connection made through a team
 * gateway serves each member server whose own issuer binds the same client,
 * and the attachment endpoint accepts it. A client whose credential is its
 * own (`credential_owner: self`) is shared by every caller and needs no
 * person.
 *
 * With `anyOne`, the server is ready once any of its personal sign-ins has a
 * working connection: under upstream auth enforcement the gateway lets a
 * caller choose among a server's own sign-ins, so one serves. Without it,
 * every sign-in is required, as the runtime requires each of a server's own
 * registrations when upstream auth is not enforced.
 */
export function serverConnection(
  clients: ServerSignIn[],
  sessions: RemoteSession[],
  now: number = Date.now(),
  { anyOne = false }: { anyOne?: boolean } = {},
): ServerConnection {
  const personal = clients.filter(
    (client) => client.credentialOwner !== "self",
  );
  if (personal.length === 0) return { status: "not-required" };
  const found: RemoteSession[] = [];
  const missing: ServerSignIn[] = [];
  // The sign-ins whose only connection has ended. They still need a person,
  // so a server that also misses another sign-in lists them as outstanding
  // rather than counting them as connected.
  const ended: ServerSignIn[] = [];
  const connectedClients: ServerSignIn[] = [];
  for (const client of personal) {
    const matching = sessions.filter(
      (candidate) => candidate.remoteSessionClientId === client.id,
    );
    // A working connection wins over one that has ended, whatever the order.
    const session =
      matching.find(
        (candidate) => connectionLifetime(candidate, now).kind !== "ended",
      ) ?? matching[0];
    if (!session) {
      missing.push(client);
      continue;
    }
    found.push(session);
    if (connectionLifetime(session, now).kind === "ended") {
      ended.push(client);
      continue;
    }
    connectedClients.push(client);
  }
  if (anyOne) {
    const working = found.filter(
      (session) => connectionLifetime(session, now).kind !== "ended",
    );
    if (working.length > 0) return { status: "ready", sessions: working };
    if (found.length > 0) return { status: "ended", sessions: found };
    return { status: "missing", signIns: missing, connected: 0 };
  }
  if (missing.length > 0)
    return {
      status: "missing",
      signIns: [...missing, ...ended],
      connected: connectedClients.length,
      ...(connectedClients.length > 0 && {
        connectedNames: connectedClients.map((client) => client.name),
      }),
    };
  if (
    found.some((session) => connectionLifetime(session, now).kind === "ended")
  )
    return { status: "ended", sessions: found };
  return { status: "ready", sessions: found };
}

/**
 * Whether a server may ask each person to sign in upstream, read from the
 * server's own auth clients rather than from its user session issuer. An
 * issuer is often shared across the organization, so the personal sign-ins it
 * lists can belong to other servers: a server that offers only shared
 * credentials needs no person, whatever its issuer lists.
 *
 * Incomplete setup still needs attention; it cannot be treated as shared.
 */
export function offersPersonalSignIn(
  auth: Pick<McpServerAuthentication, "status" | "clients">,
): boolean {
  if (auth.status === "setup_incomplete") return true;
  return auth.clients.some(
    (client) => client.enabled && client.clientType === "remote_session",
  );
}

/**
 * An upstream sign-in a server asks for: its OAuth registration, who owns it,
 * and, when known, the name a person recognizes it by, such as "Stripe".
 */
export type ServerSignIn = Pick<
  RemoteSessionClient,
  "id" | "credentialOwner"
> & {
  name?: string;
};

/**
 * The names of the sign-ins a missing connection still needs, in the order the
 * server asks for them, for a sentence such as "Still needs Stripe and Vercel".
 * Two sign-ins that share a name read as "2 Linear sign-ins", and a sign-in
 * with no known name counts as one more, so the total stays true.
 */
export function missingSignInNames(connection: ServerConnection | undefined): {
  names: string[];
  unnamed: number;
} {
  if (connection?.status !== "missing" || !connection.signIns)
    return { names: [], unnamed: 0 };
  const counts = new Map<string, number>();
  for (const signIn of connection.signIns) {
    if (signIn.name)
      counts.set(signIn.name, (counts.get(signIn.name) ?? 0) + 1);
  }
  const names = [...counts].map(([name, count]) =>
    count === 1 ? name : `${count} ${name} sign-ins`,
  );
  const unnamed = connection.signIns.filter((signIn) => !signIn.name).length;
  return { names, unnamed };
}

/**
 * Personal registrations from a server's typed offers. An undefined result
 * means setup needs repair, not that the issuer can supply a substitute.
 */
export function serverSignIns(
  auth: Pick<McpServerAuthentication, "status" | "clients">,
): ServerSignIn[] | undefined {
  if (auth.status === "setup_incomplete") return undefined;
  const personal = auth.clients.filter(
    (client) => client.enabled && client.personal,
  );
  if (personal.some((client) => !client.remoteSessionClientId))
    return undefined;
  return personal.map((client) => ({
    id: client.remoteSessionClientId!,
    credentialOwner: "subject",
    name: client.name,
  }));
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
  authClients: Pick<AuthClients, "getMcpServerAuthentication">;
  remoteSessions: Pick<
    RemoteSessions,
    "list" | "listBindings" | "attachBinding" | "detachBinding"
  >;
  remoteSessionClients: Pick<RemoteSessionClients, "list">;
}

/**
 * Sorts servers with an issuer by whether they may ask each person to sign
 * in. One whose auth cannot be read gets nothing attached, as the wizard
 * keeps its row locked: an unrelated sign-in is never attached on a guess.
 */
async function personalServers(
  api: ConnectionApi,
  servers: InventoryServer[],
  signal?: AbortSignal,
): Promise<{
  personal: InventoryServer[];
  unreadable: InventoryServer[];
  /** The upstream sign-ins each personal server's own auth clients ask for. */
  signIns: Map<string, ServerSignIn[] | undefined>;
}> {
  const signIns = new Map<string, ServerSignIn[] | undefined>();
  const reads = await Promise.all(
    servers.map(async (server) => {
      if (!server.issuerId) return "shared" as const;
      try {
        const auth = await api.authClients.getMcpServerAuthentication(
          { mcpServerId: server.id },
          undefined,
          { signal },
        );
        if (!offersPersonalSignIn(auth)) return "shared" as const;
        signIns.set(server.id, serverSignIns(auth));
        return "personal" as const;
      } catch (failure) {
        if (signal?.aborted) throw failure;
        return "unreadable" as const;
      }
    }),
  );
  return {
    personal: servers.filter((_, index) => reads[index] === "personal"),
    unreadable: servers.filter((_, index) => reads[index] === "unreadable"),
    signIns,
  };
}

export function usesTypedOwnerConnections(server: InventoryServer): boolean {
  return server.kind === "Remote" || server.kind === "Tunneled";
}

/**
 * Attach the caller's own connections, preserving every valid assignment.
 * Editors may replace stale assignments with their own usable session.
 * Bulk selection reports missing servers and continues with the rest.
 *
 * A proxied server uses its own typed offers. Hosted servers still use
 * their downstream issuer registrations.
 */
export async function attachOwnerConnections(
  api: ConnectionApi,
  agentId: string,
  servers: InventoryServer[],
  signal?: AbortSignal,
  skipMissing = false,
): Promise<string[]> {
  const { personal, unreadable, signIns } = await personalServers(
    api,
    servers,
    signal,
  );
  if (unreadable[0] && !skipMissing)
    throw new Error(
      `Could not check how ${unreadable[0].name} signs in. Try again.`,
    );
  const incomplete = personal.filter(
    (server) => usesTypedOwnerConnections(server) && !signIns.get(server.id),
  );
  if (incomplete[0] && !skipMissing) {
    throw new Error(
      `Finish upstream credential setup for ${incomplete[0].name} in Identity, then retry.`,
    );
  }
  const groups = new Map<string, InventoryServer[]>();
  for (const server of personal) {
    if (incomplete.includes(server)) continue;
    const key = `${server.projectId}:${server.issuerId}`;
    const group = groups.get(key);
    if (group) group.push(server);
    else groups.set(key, [server]);
  }
  const missing = new Set(
    [...unreadable, ...incomplete].map((server) => server.name),
  );
  for (const [server, ...others] of groups.values()) {
    if (!server) continue;
    const group = [server, ...others];
    const gramProject = server.projectSlug;
    const userSessionIssuerId = server.issuerId!;
    const scope = { gramProject, principalId: agentId, userSessionIssuerId };
    const [hostedClients, candidates, bindings] = await Promise.all([
      group.some((item) => !usesTypedOwnerConnections(item))
        ? collectPageItems(
            api.remoteSessionClients.list(
              { gramProject, userSessionIssuerId },
              undefined,
              { signal },
            ),
          )
        : [],
      // Let the attachment API enforce live session and provenance eligibility.
      // The issuer passed in scope is the attachment target, not the issuer
      // the caller used to establish the session (for example, a gateway).
      collectPageItems(api.remoteSessions.list(scope, undefined, { signal })),
      api.remoteSessions.listBindings(scope, undefined, { signal }),
    ]);
    const ownSignIns = (item: InventoryServer) =>
      (usesTypedOwnerConnections(item)
        ? (signIns.get(item.id) ?? [])
        : hostedClients
      ).filter((client) => client.credentialOwner !== "self");
    const clients = new Map(
      group.flatMap(ownSignIns).map((client) => [client.id, client]),
    );
    const available = (id: string) =>
      bindings.items.some(
        (binding) =>
          binding.remoteSessionClientId === id &&
          binding.remoteSession &&
          connectionLifetime(binding.remoteSession).kind !== "ended",
      ) ||
      candidates.some(
        (candidate) =>
          candidate.remoteSessionClientId === id &&
          connectionLifetime(candidate).kind !== "ended",
      );
    if (!skipMissing) {
      const unavailable = group.find((item) => {
        const own = ownSignIns(item);
        return usesTypedOwnerConnections(item) && own.length > 0
          ? !own.some((client) => available(client.id))
          : !own.every((client) => available(client.id));
      });
      if (unavailable)
        throw new Error(
          `Your connection to ${unavailable.name} is no longer available. Sign in to it again, then retry.`,
        );
    }
    const served = new Set<string>();
    for (const client of clients.values()) {
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
      ) {
        served.add(client.id);
        continue;
      }
      const session = candidates.find(
        (candidate) =>
          candidate.remoteSessionClientId === client.id &&
          connectionLifetime(candidate).kind !== "ended",
      );
      if (!session) continue;
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
      served.add(client.id);
    }
    for (const sharing of group) {
      const own = ownSignIns(sharing);
      const ready =
        usesTypedOwnerConnections(sharing) && own.length > 0
          ? own.some((client) => served.has(client.id))
          : own.every((client) => served.has(client.id));
      if (ready) continue;
      if (!skipMissing)
        throw new Error(
          `Your connection to ${sharing.name} is no longer available. Sign in to it again, then retry.`,
        );
      missing.add(sharing.name);
    }
  }
  return [...missing];
}
