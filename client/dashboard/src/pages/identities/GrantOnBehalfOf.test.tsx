import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Key } from "@gram/client/models/components/key.js";
import type { McpServerAuthentication } from "@gram/client/models/components/mcpserverauthentication.js";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import type { ServerConnection } from "@/pages/agents/credentials/ownerConnections";
import { GrantOnBehalfOf, ServerOnBehalfOfLine } from "./GrantOnBehalfOf";
import {
  keysUseSharedCredential,
  onBehalfOfPeople,
  serverOnBehalfOf,
} from "./onBehalfOf";

const mocks = vi.hoisted(() => ({
  connection: undefined as ServerConnection | undefined,
  issuerId: undefined as string | undefined,
  inventoryFailed: false,
  enforcement: "legacy" as "loading" | "enforced" | "legacy" | "error",
  keys: [] as Pick<Key, "expiresAt" | "authClientChoices">[],
  authClients: [] as McpServerAuthentication["clients"],
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
}));
vi.mock("@/hooks/useUpstreamAuthEnforcement", () => ({
  useUpstreamAuthEnforcement: () => mocks.enforcement,
}));
vi.mock("@gram/client/react-query/listAPIKeys.js", () => ({
  useListAPIKeys: () => ({
    data: { keys: mocks.keys },
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/mcpServerAuthentication.js", () => ({
  useMcpServerAuthentication: () => ({
    data: { clients: mocks.authClients },
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@/pages/agents/credentials/useServerInventory", () => ({
  useServerInventory: () => ({
    servers: [
      {
        id: "server-1",
        resourceId: "toolset-1",
        name: "Grain",
        issuerId: mocks.issuerId,
      },
    ],
    isLoading: false,
    isError: mocks.inventoryFailed,
  }),
}));
vi.mock("@/pages/agents/credentials/useOwnerConnections", () => ({
  useOwnerConnections: () => ({
    byServer: new Map(),
    assignedByServer: new Map(
      mocks.connection ? [["server-1", mocks.connection]] : [],
    ),
  }),
}));
vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({
    identifier,
    children,
  }: {
    identifier: { userId: string };
    children: ReactNode;
  }) => <a href={`/identities/user:${identifier.userId}`}>{children}</a>,
}));
afterEach(() => {
  cleanup();
  mocks.connection = undefined;
  mocks.issuerId = undefined;
  mocks.inventoryFailed = false;
  mocks.enforcement = "legacy";
  mocks.keys = [];
  mocks.authClients = [];
});

/** A server's shared service account, which agent keys can be set up to use. */
const SERVICE_ACCOUNT = {
  id: "client-service",
  clientType: "service_account",
  enabled: true,
} as McpServerAuthentication["clients"][number];

/** An agent key, live for another hour, set up to use the service account on server-1. */
function chosenKey(
  fields: Partial<Pick<Key, "expiresAt" | "authClientChoices">> = {},
): Pick<Key, "expiresAt" | "authClientChoices"> {
  return {
    expiresAt: new Date(Date.now() + 60 * 60 * 1000),
    authClientChoices: [
      { mcpServerId: "server-1", authClientId: SERVICE_ACCOUNT.id },
    ],
    ...fields,
  };
}

/** A sign-in whose access token ran out, and nothing renews it. */
const LAPSED = { accessExpiresAt: new Date("2020-01-01T00:00:00Z") };

function session(fields: Partial<RemoteSession>): RemoteSession {
  return { subjectUrn: "user:user-ada", ...fields } as RemoteSession;
}

describe("onBehalfOfPeople", () => {
  it("names the owner of a person's own connection", () => {
    expect(
      onBehalfOfPeople({
        status: "ready",
        sessions: [
          session({
            subjectDisplayName: "Ada Lovelace",
            subjectEmail: "ada@example.com",
          }),
        ],
      }),
    ).toEqual([{ userId: "user-ada", name: "Ada Lovelace", expired: false }]);
  });

  it("falls back to the email, and marks a lapsed sign-in", () => {
    expect(
      onBehalfOfPeople({
        status: "ended",
        sessions: [session({ subjectEmail: "ada@example.com", ...LAPSED })],
      }),
    ).toEqual([{ userId: "user-ada", name: "ada@example.com", expired: true }]);
  });

  it("names one person once across their sign-ins", () => {
    expect(
      onBehalfOfPeople({
        status: "ended",
        sessions: [
          session({ subjectDisplayName: "Ada Lovelace" }),
          session({ subjectDisplayName: "Ada Lovelace", ...LAPSED }),
        ],
      }),
    ).toEqual([{ userId: "user-ada", name: "Ada Lovelace", expired: true }]);
  });

  it("names every owner, and marks only the one whose sign-in lapsed", () => {
    expect(
      onBehalfOfPeople({
        status: "ended",
        sessions: [
          session({ subjectDisplayName: "Ada Lovelace" }),
          session({
            subjectUrn: "user:user-bob",
            subjectDisplayName: "Bob Byte",
            ...LAPSED,
          }),
        ],
      }),
    ).toEqual([
      { userId: "user-ada", name: "Ada Lovelace", expired: false },
      { userId: "user-bob", name: "Bob Byte", expired: true },
    ]);
  });

  it.each([
    {
      case: "a shared credential",
      connection: { status: "not-required" as const },
    },
    {
      case: "a connection still being read",
      connection: { status: "checking" as const },
    },
    {
      case: "a missing connection",
      connection: { status: "missing" as const },
    },
    { case: "no connection", connection: undefined },
    {
      case: "a connection with no person behind it",
      connection: {
        status: "ready" as const,
        sessions: [session({ subjectUrn: "agent:agent-1" })],
      },
    },
  ])("acts for no one with $case", ({ connection }) => {
    expect(onBehalfOfPeople(connection)).toEqual([]);
  });
});

describe("GrantOnBehalfOf", () => {
  it("names the person whose connection the agent uses, linked to them", () => {
    mocks.connection = {
      status: "ready",
      sessions: [session({ subjectDisplayName: "Ada Lovelace" })],
    };
    render(<GrantOnBehalfOf agentId="agent-1" resourceId="toolset-1" />);
    expect(screen.getByText(/On behalf of/)).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Ada Lovelace" }).getAttribute("href"),
    ).toBe("/identities/user:user-ada");
    // Their face, drawn as initials when there is no photo.
    expect(screen.getByText("AL")).toBeTruthy();
    expect(screen.queryByText("(connection expired)")).toBeNull();
  });

  it("marks a connection that is assigned but has expired", () => {
    mocks.connection = {
      status: "ended",
      sessions: [session({ subjectDisplayName: "Ada Lovelace", ...LAPSED })],
    };
    render(<GrantOnBehalfOf agentId="agent-1" resourceId="toolset-1" />);
    expect(screen.getByRole("link", { name: "Ada Lovelace" })).toBeTruthy();
    expect(screen.getByText("(connection expired)")).toBeTruthy();
  });

  it("names each person when the sign-ins belong to different people", () => {
    mocks.connection = {
      status: "ended",
      sessions: [
        session({ subjectDisplayName: "Ada Lovelace" }),
        session({
          subjectUrn: "user:user-bob",
          subjectDisplayName: "Bob Byte",
          ...LAPSED,
        }),
      ],
    };
    render(<GrantOnBehalfOf agentId="agent-1" resourceId="toolset-1" />);
    expect(screen.getByRole("link", { name: "Ada Lovelace" })).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Bob Byte" }).getAttribute("href"),
    ).toBe("/identities/user:user-bob");
    // Only Bob's sign-in lapsed.
    expect(screen.getAllByText("(connection expired)")).toHaveLength(1);
    // The comma follows the name directly, with no space before it.
    expect(
      screen.getByRole("link", { name: "Ada Lovelace" }).parentElement
        ?.textContent,
    ).toBe("Ada Lovelace,");
    expect(
      screen.getByRole("link", { name: "Bob Byte" }).parentElement?.textContent,
    ).toBe("Bob Byte (connection expired)");
  });

  it("shows nothing for a shared credential", () => {
    mocks.connection = { status: "not-required" };
    const { container } = render(
      <GrantOnBehalfOf agentId="agent-1" resourceId="toolset-1" />,
    );
    expect(container.textContent).toBe("");
  });
});

