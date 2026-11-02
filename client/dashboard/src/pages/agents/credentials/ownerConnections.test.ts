import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import type { McpServerAuthentication } from "@gram/client/models/components/mcpserverauthentication.js";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import {
  attachOwnerConnections,
  connectionSelectable,
  offersPersonalSignIn,
  serverConnection,
  type ConnectionApi,
} from "./ownerConnections";
import type { InventoryServer } from "./useServerInventory";

const now = Date.parse("2026-10-23T12:00:00Z");
const later = new Date(now + 86_400_000 * 7);
const earlier = new Date(now - 60_000);

// Expiry checks read the clock, so pin it to the fixtures' day; otherwise
// `later` falls into the past a week after these dates were written.
beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(now);
});
afterEach(() => {
  vi.useRealTimers();
});

function client(id: string, credentialOwner = "subject") {
  return { id, credentialOwner } as RemoteSessionClient;
}

function session(overrides: Partial<RemoteSession>): RemoteSession {
  return {
    id: "session_mine",
    remoteSessionClientId: "client_one",
    userSessionIssuerId: "issuer_one",
    subjectUrn: "user:user_owner",
    accessExpiresAt: later,
    hasRefreshToken: false,
    scopes: [],
    createdAt: earlier,
    updatedAt: earlier,
    ...overrides,
  } as RemoteSession;
}

const server: InventoryServer = {
  id: "server_linear",
  resourceId: "server_linear",
  name: "Linear",
  slug: "linear",
  projectId: "project_one",
  projectName: "Project one",
  projectSlug: "project-one",
  issuerId: "issuer_one",
  kind: "Remote",
};

function authClient(clientType: string, enabled = true): AuthClient {
  return { id: `auth_${clientType}`, clientType, enabled } as AuthClient;
}

function authentication(
  clients: AuthClient[],
  status = clients.some((c) => c.enabled) ? "configured" : "no_auth_required",
): McpServerAuthentication {
  return { status, clients } as McpServerAuthentication;
}

describe("offersPersonalSignIn", () => {
  it("is false for a server that offers only a shared credential", () => {
    expect(
      offersPersonalSignIn(authentication([authClient("static_credential")])),
    ).toBe(false);
  });

  it("is true for a server that offers each person's own sign-in", () => {
    expect(
      offersPersonalSignIn(
        authentication([
          authClient("static_credential"),
          authClient("remote_session"),
        ]),
      ),
    ).toBe(true);
  });

  it("ignores a personal sign-in that is disabled", () => {
    expect(
      offersPersonalSignIn(
        authentication([
          authClient("static_credential"),
          authClient("remote_session", false),
        ]),
      ),
    ).toBe(false);
  });

  it("is false for a server that needs no upstream sign-in", () => {
    expect(offersPersonalSignIn(authentication([]))).toBe(false);
  });

  it("defers to the issuer while setup is incomplete", () => {
    expect(offersPersonalSignIn(authentication([], "setup_incomplete"))).toBe(
      true,
    );
  });
});

