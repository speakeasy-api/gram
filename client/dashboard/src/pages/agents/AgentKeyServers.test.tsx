vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
}));
import { useState } from "react";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { AgentKeyServers, type KeyServer } from "./AgentKeyServers";
const mocks = vi.hoisted(() => ({
  domains: vi.fn(),
  toolsets: vi.fn(),
  modern: vi.fn(),
  detail: vi.fn(),
  endpoints: vi.fn(),
  clients: vi.fn(),
  candidates: vi.fn(),
  bindings: vi.fn(),
  attach: vi.fn(),
  detach: vi.fn(),
  userId: "user_example",
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org_example",
    projects: [{ id: "project_one", name: "Project one", slug: "project-one" }],
  }),
  useSession: () => ({ user: { id: mocks.userId } }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    domains: { listDomains: mocks.domains },
    toolsets: { getBySlug: mocks.detail },
    mcpEndpoints: { list: mocks.endpoints },
    remoteSessionClients: { list: mocks.clients },
    remoteSessions: {
      list: async (request: { principalId: string }) => [
        { result: { items: await mocks.candidates(request.principalId) } },
      ],
      attachBinding: (request: {
        attachBindingRequestBody: {
          principalId: string;
          remoteSessionId: string;
        };
      }) =>
        mocks.attach(
          request.attachBindingRequestBody.principalId,
          request.attachBindingRequestBody.remoteSessionId,
        ),
      detachBinding: (request: {
        detachBindingRequestBody: { principalId: string; id: string };
      }) =>
        mocks.detach(
          request.detachBindingRequestBody.principalId,
          request.detachBindingRequestBody.id,
        ),
      listBindings: async (request: { principalId: string }) => ({
        items: await mocks.bindings(request.principalId),
      }),
    },
  }),
}));
vi.mock("@/lib/utils", () => ({
  cn: (...values: string[]) => values.filter(Boolean).join(" "),
  getServerURL: () => "https://gram.example",
  firstPartyConnectUrl: (url: string | undefined) =>
    url ? `${url}/connect/first-party` : undefined,
}));
vi.mock("@gram/client/react-query/listToolsetsForOrg.js", () => ({
  useListToolsetsForOrg: (_r: unknown, _s: unknown, options: object) =>
    useQuery({ queryKey: ["toolsets"], queryFn: mocks.toolsets, ...options }),
}));
vi.mock("@gram/client/react-query/listMcpServersForOrg.js", () => ({
  useListMcpServersForOrg: (_r: unknown, _s: unknown, options: object) =>
    useQuery({ queryKey: ["modern"], queryFn: mocks.modern, ...options }),
}));
const agent = {
  id: "agent_example",
  ownerUserId: "user_example",
  permissions: { write: true },
} as ManagedAgent;
const server: KeyServer = {
  id: "remote",
  resourceId: "remote",
  name: "Remote server",
  projectId: "project_one",
  projectSlug: "project-one",
  kind: "Remote",
  issuerId: "issuer_example",
  endpoints: ["https://gram.example/mcp/remote"],
  connectUrl: "https://gram.example/mcp/remote/connect/first-party",
};
function setup(
  step = 0,
  initial: KeyServer[] = [],
  discovery = {
    complete: true,
    error: false,
    grants: [
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    ],
  },
) {
  const ready = vi.fn<(ready: boolean) => void>();
  const changed = vi.fn();
  const inventory = vi.fn();
  function Harness() {
    const [selected, setSelected] = useState(initial);
    return (
      <AgentKeyServers
        agent={agent}
        discoveryComplete={discovery.complete}
        discoveryError={discovery.error}
        grants={
          discovery.grants as import("@gram/client/models/components/agentpolicygrantform.js").AgentPolicyGrantForm[]
        }
        onInventory={(servers) => {
          inventory(servers);
        }}
        step={step}
        selected={selected}
        onChange={(next) => {
          changed(next);
          setSelected(next);
        }}
        onReady={ready}
      />
    );
  }
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <Harness />
    </QueryClientProvider>,
  );
  return { ready, changed, inventory, queryClient };
}
beforeEach(() => {
  vi.resetAllMocks();
  vi.spyOn(window, "open").mockReturnValue({
    location: { href: "about:blank" },
    focus: vi.fn(),
    closed: false,
    opener: window,
  } as unknown as Window);
  mocks.domains.mockResolvedValue({
    domains: [{ id: "domain_example", domain: "mcp.example.test" }],
  });
  mocks.toolsets.mockResolvedValue({
    toolsets: [
      {
        mcpEnabled: true,
        id: "legacy",
        name: "Empty toolset",
        slug: "legacy",
        projectId: "project_one",
        tools: [],
      },
    ],
  });
  mocks.modern.mockResolvedValue({
    mcpServers: [
      {
        id: "remote",
        name: "Remote server",
        projectId: "project_one",
        remoteMcpServerId: "backend",
        userSessionIssuerId: "issuer_example",
      },
      {
        id: "tunnel",
        name: "Tunnel server",
        projectId: "project_one",
        tunneledMcpServerId: "tunnel_backend",
      },
      {
        id: "hosted",
        name: "Hosted server",
        projectId: "project_one",
        toolsetId: "hosted_toolset",
      },
      {
        id: "unproxied",
        name: "Unproxied server",
        projectId: "project_one",
        unproxiedMcpServerId: "unproxied_backend",
      },
    ],
  });
  mocks.endpoints.mockResolvedValue({ mcpEndpoints: [{ slug: "remote" }] });
  mocks.detail.mockResolvedValue({
    slug: "legacy",
    mcpSlug: "legacy",
    userSessionIssuerId: "legacy_issuer",
  });
  mocks.clients.mockResolvedValue([
    {
      result: {
        items: [{ id: "client_example", clientId: "Example upstream" }],
      },
    },
  ]);
  mocks.candidates.mockResolvedValue([
    {
      id: "account_example",
      remoteSessionClientId: "client_example",
      scopes: ["read"],
      accessExpiresAt: new Date(Date.now() + 3 * 86_400_000),
      hasRefreshToken: false,
    },
  ]);
  mocks.userId = "user_example";
  mocks.bindings.mockResolvedValue([]);
  mocks.attach.mockResolvedValue(undefined);
  mocks.detach.mockResolvedValue(undefined);
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
describe("Agent key MCP servers", () => {
  it("explains unavailable inventory when only disabled or unproxied servers exist", async () => {
    mocks.toolsets.mockResolvedValue({
      toolsets: [
        {
          id: "disabled",
          name: "Disabled server",
          slug: "disabled",
          projectId: "project_one",
          mcpEnabled: false,
        },
      ],
    });
    mocks.modern.mockResolvedValue({
      mcpServers: [
        {
          id: "unproxied",
          name: "Unproxied server",
          projectId: "project_one",
          unproxiedMcpServerId: "unproxied_backend",
        },
      ],
    });
    setup();
    expect(await screen.findByText("No servers available")).toBeTruthy();
    expect(screen.queryByText("No matching servers")).toBeNull();
    expect(
      screen.getByText("Add an MCP server to a project before creating a key."),
    ).toBeTruthy();
  });
  it("offers credential-capable servers and excludes unproxied servers", async () => {
    setup();
    for (const name of [
      "Remote server",
      "Tunnel server",
      "Hosted server",
      "Empty toolset",
    ])
      expect(
        await screen.findByRole("checkbox", { name: new RegExp(name) }),
      ).toBeTruthy();
  });
  it("searches by server and project while preserving hidden selections", async () => {
    setup();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "Hosted server" }),
    );
    await screen.findByText("1 server selected");
    const search = screen.getByRole("textbox", { name: "Search servers" });
    fireEvent.change(search, { target: { value: "remote" } });
    expect(screen.queryAllByRole("checkbox")).toHaveLength(1);
    expect(screen.getByText("1 server selected")).toBeTruthy();
    fireEvent.change(search, { target: { value: "no match" } });
    expect(screen.getByText("No matching servers")).toBeTruthy();
    fireEvent.change(search, { target: { value: "PROJECT ONE" } });
    expect(screen.getAllByRole("checkbox")).toHaveLength(4);
    expect(
      screen
        .getByRole("checkbox", { name: "Hosted server" })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });
  it("shows readable account choices and explains agent-level persistence", async () => {
    setup(1, [server]);
    await screen.findByRole("button", { name: "Use account" });
    expect(screen.getByText("Identity unavailable")).toBeTruthy();
    expect(
      screen.getByText(/Each connection is shared by this agent’s keys/),
    ).toBeTruthy();
    expect(screen.queryByText(/account_example|client_example/)).toBeNull();
    expect(screen.getByText("Access: read")).toBeTruthy();
  });
  it("uses the selected canonical session's stored identity, not the Speakeasy subject", async () => {
    mocks.candidates.mockResolvedValue([
      {
        id: "account_example",
        remoteSessionClientId: "client_example",
        scopes: ["read"],
        upstreamDisplayName: "Example upstream user",
        upstreamEmail: "upstream@example.test",
        subjectDisplayName: "Speakeasy subject",
        subjectEmail: "gram@example.test",
      },
    ]);
    setup(1, [server]);
    expect(
      await screen.findByText("Example upstream user · upstream@example.test"),
    ).toBeTruthy();
    expect(
      screen.queryByText(/Speakeasy subject|gram@example.test/),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Use account" }));
    await waitFor(() =>
      expect(mocks.attach).toHaveBeenCalledWith(
        "agent_example",
        "account_example",
      ),
    );
  });
  it("fails closed when half the inventory fails", async () => {
    mocks.modern.mockRejectedValue(new Error("Unavailable"));
    setup();
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Could not load all MCP servers",
    );
    expect(screen.queryAllByRole("checkbox")).toHaveLength(0);
  });
  it("loads modern endpoints using the owning project and preserves grant resource IDs", async () => {
    const { changed } = setup();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /Hosted server/ }),
    );
    await waitFor(() => expect(changed).toHaveBeenCalled());
    expect(mocks.endpoints).toHaveBeenCalledWith({
      mcpServerId: "hosted",
      gramProject: "project-one",
    });
    expect(changed.mock.calls[0]?.[0][0]).toMatchObject({
      resourceId: "hosted_toolset",
      endpoints: ["https://gram.example/mcp/remote"],
    });
  });
  it("resolves custom-domain endpoint hosts without inventing platform-domain aliases", async () => {
    mocks.endpoints.mockResolvedValue({
      mcpEndpoints: [{ slug: "custom-slug", customDomainId: "domain_example" }],
    });
    const { changed } = setup();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /Hosted server/ }),
    );
    await waitFor(() => expect(changed).toHaveBeenCalled());
    expect(changed.mock.calls[0]?.[0][0]).toMatchObject({
      endpoints: ["https://mcp.example.test/mcp/custom-slug"],
    });
    expect(changed.mock.calls[0]?.[0][0].connectUrl).toBeUndefined();
  });
  it("does not invent URLs for unknown custom domains or unroutable legacy toolsets", async () => {
    mocks.domains.mockResolvedValue({ domains: [] });
    mocks.endpoints.mockResolvedValue({
      mcpEndpoints: [{ slug: "custom-slug", customDomainId: "unknown" }],
    });
    mocks.detail.mockResolvedValue({ slug: "legacy" });
    const { changed } = setup();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /Hosted server/ }),
    );
    await waitFor(() => expect(changed).toHaveBeenCalled());
    expect(changed.mock.calls[0]?.[0][0].endpoints).toEqual([]);
    fireEvent.click(screen.getByRole("checkbox", { name: /Empty toolset/ }));
    await waitFor(() => expect(changed).toHaveBeenCalledTimes(2));
    expect(changed.mock.calls[1]?.[0][1].endpoints).toEqual([]);
  });
  it("omits account connect URLs for legacy environment routes without an MCP slug", async () => {
    mocks.detail.mockResolvedValue({
      slug: "legacy",
      defaultEnvironmentSlug: "default",
      userSessionIssuerId: "legacy_issuer",
    });
    const { changed } = setup();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /Empty toolset/ }),
    );
    await waitFor(() => expect(changed).toHaveBeenCalled());
    expect(changed.mock.calls[0]?.[0][0].connectUrl).toBeUndefined();
    expect(changed.mock.calls[0]?.[0][0].endpoints).toEqual([
      "https://gram.example/mcp/project-one/legacy/default",
    ]);
  });
  it("loads legacy issuer metadata without fetching remote MCP endpoints", async () => {
    const { changed } = setup();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /Empty toolset/ }),
    );
    await waitFor(() => expect(changed).toHaveBeenCalled());
    expect(mocks.detail).toHaveBeenCalledWith({
      slug: "legacy",
      gramProject: "project-one",
    });
    expect(changed.mock.calls[0]?.[0][0]).toMatchObject({
      issuerId: "legacy_issuer",
    });
    expect(
      screen.queryByRole("checkbox", { name: /Unproxied server/ }),
    ).toBeNull();
    expect(mocks.endpoints).not.toHaveBeenCalled();
  });
  it("reuses an account through attachment APIs and waits for confirmed active bindings", async () => {
    const { ready, queryClient } = setup(1, [server]);
    queryClient.setQueryData(["owner-connections", "bindings", agent.id], []);
    fireEvent.click(await screen.findByRole("button", { name: "Use account" }));
    await waitFor(() =>
      expect(mocks.attach).toHaveBeenCalledWith(
        "agent_example",
        "account_example",
      ),
    );
    await waitFor(() =>
      expect(
        queryClient.getQueryState(["owner-connections", "bindings", agent.id])
          ?.isInvalidated,
      ).toBe(true),
    );
    expect(ready).not.toHaveBeenCalledWith(true);
    mocks.bindings.mockResolvedValue([
      {
        remoteSessionId: "account_example",
        remoteSessionClientId: "client_example",
        remoteSession: {
          id: "account_example",
          remoteSessionClientId: "client_example",
        },
      },
    ]);
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: "Refresh connection status",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Refresh connection status" }),
    );
    await screen.findByText("Accounts ready.");
    expect(ready).toHaveBeenLastCalledWith(true);
  });
  it.each(["account_example", "revoked_account"])(
    "keeps tombstone %s detach-only even if a new grant has the same session ID",
    async (sessionId) => {
      mocks.bindings.mockResolvedValue([
        {
          id: "old_binding",
          remoteSessionId: sessionId,
          remoteSessionClientId: "client_example",
        },
      ]);
      const { ready } = setup(1, [server]);
      const detach = await screen.findByRole("button", {
        name: "Disconnect unavailable account",
      });
      expect(ready).not.toHaveBeenCalledWith(true);
      expect(screen.queryByRole("button", { name: "Connected" })).toBeNull();
      mocks.bindings.mockResolvedValue([]);
      fireEvent.click(detach);
      await waitFor(() =>
        expect(mocks.detach).toHaveBeenCalledWith(
          "agent_example",
          "old_binding",
        ),
      );
      expect(
        await screen.findByRole("button", { name: "Use account" }),
      ).toBeTruthy();
      expect(ready).not.toHaveBeenCalledWith(true);
    },
  );
  it("says when an account stops working and that it does not refresh", async () => {
    setup(1, [server]);
    expect(
      await screen.findByText(
        /^Expires .* \(in 3 days\) and does not refresh$/,
      ),
    ).toBeTruthy();
  });
  it("allows an authorized editor to assign their own connection", async () => {
    mocks.userId = "user_manager";
    setup(1, [server]);
    fireEvent.click(await screen.findByRole("button", { name: "Use account" }));
    await waitFor(() => expect(mocks.attach).toHaveBeenCalled());
  });
  it.each(["user:user_example", "user:other"])(
    "allows an editor to remove another person’s assignment (%s)",
    async (subjectUrn) => {
      mocks.userId = "user_manager";
      mocks.bindings.mockResolvedValue([
        {
          id: "other_binding",
          remoteSessionId: "other_session",
          remoteSessionClientId: "client_example",
          remoteSession: {
            id: "other_session",
            subjectUrn,
            subjectDisplayName: "Jordan Example",
            remoteSessionClientId: "client_example",
            scopes: [],
            hasRefreshToken: false,
            accessExpiresAt: new Date(Date.now() + 86_400_000),
          },
        },
      ]);
      const { ready } = setup(1, [server]);
      expect(
        await screen.findByText("Jordan Example's connection"),
      ).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Remove" })).toBeTruthy();
      expect(
        screen.queryByRole("button", { name: "Reauthenticate" }),
      ).toBeNull();
      await waitFor(() => expect(ready).toHaveBeenCalledWith(true));
      expect(mocks.detach).not.toHaveBeenCalled();
    },
  );
  it.each(["access", "authorization"])(
    "preserves reauthentication through focus while OAuth is open and binds after it closes (%s)",
    async (expiry) => {
      const expired = {
        id: "expired",
        subjectUrn: "user:user_example",
        subjectDisplayName: "Alex Example",
        remoteSessionClientId: "client_example",
        scopes: [],
        hasRefreshToken: false,
        accessExpiresAt: new Date(
          Date.now() + (expiry === "access" ? -86_400_000 : 86_400_000),
        ),
        authorizationExpiresAt:
          expiry === "authorization"
            ? new Date(Date.now() - 86_400_000)
            : undefined,
      };
      mocks.bindings.mockResolvedValue([
        {
          id: "expired_binding",
          remoteSessionId: expired.id,
          remoteSessionClientId: "client_example",
          remoteSession: expired,
        },
      ]);
      mocks.candidates.mockResolvedValue([expired]);
      const { ready } = setup(1, [server]);
      expect(
        await screen.findByRole("button", { name: "Reauthenticate" }),
      ).toBeTruthy();
      expect(ready).not.toHaveBeenCalledWith(true);
      const refreshLink = screen.getByRole("button", {
        name: "Reauthenticate",
      });
      fireEvent.click(refreshLink);
      expect(window.open).toHaveBeenCalledWith(
        "about:blank",
        "speakeasy-connection",
        "popup=yes,width=560,height=720",
      );
      expect(screen.getByText(/^Expired ·/)).toBeTruthy();
      expect(mocks.detach).not.toHaveBeenCalled();
      const callsBeforeFocus = mocks.candidates.mock.calls.length;
      await act(async () => {
        fireEvent(window, new Event("focus"));
        await new Promise((resolve) => setTimeout(resolve, 0));
      });
      expect(mocks.candidates.mock.calls.length).toBe(callsBeforeFocus);
      expect(mocks.attach).not.toHaveBeenCalled();
      const renewed = {
        ...expired,
        id: "renewed",
        authorizationExpiresAt: undefined,
        accessExpiresAt: new Date(Date.now() + 3_600_000),
      };
      mocks.candidates.mockResolvedValue([renewed]);
      mocks.attach.mockImplementation(async () => {
        mocks.bindings.mockResolvedValue([
          {
            id: "renewed_binding",
            remoteSessionId: renewed.id,
            remoteSessionClientId: renewed.remoteSessionClientId,
            remoteSession: renewed,
          },
        ]);
      });
      const popup = vi.mocked(window.open).mock.results[0]?.value as {
        closed: boolean;
      };
      popup.closed = true;
      fireEvent(window, new Event("focus"));
      await waitFor(() =>
        expect(mocks.attach).toHaveBeenCalledWith(agent.id, renewed.id),
      );
      expect(mocks.detach).toHaveBeenCalledWith(agent.id, "expired_binding");
      expect(mocks.attach).toHaveBeenCalledTimes(1);
      expect(mocks.detach).toHaveBeenCalledTimes(1);
      await waitFor(() => expect(ready).toHaveBeenCalledWith(true));
    },
  );
  it("does not replace an expired binding with a pre-existing grant before OAuth completes", async () => {
    const old = {
      id: "old",
      subjectUrn: "user:user_example",
      remoteSessionClientId: "client_example",
      scopes: [],
      hasRefreshToken: false,
      accessExpiresAt: new Date(Date.now() - 1000),
    };
    const live = {
      ...old,
      id: "already_live",
      accessExpiresAt: new Date(Date.now() + 3600000),
    };
    mocks.bindings.mockResolvedValue([
      {
        id: "binding",
        remoteSessionId: old.id,
        remoteSessionClientId: "client_example",
        remoteSession: old,
      },
    ]);
    mocks.candidates.mockResolvedValue([live]);
    setup(1, [server]);
    fireEvent.click(
      await screen.findByRole("button", { name: "Reauthenticate" }),
    );
    expect(mocks.attach).not.toHaveBeenCalled();
    const calls = mocks.candidates.mock.calls.length;
    const popup = vi.mocked(window.open).mock.results[0]?.value as {
      closed: boolean;
    };
    popup.closed = true;
    fireEvent(window, new Event("focus"));
    await waitFor(() =>
      expect(mocks.candidates.mock.calls.length).toBeGreaterThan(calls),
    );
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: "Reauthenticate",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    expect(mocks.detach).not.toHaveBeenCalled();
    mocks.candidates.mockResolvedValue([
      { ...live, id: "unrelated_later_login" },
    ]);
    fireEvent(window, new Event("focus"));
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: "Reauthenticate",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    expect(mocks.attach).not.toHaveBeenCalled();
  });
  it("reports blocked popups without detaching an account", async () => {
    vi.mocked(window.open).mockReturnValue(null);
    setup(1, [server]);
    fireEvent.click(
      await screen.findByRole("button", { name: "Connect an account" }),
    );
    expect(await screen.findByText(/Allow popups/)).toBeTruthy();
    expect(mocks.detach).not.toHaveBeenCalled();
  });
  it("reloads connections automatically when the sign-in popup closes", async () => {
    const popup = {
      location: { href: "" },
      focus: vi.fn(),
      closed: false,
      opener: window as Window | null,
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    setup(1, [server]);
    fireEvent.click(
      await screen.findByRole("button", { name: "Connect an account" }),
    );
    expect(popup.opener).toBeNull();
    const calls = mocks.candidates.mock.calls.length;
    popup.closed = true;
    await waitFor(() =>
      expect(mocks.candidates.mock.calls.length).toBeGreaterThan(calls),
    );
  });
  it("does not accept missing or stale bindings as ready", async () => {
    mocks.bindings.mockResolvedValue([
      {
        id: "stale_binding",
        remoteSessionId: "stale_account",
        remoteSessionClientId: "client_example",
      },
    ]);
    const { ready } = setup(1, [server]);
    await screen.findByRole("button", { name: "Another account in use" });
    expect(ready).not.toHaveBeenCalledWith(true);
  });
  it("blocks on failed account discovery instead of assuming no accounts are required", async () => {
    mocks.candidates.mockRejectedValue(new Error("Denied"));
    const { ready } = setup(1, [server]);
    await screen.findByRole("alert");
    expect(ready).not.toHaveBeenCalledWith(true);
  });
  it("does not require account attachments when no issuer is configured", async () => {
    const { ready } = setup(1, [{ ...server, issuerId: undefined }]);
    // Nothing to connect, so the step says so instead of confirming accounts.
    await screen.findByText(/None of the servers you chose need one/);
    expect(ready).toHaveBeenLastCalledWith(true);
    expect(mocks.clients).not.toHaveBeenCalled();
  });
  it("offers only discovered MCP connect resources while inventory uses normalized IDs", async () => {
    const { inventory } = setup(0, [], {
      complete: true,
      error: false,
      grants: [
        {
          effect: "allow",
          scope: "mcp:connect",
          selector: { resourceKind: "mcp", resourceId: "hosted_toolset" },
        },
        {
          effect: "allow",
          scope: "mcp:read",
          selector: { resourceKind: "mcp", resourceId: "remote" },
        },
      ],
    });
    await screen.findByRole("checkbox", { name: /Hosted server/ });
    expect(screen.getAllByRole("checkbox")).toHaveLength(1);
    expect(inventory).toHaveBeenLastCalledWith(
      expect.arrayContaining([
        expect.objectContaining({ id: "hosted", resourceId: "hosted_toolset" }),
        expect.objectContaining({ id: "remote", resourceId: "remote" }),
        expect.objectContaining({ id: "legacy", resourceId: "legacy" }),
      ]),
    );
    expect(inventory.mock.calls.at(-1)?.[0]).not.toEqual(
      expect.arrayContaining([expect.objectContaining({ id: "unproxied" })]),
    );
  });
  it.each(["loading", "error", "empty"])(
    "withholds server choices for %s discovery",
    async (state) => {
      setup(0, [], {
        complete: state === "empty",
        error: state === "error",
        grants: [],
      });
      await waitFor(() => expect(mocks.modern).toHaveBeenCalled());
      expect(screen.queryAllByRole("checkbox")).toHaveLength(0);
      expect(mocks.endpoints).not.toHaveBeenCalled();
    },
  );
  it("excludes legacy toolsets without explicitly enabled MCP", async () => {
    mocks.toolsets.mockResolvedValue({
      toolsets: [{ id: "legacy", name: "Disabled", projectId: "project_one" }],
    });
    const { inventory } = setup();
    await screen.findByRole("checkbox", { name: /Hosted server/ });
    expect(screen.queryByRole("checkbox", { name: /Disabled/ })).toBeNull();
    expect(inventory.mock.calls.at(-1)?.[0]).not.toEqual(
      expect.arrayContaining([expect.objectContaining({ id: "legacy" })]),
    );
  });
});

vi.mock("@gram/client/react-query/remoteSessionsAttachBinding.js", () => ({
  useRemoteSessionsAttachBindingMutation: () => ({
    mutateAsync: ({
      request,
      options,
    }: {
      request: {
        attachBindingRequestBody: {
          principalId: string;
          remoteSessionId: string;
        };
      };
      options?: { signal?: AbortSignal };
    }) => {
      const body = request.attachBindingRequestBody;
      return options
        ? mocks.attach(body.principalId, body.remoteSessionId, options.signal)
        : mocks.attach(body.principalId, body.remoteSessionId);
    },
  }),
}));
vi.mock("@gram/client/react-query/remoteSessionsDetachBinding.js", () => ({
  useRemoteSessionsDetachBindingMutation: () => ({
    mutateAsync: ({
      request,
      options,
    }: {
      request: {
        detachBindingRequestBody: { principalId: string; id: string };
      };
      options?: { signal?: AbortSignal };
    }) => {
      const body = request.detachBindingRequestBody;
      return options
        ? mocks.detach(body.principalId, body.id, options.signal)
        : mocks.detach(body.principalId, body.id);
    },
  }),
}));
