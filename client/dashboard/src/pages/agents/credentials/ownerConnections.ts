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
  clients: ServerSignIn[],
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

/**
 * Whether a server may ask each person to sign in upstream, read from the
 * server's own auth clients rather than from its user session issuer. An
 * issuer is often shared across the organization, so the personal sign-ins it
 * lists can belong to other servers: a server that offers only shared
 * credentials needs no person, whatever its issuer lists.
 *
 * While setup is incomplete the server has upstream credentials that no auth
 * client records yet, so the issuer's sign-ins stay the only signal.
 */
export function offersPersonalSignIn(
  auth: Pick<McpServerAuthentication, "status" | "clients">,
): boolean {
  if (auth.status === "setup_incomplete") return true;
  return auth.clients.some(
    (client) => client.enabled && client.clientType === "remote_session",
  );
}

/** An upstream sign-in a server asks for: its OAuth registration and who owns it. */
export type ServerSignIn = Pick<RemoteSessionClient, "id" | "credentialOwner">;

/**
 * The upstream sign-ins a server asks each person for, read from its own auth
 * clients. Under upstream auth enforcement these decide which registration a
 * call signs in through, so they are the answer even when the server shares
 * its user session issuer with others: the issuer binds one registration,
 * which can belong to another server. Undefined while the auth clients do
 * not record the upstream yet, when the issuer's registrations stay the only
 * signal: setup is incomplete, or a personal client names no registration.
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

/**
 * Attach the caller's own connections, preserving every valid assignment.
 * Editors may replace stale assignments with their own usable session.
 * Bulk selection reports missing servers and continues with the rest.
 *
 * Under upstream auth enforcement the sign-ins come from each server's own
 * auth clients: an issuer shared across the organization lists other
 * servers' registrations, which the caller has no reason to have connected.
 */
export async function attachOwnerConnections(
  api: ConnectionApi,
  agentId: string,
  servers: InventoryServer[],
  signal?: AbortSignal,
  skipMissing = false,
  enforced = false,
): Promise<string[]> {
  const { personal, unreadable, signIns } = await personalServers(
    api,
    servers,
    signal,
  );
  /**
   * The sign-ins the servers on one issuer ask for, from their own auth
   * clients. Undefined without enforcement, or while any of them has not
   * resolved its sign-ins (`serverSignIns`), when the issuer's registrations
   * are still the answer.
   */
  const typedSignIns = (
    issuer: InventoryServer,
  ): ServerSignIn[] | undefined => {
    if (!enforced) return undefined;
    const reads = personal
      .filter(
        (server) =>
          server.projectId === issuer.projectId &&
          server.issuerId === issuer.issuerId,
      )
      .map((server) => signIns.get(server.id));
    if (reads.some((read) => !read)) return undefined;
    return [
      ...new Map(reads.flatMap((read) => read!).map((c) => [c.id, c])).values(),
    ];
  };
  if (unreadable[0] && !skipMissing)
    throw new Error(
      `Could not check how ${unreadable[0].name} signs in. Try again.`,
    );
  const issuers = [
    ...new Map(
      personal.map((server) => [
        `${server.projectId}:${server.issuerId}`,
        server,
      ]),
    ).values(),
  ];
  const missing = new Set(unreadable.map((server) => server.name));
  for (const server of issuers) {
    const gramProject = server.projectSlug;
    const userSessionIssuerId = server.issuerId!;
    const scope = { gramProject, principalId: agentId, userSessionIssuerId };
    const typed = typedSignIns(server);
    const [clients, candidates, bindings] = await Promise.all([
      typed ??
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