describe("serverConnection", () => {
  it("does not offer a session whose authorization ended before its tokens", () => {
    const connection = serverConnection(
      "issuer_one",
      [client("client_one")],
      [
        session({
          authorizationExpiresAt: earlier,
          hasRefreshToken: true,
          refreshExpiresAt: later,
        }),
      ],
      now,
    );
    expect(connection.status).toBe("ended");
    expect(connectionSelectable(connection)).toBe(false);
  });
  it("needs no connection when the issuer asks for no personal sign-in", () => {
    const connection = serverConnection(
      "issuer_one",
      [client("client_shared", "self")],
      [],
      now,
    );
    expect(connection).toEqual({ status: "not-required" });
    expect(connectionSelectable(connection)).toBe(true);
  });

  it("is ready when every personal sign-in has a working connection", () => {
    const mine = session({});
    const connection = serverConnection(
      "issuer_one",
      [client("client_one")],
      [mine],
      now,
    );
    expect(connection).toEqual({ status: "ready", sessions: [mine] });
    expect(connectionSelectable(connection)).toBe(true);
  });

  it("is missing when one sign-in has no connection, or only one through another issuer", () => {
    expect(
      serverConnection(
        "issuer_one",
        [client("client_one"), client("client_two")],
        [session({})],
        now,
      ),
    ).toEqual({ status: "missing" });
    // The attachment endpoint accepts only sessions made through this
    // server's own issuer.
    const elsewhere = serverConnection(
      "issuer_one",
      [client("client_one")],
      [session({ userSessionIssuerId: "issuer_other" })],
      now,
    );
    expect(elsewhere).toEqual({ status: "missing" });
    expect(connectionSelectable(elsewhere)).toBe(false);
  });

  it("prefers a working connection over one that has ended", () => {
    const ended = session({ id: "session_old", accessExpiresAt: earlier });
    const working = session({ id: "session_new" });
    expect(
      serverConnection(
        "issuer_one",
        [client("client_one")],
        [ended, working],
        now,
      ),
    ).toEqual({ status: "ready", sessions: [working] });
  });

  it("does not offer a connection that has ended", () => {
    const ended = session({ accessExpiresAt: earlier });
    const connection = serverConnection(
      "issuer_one",
      [client("client_one")],
      [ended],
      now,
    );
    expect(connection).toEqual({ status: "ended", sessions: [ended] });
    expect(connectionSelectable(connection)).toBe(false);
  });
});

function api(options: {
  auth?: (mcpServerId: string) => Promise<McpServerAuthentication>;
  clients: RemoteSessionClient[];
  candidates: RemoteSession[];
  bindings: Array<{
    id: string;
    remoteSessionClientId: string;
    remoteSession?: RemoteSession;
  }>;
}) {
  const attachBinding = vi.fn().mockResolvedValue({});
  const detachBinding = vi.fn().mockResolvedValue(undefined);
  const pages = <T>(items: T[]) =>
    Promise.resolve([{ result: { items } }]) as never;
  const getMcpServerAuthentication = vi.fn(
    ({ mcpServerId }: { mcpServerId: string }) =>
      options.auth?.(mcpServerId) ??
      Promise.resolve(authentication([authClient("remote_session")])),
  );
  const fake = {
    authClients: { getMcpServerAuthentication },
    remoteSessionClients: { list: vi.fn(() => pages(options.clients)) },
    remoteSessions: {
      list: vi.fn(() => pages(options.candidates)),
      listBindings: vi.fn().mockResolvedValue({ items: options.bindings }),
      attachBinding,
      detachBinding,
    },
  } as unknown as ConnectionApi;
  return { fake, attachBinding, detachBinding, getMcpServerAuthentication };
}