describe("serverOnBehalfOf", () => {
  const ADA = session({ subjectDisplayName: "Ada Lovelace" });

  it.each([
    {
      case: "no server yet, inventory loading",
      server: undefined,
      connection: undefined,
      loading: true,
      kind: "checking",
    },
    {
      case: "no such server",
      server: undefined,
      connection: undefined,
      loading: false,
      kind: "unknown",
    },
    {
      case: "a server no one signs in to",
      server: {},
      connection: undefined,
      loading: false,
      kind: "shared",
    },
    {
      case: "a personal server still being read",
      server: { issuerId: "i" },
      connection: undefined,
      loading: false,
      kind: "checking",
    },
    {
      case: "a shared credential",
      server: { issuerId: "i" },
      connection: { status: "not-required" as const },
      loading: false,
      kind: "shared",
    },
    {
      case: "no connection assigned",
      server: { issuerId: "i" },
      connection: { status: "missing" as const },
      loading: false,
      kind: "missing",
    },
    {
      case: "a failed read",
      server: { issuerId: "i" },
      connection: { status: "error" as const },
      loading: false,
      kind: "error",
    },
    {
      case: "an assignment the viewer may not read",
      server: { issuerId: "i" },
      connection: { status: "forbidden" as const },
      loading: false,
      kind: "forbidden",
    },
  ])("says $kind for $case", ({ server, connection, loading, kind }) => {
    expect(serverOnBehalfOf(server, connection, loading).kind).toBe(kind);
  });

  it("names the people whose connections are assigned", () => {
    expect(
      serverOnBehalfOf(
        { issuerId: "i" },
        { status: "ready", sessions: [ADA] },
        false,
      ),
    ).toEqual({
      kind: "people",
      people: [{ userId: "user-ada", name: "Ada Lovelace", expired: false }],
    });
  });
});

