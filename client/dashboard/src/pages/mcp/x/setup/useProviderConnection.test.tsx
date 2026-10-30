import { useReducer, type PropsWithChildren } from "react";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GramError } from "@gram/client/models/errors/gramerror";
import type { McpServer } from "@gram/client/models/components/mcpserver";
import {
  initialState,
  reduce,
  type State,
} from "@/pages/catalog/provider-setup/state";
import { scopesFor } from "@/pages/catalog/provider-setup/provider-data";
import { useProviderConnection } from "./useProviderConnection";

const mocks = vi.hoisted(() => ({
  associations: vi.fn(),
  client: vi.fn(),
  sessions: vi.fn(),
  commit: vi.fn(),
  provider: vi.fn(),
  issuer: vi.fn(),
  createIssuer: vi.fn(),
  server: vi.fn(),
  updateServer: vi.fn(),
  updateClient: vi.fn(),
  revoke: vi.fn(),
  probe: vi.fn(),
  serverUrl: "https://platform.example",
}));
vi.mock("@/contexts/Sdk", () => {
  const client = {
    remoteSessions: {
      listServerClientAssociations: mocks.associations,
      list: mocks.sessions,
      commitServerIdentityConfiguration: mocks.commit,
      revoke: mocks.revoke,
    },
    remoteSessionClients: {
      get: mocks.client,
      update: async (request: {
        updateRemoteSessionClientForm: { id: string; scope: string[] };
      }) => {
        const result = await mocks.updateClient(request);
        const current = await mocks.client({
          id: request.updateRemoteSessionClientForm.id,
        });
        mocks.client.mockResolvedValue({
          ...current,
          scope: request.updateRemoteSessionClientForm.scope,
        });
        return result;
      },
    },
    remoteSessionIssuers: { get: mocks.provider },
    userSessionIssuers: { get: mocks.issuer, create: mocks.createIssuer },
    mcpServers: { get: mocks.server, update: mocks.updateServer },
  };
  return { useSdkClient: () => client };
});
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "person" }, session: "dashboard-session" }),
  useProject: () => ({ id: "project", slug: "example-project" }),
}));
vi.mock("@gram/client/funcs/remoteSessionsTestServerConnection", () => ({
  remoteSessionsTestServerConnection: mocks.probe,
}));
vi.mock("@/lib/utils", () => ({
  getServerURL: () => mocks.serverUrl,
  mcpConnectionUrl: (url: string) => url,
  firstPartyConnectUrl: (url: string) => `${url}/connect/first-party`,
}));
vi.mock("@/lib/route-errors", () => ({
  isNotFoundError: (err: { statusCode?: number }) => err.statusCode === 404,
}));

const server = {
  id: "server-123456789012",
  visibility: "private",
  userSessionIssuerId: "project-issuer",
  platformEndpointSlug: "demo-endpoint",
  remoteMcpServerId: "upstream",
  environmentId: "env",
  toolVariationsGroupId: "variations",
} as McpServer;
const clients: QueryClient[] = [];
function mount(provider: "slack", override: Partial<State> = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  return renderHook(
    ({ mcpServer }: { mcpServer: McpServer }) => {
      const [state, dispatch] = useReducer(reduce, {
        ...initialState(provider),
        ...override,
      });
      const connection = useProviderConnection(state, dispatch, mcpServer);
      return { state, ...connection };
    },
    {
      initialProps: { mcpServer: server },
      wrapper: ({ children }: PropsWithChildren) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    },
  );
}
function saved(_provider: "slack", scopes: string[] = []) {
  mocks.associations.mockResolvedValue({
    associations: [
      {
        issuerUrl: "https://mcp.slack.com",
        remoteSessionClientId: "client",
        serverCount: 1,
      },
    ],
  });
  mocks.client.mockResolvedValue({
    id: "client",
    scope: scopes,
    callbackUrl: "https://platform.example/callback",
  });
}
async function* pages(items: unknown[]) {
  yield { result: { items } };
}
beforeEach(() => {
  vi.resetAllMocks();
  mocks.serverUrl = "https://platform.example";
  mocks.associations.mockResolvedValue({ associations: [] });
  mocks.sessions.mockImplementation(async () => pages([]));
  mocks.server.mockResolvedValue(server);
  mocks.issuer.mockResolvedValue({
    id: "project-issuer",
    projectId: "project",
  });
  mocks.provider.mockResolvedValue({ id: "provider" });
  mocks.commit.mockResolvedValue({ status: "registered" });
  mocks.updateServer.mockImplementation(async ({ updateMcpServerForm }) => {
    const updated = { ...server, ...updateMcpServerForm };
    mocks.server.mockResolvedValue(updated);
    return updated;
  });
  mocks.probe.mockResolvedValue({
    ok: true,
    value: {
      status: "ready",
      code: "ready",
      message: "Connected.",
      toolCount: 2,
    },
  });
  vi.spyOn(window, "open").mockReturnValue(null);
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((c) => c.clear());
  vi.restoreAllMocks();
});