describe("attachOwnerConnections", () => {
  it("attaches the owner's connection to every personal sign-in, once per issuer", async () => {
    const { fake, attachBinding } = api({
      clients: [client("client_one"), client("client_shared", "self")],
      candidates: [session({})],
      bindings: [],
    });
    await attachOwnerConnections(fake, "agent_new", [
      server,
      { ...server, id: "server_linear_copy" },
      { ...server, id: "server_plain", issuerId: undefined },
    ]);
    expect(attachBinding).toHaveBeenCalledTimes(1);
    expect(attachBinding).toHaveBeenCalledWith(
      {
        gramProject: "project-one",
        attachBindingRequestBody: {
          principalId: "agent_new",
          userSessionIssuerId: "issuer_one",
          remoteSessionId: "session_mine",
        },
      },
      undefined,
      expect.anything(),
    );
  });

  it("keeps another person’s valid attachment even without a personal candidate", async () => {
    const { fake, attachBinding, detachBinding } = api({
      clients: [client("client_one")],
      candidates: [session({})],
      bindings: [
        {
          id: "binding_live",
          remoteSessionClientId: "client_one",
          remoteSession: session({ subjectUrn: "user:another_person" }),
        },
      ],
    });
    await attachOwnerConnections(fake, "agent_new", [server]);
    expect(attachBinding).not.toHaveBeenCalled();
    expect(detachBinding).not.toHaveBeenCalled();
  });

  it("replaces the owner's own attachment once its connection was replaced", async () => {
    const { fake, attachBinding, detachBinding } = api({
      clients: [client("client_one")],
      candidates: [session({ id: "session_new" })],
      bindings: [{ id: "binding_stale", remoteSessionClientId: "client_one" }],
    });
    await attachOwnerConnections(fake, "agent_new", [server]);
    expect(detachBinding).toHaveBeenCalledWith(
      {
        gramProject: "project-one",
        detachBindingRequestBody: {
          principalId: "agent_new",
          userSessionIssuerId: "issuer_one",
          id: "binding_stale",
        },
      },
      undefined,
      expect.anything(),
    );
    expect(attachBinding.mock.calls[0]?.[0]).toMatchObject({
      attachBindingRequestBody: { remoteSessionId: "session_new" },
    });
    expect(detachBinding.mock.invocationCallOrder[0]).toBeLessThan(
      attachBinding.mock.invocationCallOrder[0]!,
    );
  });

  it("replaces an attachment whose connection has ended", async () => {
    const { fake, attachBinding, detachBinding } = api({
      clients: [client("client_one")],
      candidates: [session({ id: "session_new" })],
      bindings: [
        {
          id: "binding_ended",
          remoteSessionClientId: "client_one",
          remoteSession: session({
            id: "session_old",
            accessExpiresAt: new Date(Date.now() - 60_000),
          }),
        },
      ],
    });
    await attachOwnerConnections(fake, "agent_new", [server]);
    expect(detachBinding.mock.calls[0]?.[0]).toMatchObject({
      detachBindingRequestBody: { id: "binding_ended" },
    });
    expect(attachBinding.mock.calls[0]?.[0]).toMatchObject({
      attachBindingRequestBody: { remoteSessionId: "session_new" },
    });
  });

  it("reports missing servers during bulk selection without removing assignments", async () => {
    const { fake, attachBinding, detachBinding } = api({
      clients: [client("client_one")],
      candidates: [],
      bindings: [],
    });
    await expect(
      attachOwnerConnections(fake, "agent_new", [server], undefined, true),
    ).resolves.toEqual(["Linear"]);
    expect(attachBinding).not.toHaveBeenCalled();
    expect(detachBinding).not.toHaveBeenCalled();
  });

  it("names the server whose connection is gone instead of attaching nothing", async () => {
    const { fake, attachBinding } = api({
      clients: [client("client_one")],
      candidates: [session({ accessExpiresAt: new Date(Date.now() - 60_000) })],
      bindings: [],
    });
    await expect(
      attachOwnerConnections(fake, "agent_new", [server]),
    ).rejects.toThrow(/Your connection to Linear is no longer available/);
    expect(attachBinding).not.toHaveBeenCalled();
  });

  it("skips a server whose own auth offers only a shared credential, even with an issuer", async () => {
    const { fake, attachBinding } = api({
      auth: () =>
        Promise.resolve(authentication([authClient("static_credential")])),
      // The issuer's personal sign-in belongs to another server.
      clients: [client("client_one")],
      candidates: [],
      bindings: [],
    });
    await expect(
      attachOwnerConnections(fake, "agent_new", [server]),
    ).resolves.toEqual([]);
    expect(fake.remoteSessionClients.list).not.toHaveBeenCalled();
    expect(attachBinding).not.toHaveBeenCalled();
  });

  it("still attaches when a server's auth cannot be read", async () => {
    const { fake, attachBinding } = api({
      auth: () => Promise.reject(new Error("network down")),
      clients: [client("client_one")],
      candidates: [session({})],
      bindings: [],
    });
    await attachOwnerConnections(fake, "agent_new", [server]);
    expect(attachBinding).toHaveBeenCalledTimes(1);
  });
});