describe("keysUseSharedCredential", () => {
  const auth = { clients: [SERVICE_ACCOUNT] };

  it("is true for a live key set up to use the server's shared credential", () => {
    expect(keysUseSharedCredential([chosenKey()], "server-1", auth)).toBe(true);
  });

  it.each([
    {
      case: "an expired key",
      keys: [chosenKey({ expiresAt: new Date("2020-01-01T00:00:00Z") })],
      clients: [SERVICE_ACCOUNT],
    },
    {
      case: "a choice for another server",
      keys: [
        chosenKey({
          authClientChoices: [
            { mcpServerId: "server-2", authClientId: SERVICE_ACCOUNT.id },
          ],
        }),
      ],
      clients: [SERVICE_ACCOUNT],
    },
    {
      case: "a no-auth choice",
      keys: [chosenKey({ authClientChoices: [{ mcpServerId: "server-1" }] })],
      clients: [SERVICE_ACCOUNT],
    },
    {
      case: "a credential the server has disabled",
      keys: [chosenKey()],
      clients: [{ ...SERVICE_ACCOUNT, enabled: false }],
    },
    {
      case: "a credential the server no longer offers",
      keys: [chosenKey()],
      clients: [],
    },
    { case: "keys with no choice", keys: [{}], clients: [SERVICE_ACCOUNT] },
  ])("is false for $case", ({ keys, clients }) => {
    expect(keysUseSharedCredential(keys, "server-1", { clients })).toBe(false);
  });
});

describe("serverOnBehalfOf with key credential choices", () => {
  it("says shared when the keys use a shared credential, even with people assigned", () => {
    expect(
      serverOnBehalfOf(
        { issuerId: "i" },
        {
          status: "ready",
          sessions: [session({ subjectDisplayName: "Ada Lovelace" })],
        },
        false,
        Date.now(),
        true,
      ).kind,
    ).toBe("shared");
  });

  it("waits while the keys' choices are read for a personal server", () => {
    expect(
      serverOnBehalfOf(
        { issuerId: "i" },
        { status: "missing" },
        false,
        Date.now(),
        "checking",
      ).kind,
    ).toBe("checking");
  });
});

describe("ServerOnBehalfOfLine", () => {
  it("says a shared credential chosen for the agent's keys acts for no one", () => {
    mocks.issuerId = "issuer-1";
    mocks.connection = { status: "missing" };
    mocks.enforcement = "enforced";
    mocks.keys = [chosenKey()];
    mocks.authClients = [SERVICE_ACCOUNT];
    const { container } = render(
      <ServerOnBehalfOfLine agentId="agent-1" resourceId="toolset-1" />,
    );
    expect(container.textContent).toBe("Shared credential, acts for no one");
  });

  it("keeps the assigned connections where upstream auth is not enforced", () => {
    mocks.issuerId = "issuer-1";
    mocks.connection = { status: "missing" };
    mocks.keys = [chosenKey()];
    mocks.authClients = [SERVICE_ACCOUNT];
    const { container } = render(
      <ServerOnBehalfOfLine agentId="agent-1" resourceId="toolset-1" />,
    );
    expect(container.textContent).toBe(
      "Needs a person's connection, none assigned",
    );
  });

  it("names the person, as the Permissions tab does", () => {
    mocks.issuerId = "issuer-1";
    mocks.connection = {
      status: "ready",
      sessions: [session({ subjectDisplayName: "Ada Lovelace" })],
    };
    render(<ServerOnBehalfOfLine agentId="agent-1" resourceId="toolset-1" />);
    expect(screen.getByText(/On behalf of/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Ada Lovelace" })).toBeTruthy();
  });

  it("says a server no person signs in to uses a shared credential", () => {
    const { container } = render(
      <ServerOnBehalfOfLine agentId="agent-1" resourceId="toolset-1" />,
    );
    expect(container.textContent).toBe("Shared credential, acts for no one");
  });

  it("says when a personal server has no connection assigned", () => {
    mocks.issuerId = "issuer-1";
    mocks.connection = { status: "missing" };
    const { container } = render(
      <ServerOnBehalfOfLine agentId="agent-1" resourceId="toolset-1" />,
    );
    expect(container.textContent).toBe(
      "Needs a person's connection, none assigned",
    );
  });

  it("says when whose connection it uses is not visible to the viewer", () => {
    mocks.issuerId = "issuer-1";
    mocks.connection = { status: "forbidden" };
    const { container } = render(
      <ServerOnBehalfOfLine agentId="agent-1" resourceId="toolset-1" />,
    );
    expect(container.textContent).toBe(
      "Whose credential it uses is not visible to you",
    );
  });

  it("says when the server is not known", () => {
    const { container } = render(
      <ServerOnBehalfOfLine agentId="agent-1" resourceId={undefined} />,
    );
    expect(container.textContent).toBe("Credential not known");
  });

  it("says it could not check when the server list failed", () => {
    mocks.inventoryFailed = true;
    const { container } = render(
      <ServerOnBehalfOfLine agentId="agent-1" resourceId="server-unlisted" />,
    );
    expect(container.textContent).toBe(
      "Could not check whose credential it uses",
    );
  });
});