describe("live provider setup", () => {
  it.each(["slack"] as const)(
    "saves %s credentials through the backend and clears the form",
    async (provider) => {
      const state = initialState(provider);
      const { result } = mount(provider, {
        step: "credentials",
        values: {
          ...state.values,
          clientId: " app-id ",
          clientSecret: " app-secret ",
        },
      });
      await waitFor(() => expect(result.current.loading).toBe(false));
      act(() => result.current.dispatch({ type: "next" }));
      await waitFor(() => expect(result.current.state.saved).toBe(true));
      expect(mocks.commit).toHaveBeenCalledWith({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          mcpServerId: server.id,
          targetServerOnly: true,
          providerId: "provider",
          clientMode: "manual",
          clientConfiguration: {
            clientId: "app-id",
            clientSecret: "app-secret",
            tokenEndpointAuthMethod: "client_secret_post",
            scope: provider === "slack" ? scopesFor(state) : [],
          },
        }),
      });
      expect(result.current.state.values.clientId).toBe("");
      expect(result.current.state.values.clientSecret).toBe("");
      expect(result.current.state.step).toBe("test");
      expect(result.current.state.authorized).toBe(false);
    },
  );

  it("isolates an organization issuer and preserves the server references before binding the app", async () => {
    mocks.issuer.mockImplementation(async ({ slug }) => {
      if (slug) throw { statusCode: 404 };
      return { id: "organization-issuer" };
    });
    mocks.createIssuer.mockResolvedValue({ id: "demo-issuer" });
    mocks.provider.mockRejectedValue({ statusCode: 404 });
    const state = initialState("slack");
    const { result } = mount("slack", {
      step: "credentials",
      values: {
        ...state.values,
        clientId: "app-id",
        clientSecret: "app-secret",
      },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.dispatch({ type: "next" }));
    await waitFor(() => expect(result.current.pending).toBeNull());
    expect(mocks.updateServer).toHaveBeenCalledWith({
      updateMcpServerForm: {
        id: server.id,
        visibility: "private",
        remoteMcpServerId: "upstream",
        environmentId: "env",
        toolVariationsGroupId: "variations",
        userSessionIssuerId: "demo-issuer",
      },
    });
    expect(mocks.commit).toHaveBeenCalledWith({
      commitServerIdentityConfigurationForm: expect.objectContaining({
        createProvider: expect.objectContaining({
          issuer: "https://mcp.slack.com",
          authorizationEndpoint: "https://slack.com/oauth/v2_user/authorize",
          tokenEndpoint: "https://slack.com/api/oauth.v2.user.access",
          authorizationResponseIssParameterSupported: false,
        }),
      }),
    });
    expect(mocks.updateServer.mock.invocationCallOrder[0]).toBeLessThan(
      mocks.commit.mock.invocationCallOrder[0]!,
    );
  });

  it("keeps a rejected save on the credentials step without marking it saved", async () => {
    mocks.commit.mockRejectedValue(new Error("Saving failed"));
    const state = initialState("slack");
    const { result } = mount("slack", {
      step: "credentials",
      values: { ...state.values, clientId: "id", clientSecret: "secret" },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.dispatch({ type: "next" }));
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(result.current.errorStep).toBe("save");
    expect(result.current.state.saved).toBe(false);
    expect(result.current.state.step).toBe("credentials");
    expect(result.current.state.values.clientSecret).toBe("secret");
    mocks.commit.mockResolvedValue({ status: "registered" });
    act(() => result.current.dispatch({ type: "next" }));
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    expect(result.current.error).toBeNull();
    expect(result.current.state.values.clientSecret).toBe("");
  });

  it("requires a dedicated identity when the backend refuses a shared save", async () => {
    mocks.commit.mockRejectedValue(
      new GramError("internal detail", {
        request: new Request("https://platform.example"),
        response: new Response("", { status: 409 }),
        body: "",
      }),
    );
    const state = initialState("slack");
    const { result } = mount("slack", {
      step: "credentials",
      values: { ...state.values, clientId: "id", clientSecret: "secret" },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.dispatch({ type: "next" }));
    await waitFor(() => expect(result.current.recovery).toBe("settings"));
    expect(result.current.state.saved).toBe(false);
    expect(result.current.error).not.toContain("internal detail");
  });

  it("can retry a failed saved-configuration lookup", async () => {
    mocks.associations.mockRejectedValueOnce(new Error("Unavailable"));
    const { result } = mount("slack");
    await waitFor(() => expect(result.current.error).toContain("load saved"));
    expect(result.current.loading).toBe(true);
    act(() => result.current.retryLoad());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBeNull();
  });

  it("marks sign-in successful only after the backend verifies the connection", async () => {
    saved("slack", scopesFor(initialState("slack")));
    vi.mocked(window.open).mockReturnValue({
      closed: true,
      location: { href: "" },
      close: vi.fn(),
    } as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() => expect(result.current.state.authorized).toBe(true));
    expect(result.current.state.tested).toBe(false);
    expect(result.current.error).toBeNull();
    expect(mocks.probe).toHaveBeenCalled();
  });

  it("opens Slack sign-in on this server's first-party endpoint with the saved scopes", async () => {
    saved("slack", scopesFor(initialState("slack")));
    const popup = { closed: false, location: { href: "" }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", {
      step: "test",
    });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    expect(result.current.callbackUrl).toBe(
      "https://platform.example/callback",
    );
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    await waitFor(() =>
      expect(popup.location.href).toBe(
        "https://platform.example/mcp/demo-endpoint/connect/first-party",
      ),
    );
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() => expect(result.current.state.authorized).toBe(true));
    expect(mocks.updateClient).not.toHaveBeenCalled();
  });

  it("does not infer sign-in from returning to or closing the provider tab", async () => {
    saved("slack");
    mocks.probe.mockResolvedValue({
      ok: true,
      value: {
        status: "authorization_required",
        code: "authorization_required",
        message: "Sign in first.",
      },
    });
    const popup = { closed: true, location: { href: "" }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() =>
      expect(result.current.error).toContain("did not finish"),
    );
    expect(result.current.state.authorized).toBe(false);
    expect(result.current.state.tested).toBe(false);
  });

  it("lets the user cancel waiting for sign-in", async () => {
    saved("slack");
    const popup = { closed: false, location: { href: "" }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    expect(result.current.pending).toBe("signin");
    act(() => result.current.cancelSignIn());
    await waitFor(() => expect(result.current.pending).toBeNull());
    expect(popup.close).toHaveBeenCalled();
    expect(result.current.state.authorized).toBe(false);
    expect(result.current.state.tested).toBe(false);
  });

  it("refuses a write grant returned for a read-only configuration", async () => {
    const state = initialState("slack");
    state.values.access = "read";
    saved("slack", scopesFor(state));
    mocks.sessions
      .mockImplementationOnce(async () => pages([]))
      .mockImplementation(async () =>
        pages([
          {
            id: "broader-grant",
            userSessionIssuerId: "project-issuer",
            scopes: ["channels:read", "chat:write"],
          },
        ]),
      );
    const popup = { closed: false, location: { href: "" }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    await waitFor(() => expect(result.current.recovery).toBe("permissions"));
    expect(result.current.error).toContain("still granting write access");
    expect(mocks.revoke).toHaveBeenCalledWith({ id: "broader-grant" });
    expect(result.current.state.authorized).toBe(false);
    expect(result.current.state.tested).toBe(false);
  });
  it("does not revoke grants from a cancelled read-only sign-in check", async () => {
    const state = initialState("slack");
    state.values.access = "read";
    saved("slack", scopesFor(state));
    let finish!: (
      value: AsyncGenerator<{ result: { items: unknown[] } }>,
    ) => void;
    mocks.sessions
      .mockImplementationOnce(async () => pages([]))
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }),
      );
    const popup = { closed: false, location: { href: "" }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    await waitFor(() => expect(mocks.sessions).toHaveBeenCalledTimes(2));
    act(() => result.current.cancelSignIn());
    await act(async () =>
      finish(
        pages([
          {
            id: "new-grant",
            userSessionIssuerId: "project-issuer",
            scopes: ["chat:write"],
          },
        ]),
      ),
    );
    expect(mocks.revoke).not.toHaveBeenCalled();
    expect(result.current.state.authorized).toBe(false);
    expect(result.current.pending).toBeNull();
  });

  it("refuses a non-local HTTP sign-in URL before opening a popup", async () => {
    mocks.serverUrl = "http://platform.example";
    saved("slack", scopesFor(initialState("slack")));
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    expect(result.current.error).toContain("HTTPS");
    expect(window.open).not.toHaveBeenCalled();
  });
  it("allows HTTP only for the local development sign-in endpoint", async () => {
    mocks.serverUrl = "http://localhost:1234";
    saved("slack", scopesFor(initialState("slack")));
    const popup = { closed: false, location: { href: "" }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    await waitFor(() =>
      expect(popup.location.href).toBe(
        "http://localhost:1234/mcp/demo-endpoint/connect/first-party",
      ),
    );
  });
  it("loads canonically equivalent Slack issuer URLs", async () => {
    saved("slack", scopesFor(initialState("slack")));
    mocks.associations.mockResolvedValue({
      associations: [
        {
          issuerUrl: "HTTPS://MCP.SLACK.COM:443/",
          remoteSessionClientId: "client",
          serverCount: 1,
        },
      ],
    });
    const { result } = mount("slack");
    await waitFor(() => expect(result.current.state.saved).toBe(true));
  });
  it("does not enable a disabled server before credential saving succeeds", async () => {
    const disabled = {
      ...server,
      visibility: "disabled",
      userSessionIssuerId: "organization-issuer",
    };
    mocks.server.mockResolvedValue(disabled);
    mocks.issuer.mockImplementation(async ({ slug }) => {
      if (slug) throw { statusCode: 404 };
      return { id: "organization-issuer" };
    });
    mocks.createIssuer.mockResolvedValue({ id: "isolated" });
    mocks.commit.mockRejectedValue(new Error("Rejected"));
    const initial = initialState("slack");
    const { result } = mount("slack", {
      step: "credentials",
      values: { ...initial.values, clientId: "id", clientSecret: "secret" },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.dispatch({ type: "next" }));
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(mocks.updateServer).toHaveBeenCalledExactlyOnceWith({
      updateMcpServerForm: expect.objectContaining({
        visibility: "disabled",
        userSessionIssuerId: "isolated",
      }),
    });
    expect(result.current.state.saved).toBe(false);
  });

  it("saves replacement credentials after retrying a failed permissions update", async () => {
    saved("slack", scopesFor(initialState("slack")));
    const { result } = mount("slack", { step: "credentials" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.dispatch({ type: "connection", saved: false }));
    mocks.updateClient.mockRejectedValueOnce(new Error("Offline"));
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(result.current.error).toBeTruthy());
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "clientId",
        value: "replacement-id",
      }),
    );
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "clientSecret",
        value: "replacement-secret",
      }),
    );
    act(() => result.current.dispatch({ type: "next" }));
    await waitFor(() => expect(mocks.commit).toHaveBeenCalled());
    expect(mocks.commit).toHaveBeenCalledWith(
      expect.objectContaining({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          clientConfiguration: expect.objectContaining({
            clientId: "replacement-id",
            clientSecret: "replacement-secret",
          }),
        }),
      }),
    );
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    expect(result.current.state.step).toBe("test");
  });
  it("cancels a stale credential save when the rendered identity changes", async () => {
    let finish!: (value: { id: string }) => void;
    mocks.provider.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const state = initialState("slack");
    const hook = mount("slack", {
      step: "credentials",
      values: { ...state.values, clientId: "id", clientSecret: "secret" },
    });
    await waitFor(() => expect(hook.result.current.loading).toBe(false));
    act(() => hook.result.current.dispatch({ type: "next" }));
    await waitFor(() => expect(mocks.provider).toHaveBeenCalled());
    hook.rerender({
      mcpServer: { ...server, userSessionIssuerId: "replacement-issuer" },
    });
    await act(async () => finish({ id: "provider" }));
    await waitFor(() => expect(hook.result.current.pending).toBeNull());
    expect(mocks.commit).not.toHaveBeenCalled();
    expect(hook.result.current.state.saved).toBe(false);
    expect(hook.result.current.state.step).toBe("credentials");
  });
  it("detects identity changes made by another editor before committing", async () => {
    mocks.server.mockResolvedValueOnce(server).mockResolvedValue({
      ...server,
      userSessionIssuerId: "replacement-issuer",
    });
    const state = initialState("slack");
    const { result } = mount("slack", {
      step: "credentials",
      values: { ...state.values, clientId: "id", clientSecret: "secret" },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.dispatch({ type: "next" }));
    await waitFor(() => expect(result.current.recovery).toBe("settings"));
    expect(mocks.commit).not.toHaveBeenCalled();
    expect(result.current.state.saved).toBe(false);
  });

  it("revokes every user's grant on this issuer when permissions change", async () => {
    saved("slack", scopesFor(initialState("slack")));
    mocks.sessions.mockImplementation(async () =>
      pages([
        { id: "current-user", userSessionIssuerId: "project-issuer" },
        { id: "other-user", userSessionIssuerId: "project-issuer" },
        { id: "other-issuer", userSessionIssuerId: "another-issuer" },
      ]),
    );
    const { result } = mount("slack", { step: "app" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(mocks.updateClient).toHaveBeenCalled());
    expect(mocks.sessions).toHaveBeenCalledWith({
      remoteSessionClientId: "client",
    });
    expect(mocks.revoke.mock.calls).toEqual([
      [{ id: "current-user" }],
      [{ id: "other-user" }],
    ]);
  });
  it("invalidates the stored grant even while replacement credentials are being edited", async () => {
    saved("slack", scopesFor(initialState("slack")));
    mocks.sessions.mockImplementation(async () =>
      pages([{ id: "old-grant", userSessionIssuerId: "project-issuer" }]),
    );
    const { result } = mount("slack", { step: "app" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.dispatch({
        type: "connection",
        saved: false,
        authorized: false,
        tested: false,
      }),
    );
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() =>
      expect(mocks.revoke).toHaveBeenCalledWith({ id: "old-grant" }),
    );
    expect(result.current.state.saved).toBe(false);
  });
  it("rechecks whether the app became shared before changing its permissions", async () => {
    saved("slack", scopesFor(initialState("slack")));
    const { result } = mount("slack", { step: "app" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    mocks.associations.mockResolvedValue({
      associations: [
        {
          issuerUrl: "https://mcp.slack.com",
          remoteSessionClientId: "client",
          serverCount: 2,
        },
      ],
    });
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(result.current.recovery).toBe("settings"));
    expect(mocks.revoke).not.toHaveBeenCalled();
    expect(mocks.updateClient).not.toHaveBeenCalled();
  });
  it("clears successful verification when the server identity changes", async () => {
    saved("slack", scopesFor(initialState("slack")));
    const hook = mount("slack", { step: "test" });
    await waitFor(() => expect(hook.result.current.state.saved).toBe(true));
    act(() => hook.result.current.dispatch({ type: "modal", modal: "test" }));
    await waitFor(() => expect(hook.result.current.state.tested).toBe(true));
    mocks.associations.mockResolvedValue({ associations: [] });
    hook.rerender({
      mcpServer: { ...server, userSessionIssuerId: "replacement-issuer" },
    });
    await waitFor(() => expect(hook.result.current.loading).toBe(false));
    expect(hook.result.current.state.saved).toBe(false);
    expect(hook.result.current.state.authorized).toBe(false);
    expect(hook.result.current.state.tested).toBe(false);
    expect(hook.result.current.toolCount).toBeNull();
  });

  it("invalidates the old Slack grant as soon as permissions change, before updating scopes", async () => {
    saved("slack", scopesFor(initialState("slack")));
    mocks.sessions.mockImplementation(async () =>
      pages([
        { id: "own-grant", userSessionIssuerId: "project-issuer" },
        { id: "other-issuer", userSessionIssuerId: "elsewhere" },
      ]),
    );
    const { result } = mount("slack", {
      step: "app",
      authorized: true,
      tested: true,
    });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(mocks.updateClient).toHaveBeenCalled());
    expect(mocks.revoke).toHaveBeenCalledExactlyOnceWith({ id: "own-grant" });
    expect(mocks.revoke.mock.invocationCallOrder[0]).toBeLessThan(
      mocks.updateClient.mock.invocationCallOrder[0]!,
    );
    expect(result.current.state.authorized).toBe(false);
    expect(result.current.state.tested).toBe(false);
  });

  it("cannot verify an old grant while a Slack permission update is in flight", async () => {
    saved("slack", scopesFor(initialState("slack")));
    let finish!: () => void;
    mocks.updateClient.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    const popup = { closed: false, location: { href: "" }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(mocks.updateClient).toHaveBeenCalled());
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    expect(mocks.probe).not.toHaveBeenCalled();
    expect(result.current.state.authorized).toBe(false);
    await act(async () => finish());
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    await waitFor(() =>
      expect(popup.location.href).toContain("/connect/first-party"),
    );
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() => expect(result.current.state.authorized).toBe(true));
    expect(mocks.updateClient).toHaveBeenCalledTimes(1);
  });

  it("waits for permission repair before verifying a retried Slack sign-in", async () => {
    saved("slack", scopesFor(initialState("slack")));
    mocks.updateClient.mockRejectedValueOnce(new Error("Unavailable"));
    const popup = { closed: false, location: { href: "" }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(result.current.error).toContain("permissions"));
    let finish!: () => void;
    mocks.updateClient.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    await waitFor(() => expect(mocks.updateClient).toHaveBeenCalledTimes(2));
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    expect(result.current.pending).toBe("signin");
    expect(popup.location.href).toBe("");
    expect(mocks.probe).not.toHaveBeenCalled();
    await act(async () => finish());
    await waitFor(() => expect(result.current.state.authorized).toBe(true));
    expect(result.current.state.tested).toBe(false);
  });

  it("keeps permission repair locked after cancelling sign-in until the mutation settles", async () => {
    saved("slack", scopesFor(initialState("slack")));
    mocks.updateClient.mockRejectedValueOnce(new Error("Unavailable"));
    const popup = { closed: false, location: { href: "" }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(result.current.error).toContain("permissions"));
    let finish!: () => void;
    mocks.updateClient.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    await waitFor(() => expect(mocks.updateClient).toHaveBeenCalledTimes(2));
    act(() => result.current.cancelSignIn());
    expect(result.current.pending).toBe("save");
    act(() =>
      result.current.dispatch({ type: "field", field: "access", value: "all" }),
    );
    expect(result.current.state.values.access).toBe("read");
    await act(async () => finish());
    await waitFor(() => expect(result.current.pending).toBeNull());
    expect(popup.location.href).toBe("");
    expect(result.current.state.authorized).toBe(false);
    expect(mocks.probe).not.toHaveBeenCalled();
  });

  it("blocks progression after failed Slack grant invalidation and retries on Continue", async () => {
    saved("slack", scopesFor(initialState("slack")));
    mocks.sessions.mockImplementation(async () =>
      pages([{ id: "own-grant", userSessionIssuerId: "project-issuer" }]),
    );
    mocks.revoke.mockRejectedValueOnce(new Error("Unavailable"));
    const { result } = mount("slack", { step: "app" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(result.current.error).toContain("permissions"));
    expect(mocks.updateClient).not.toHaveBeenCalled();
    expect(result.current.state.step).toBe("app");
    act(() => result.current.dispatch({ type: "next" }));
    await waitFor(() => expect(result.current.state.step).toBe("credentials"));
    expect(mocks.revoke).toHaveBeenCalledTimes(2);
    expect(mocks.updateClient).toHaveBeenCalledTimes(1);
  });

  it("updates Slack permissions and revokes grants on only the target issuer before reauthorization", async () => {
    const all = scopesFor(initialState("slack"));
    saved("slack", all);
    mocks.sessions.mockImplementation(async () =>
      pages([
        { id: "own-grant", userSessionIssuerId: "project-issuer" },
        { id: "other-issuer", userSessionIssuerId: "other-issuer" },
      ]),
    );
    const popup = { closed: false, location: { href: "" }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(result.current.pending).toBeNull());
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    await waitFor(() =>
      expect(popup.location.href).toContain("/connect/first-party"),
    );
    expect(mocks.updateClient).toHaveBeenCalledWith({
      updateRemoteSessionClientForm: {
        id: "client",
        scope: all.filter((scope) => !scope.includes(":write")),
      },
    });
    expect(mocks.sessions).toHaveBeenCalledWith({
      remoteSessionClientId: "client",
    });
    expect(mocks.revoke).toHaveBeenCalledExactlyOnceWith({ id: "own-grant" });
    expect(result.current.state.tested).toBe(false);
  });

  it("ignores a connection verification that finishes after cancelling sign-in", async () => {
    saved("slack", scopesFor(initialState("slack")));
    let finish!: (value: unknown) => void;
    mocks.probe.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const popup = { closed: false, location: { href: "" }, close: vi.fn() };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    await waitFor(() => expect(mocks.probe).toHaveBeenCalled());
    act(() => result.current.cancelSignIn());
    await act(async () =>
      finish({ ok: true, value: { status: "ready", toolCount: 2 } }),
    );
    expect(result.current.pending).toBeNull();
    expect(result.current.state.authorized).toBe(false);
    expect(result.current.state.tested).toBe(false);
  });

  it("refuses changing a shared Slack app connection without altering the permission selection", async () => {
    saved("slack", scopesFor(initialState("slack")));
    mocks.associations.mockResolvedValue({
      associations: [
        {
          issuerUrl: "https://mcp.slack.com",
          remoteSessionClientId: "client",
          serverCount: 2,
        },
      ],
    });
    const { result } = mount("slack", { step: "app" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.dispatch({
        type: "field",
        field: "access",
        value: "read",
      }),
    );
    expect(result.current.recovery).toBe("settings");
    expect(result.current.state.values.access).toBe("all");
    expect(mocks.updateClient).not.toHaveBeenCalled();
    expect(mocks.revoke).not.toHaveBeenCalled();
  });

  it("keeps a failed backend sign-in verification in the sign-in step", async () => {
    saved("slack");
    mocks.probe.mockResolvedValue({
      ok: true,
      value: {
        status: "failed",
        code: "provider_unavailable",
        message: "Provider unavailable. Retry.",
      },
    });
    vi.mocked(window.open).mockReturnValue({
      closed: false,
      location: { href: "" },
      close: vi.fn(),
    } as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() => expect(result.current.pending).toBeNull());
    expect(result.current.errorStep).toBe("signin");
    expect(result.current.error).toBe(
      "Couldn’t verify Slack sign-in. Try again.",
    );
    expect(result.current.state.authorized).toBe(false);
  });

  it("places a blocked-popup error on sign-in", async () => {
    saved("slack");
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.dispatch({ type: "modal", modal: "signin" }));
    expect(result.current.errorStep).toBe("signin");
    expect(result.current.error).toContain("Allow pop-ups");
    expect(result.current.state.authorized).toBe(false);
  });

  it("uses the server test API with the exact target and dashboard session", async () => {
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.dispatch({ type: "modal", modal: "test" }));
    await waitFor(() => expect(result.current.state.tested).toBe(true));
    expect(mocks.probe).toHaveBeenCalledWith(
      expect.anything(),
      { testServerConnectionRequestBody: { mcpServerId: server.id } },
      {
        projectSlugHeaderGramProject: "example-project",
        sessionHeaderGramSession: "dashboard-session",
      },
      { timeoutMs: 30000 },
    );
    expect(result.current.toolCount).toBe(2);
  });

  it.each(["authorization_required", "configuration_required", "failed"])(
    "keeps %s tests incomplete with a client-owned recovery message",
    async (status) => {
      mocks.probe.mockResolvedValue({
        ok: true,
        value: {
          status,
          code: "provider_unavailable",
          message: "Try signing in again.",
        },
      });
      const { result } = mount("slack", { step: "test", authorized: true });
      await waitFor(() => expect(result.current.loading).toBe(false));
      act(() => result.current.dispatch({ type: "modal", modal: "test" }));
      await waitFor(() =>
        expect(result.current.error).toBe(
          status === "authorization_required"
            ? "Sign in to Slack, then test the connection again."
            : status === "configuration_required"
              ? "Review this server’s User Identity configuration before continuing."
              : "Couldn’t test the Slack connection. Review app permissions and try again.",
        ),
      );
      expect(result.current.state.tested).toBe(false);
      expect(result.current.toolCount).toBeNull();
      expect(result.current.errorStep).toBe("test");
      if (status === "authorization_required")
        expect(result.current.state.authorized).toBe(false);
    },
  );

  it("recovers from a network failure without showing raw errors or retaining a previous pass", async () => {
    mocks.probe.mockResolvedValueOnce({
      ok: false,
      error: new Error("sensitive upstream details"),
    });
    const { result } = mount("slack", {
      step: "test",
      authorized: true,
      tested: true,
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.dispatch({ type: "modal", modal: "test" }));
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(result.current.error).not.toContain("sensitive");
    expect(result.current.state.tested).toBe(false);
    await waitFor(() => expect(result.current.pending).toBeNull());
    act(() => result.current.dispatch({ type: "modal", modal: "test" }));
    await waitFor(() => expect(result.current.state.tested).toBe(true));
    expect(result.current.error).toBeNull();
    expect(result.current.toolCount).toBe(2);
  });

  it("does not treat a malformed ready result as a pass", async () => {
    mocks.probe.mockResolvedValue({
      ok: true,
      value: { status: "ready", code: "ready", message: "" },
    });
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.dispatch({ type: "modal", modal: "test" }));
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(result.current.state.tested).toBe(false);
  });
});
