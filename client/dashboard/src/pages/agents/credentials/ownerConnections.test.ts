import { describe, expect, it, vi } from "vitest";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import {
  attachOwnerConnections,
  connectionSelectable,
  serverConnection,
  type ConnectionApi,
} from "./ownerConnections";
import type { InventoryServer } from "./useServerInventory";

const now = Date.parse("2026-10-23T12:00:00Z");
const later = new Date(now + 86_400_000 * 7);
const earlier = new Date(now - 60_000);

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

describe("serverConnection", () => {
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
  const fake = {
    remoteSessionClients: { list: vi.fn(() => pages(options.clients)) },
    remoteSessions: {
      list: vi.fn(() => pages(options.candidates)),
      listBindings: vi.fn().mockResolvedValue({ items: options.bindings }),
      attachBinding,
      detachBinding,
    },
  } as unknown as ConnectionApi;
  return { fake, attachBinding, detachBinding };
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

  it("keeps an attachment that already serves the agent", async () => {
    const { fake, attachBinding, detachBinding } = api({
      clients: [client("client_one")],
      candidates: [session({})],
      bindings: [
        {
          id: "binding_live",
          remoteSessionClientId: "client_one",
          remoteSession: session({}),
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
});
