import { loadProviderProgress, saveProviderProgress } from "./provider-setup";
import { queryKeyRemoteSessionClient } from "@gram/client/react-query/remoteSessionClient";
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
  remote: vi.fn(),
  updateRemote: vi.fn(),
  headers: vi.fn(),
  createHeader: vi.fn(),
  updateHeader: vi.fn(),
  deleteHeader: vi.fn(),
  linkedServers: vi.fn(),
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
    mcpServers: {
      get: mocks.server,
      update: mocks.updateServer,
      list: mocks.linkedServers,
    },
    remoteMcp: {
      getServer: mocks.remote,
      updateServer: mocks.updateRemote,
      listServerHeaders: mocks.headers,
      createServerHeader: mocks.createHeader,
      updateServerHeader: mocks.updateHeader,
      deleteServerHeader: mocks.deleteHeader,
    },
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
function mount(
  provider: State["provider"],
  override: Partial<State> = {},
  source = {
    url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
    instanceUrl: "",
  },
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  type HookProps = { mcpServer: McpServer; currentSource?: typeof source };
  const initialProps: HookProps = { mcpServer: server };
  return renderHook(
    ({ mcpServer, currentSource = source }: HookProps) => {
      const [state, dispatch] = useReducer(reduce, {
        ...initialState(provider),
        ...override,
      });
      const connection = useProviderConnection(
        state,
        dispatch,
        mcpServer,
        provider === "salesforce" || provider === "snowflake"
          ? currentSource
          : undefined,
      );
      return { state, ...connection };
    },
    {
      initialProps,
      wrapper: ({ children }: PropsWithChildren) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    },
  );
}
function saved(provider: "slack" | "github", scopes: string[] = []) {
  mocks.associations.mockResolvedValue({
    associations: [
      {
        issuerUrl:
          provider === "github"
            ? "https://github.com/login/oauth"
            : "https://mcp.slack.com",
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
  mocks.remote.mockResolvedValue({
    id: "upstream",
    url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
  });
  mocks.headers.mockResolvedValue({ headers: [] });
  mocks.linkedServers.mockResolvedValue({ mcpServers: [server] });
  mocks.updateRemote.mockImplementation(async ({ updateServerForm }) => {
    const value = { id: "upstream", ...updateServerForm };
    mocks.remote.mockResolvedValue(value);
    return value;
  });
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
  it.each(["slack", "github"] as const)(
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
      act(() => result.current.next());
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
    act(() => result.current.next());
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
    act(() => result.current.next());
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(result.current.errorStep).toBe("save");
    expect(result.current.state.saved).toBe(false);
    expect(result.current.state.step).toBe("credentials");
    expect(result.current.state.values.clientSecret).toBe("secret");
    mocks.commit.mockResolvedValue({ status: "registered" });
    act(() => result.current.next());
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
    act(() => result.current.next());
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
      focus: vi.fn(),
      close: vi.fn(),
    } as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.signIn());
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
    const popup = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", {
      step: "test",
    });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    expect(result.current.callbackUrl).toBe(
      "https://platform.example/callback",
    );
    act(() => result.current.signIn());
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
    const popup = {
      closed: true,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.signIn());
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
    const popup = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.signIn());
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
    const popup = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.signIn());
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
    const popup = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.signIn());
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
    act(() => result.current.signIn());
    expect(result.current.error).toContain("HTTPS");
    expect(window.open).not.toHaveBeenCalled();
  });
  it("allows HTTP only for the local development sign-in endpoint", async () => {
    mocks.serverUrl = "http://localhost:1234";
    saved("slack", scopesFor(initialState("slack")));
    const popup = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.signIn());
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
    act(() => result.current.next());
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
      result.current.changeField({
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(result.current.error).toBeTruthy());
    act(() =>
      result.current.changeField({
        field: "clientId",
        value: "replacement-id",
      }),
    );
    act(() =>
      result.current.changeField({
        field: "clientSecret",
        value: "replacement-secret",
      }),
    );
    act(() => result.current.next());
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
    act(() => hook.result.current.next());
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
    act(() => result.current.next());
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
      result.current.changeField({
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
      result.current.changeField({
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
      result.current.changeField({
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
    act(() => hook.result.current.testConnection());
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
      result.current.changeField({
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
    const popup = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.changeField({
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(mocks.updateClient).toHaveBeenCalled());
    act(() => result.current.signIn());
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    expect(mocks.probe).not.toHaveBeenCalled();
    expect(result.current.state.authorized).toBe(false);
    await act(async () => finish());
    act(() => result.current.signIn());
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
    const popup = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.changeField({
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
    act(() => result.current.signIn());
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
    const popup = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.changeField({
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
    act(() => result.current.signIn());
    await waitFor(() => expect(mocks.updateClient).toHaveBeenCalledTimes(2));
    act(() => result.current.cancelSignIn());
    expect(result.current.pending).toBe("save");
    act(() => result.current.changeField({ field: "access", value: "all" }));
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
      result.current.changeField({
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(result.current.error).toContain("permissions"));
    expect(mocks.updateClient).not.toHaveBeenCalled();
    expect(result.current.state.step).toBe("app");
    act(() => result.current.next());
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
    const popup = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() =>
      result.current.changeField({
        field: "access",
        value: "read",
      }),
    );
    await waitFor(() => expect(result.current.pending).toBeNull());
    act(() => result.current.signIn());
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
    const popup = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.signIn());
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
      result.current.changeField({
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
      focus: vi.fn(),
      close: vi.fn(),
    } as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.signIn());
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
    act(() => result.current.signIn());
    expect(result.current.errorStep).toBe("signin");
    expect(result.current.error).toContain("Allow pop-ups");
    expect(result.current.state.authorized).toBe(false);
  });

  it("uses the server test API with the exact target and dashboard session", async () => {
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.testConnection());
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
      act(() => result.current.testConnection());
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
    act(() => result.current.testConnection());
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(result.current.error).not.toContain("sensitive");
    expect(result.current.state.tested).toBe(false);
    await waitFor(() => expect(result.current.pending).toBeNull());
    act(() => result.current.testConnection());
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
    act(() => result.current.testConnection());
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(result.current.state.tested).toBe(false);
  });
});

it("invalidates verification when Settings updates the canonical client cache", async () => {
  saved("slack", scopesFor(initialState("slack")));
  const { result } = mount("slack", { step: "test" });
  await waitFor(() => expect(result.current.loading).toBe(false));
  act(() => result.current.testConnection());
  await waitFor(() => expect(result.current.state.tested).toBe(true));
  const cache = clients.at(-1)!;
  const key = queryKeyRemoteSessionClient({ id: "client" });
  expect(cache.getQueryData(key)).toBeDefined();
  await act(async () => {
    cache.setQueryData(key, {
      ...cache.getQueryData<object>(key),
      id: "client",
      scope: scopesFor({ values: { access: "read" } }),
      updatedAt: new Date(),
    });
  });
  await waitFor(() => expect(result.current.state.tested).toBe(false));
  expect(result.current.state.authorized).toBe(false);
});

it("starts a fresh configuration read after an identity change even if the previous read is pending", async () => {
  let finishOld!: (value: {
    associations: {
      issuerUrl: string;
      remoteSessionClientId: string;
      serverCount: number;
    }[];
  }) => void;
  mocks.associations.mockReturnValueOnce(
    new Promise((resolve) => {
      finishOld = resolve;
    }),
  );
  mocks.associations.mockResolvedValue({ associations: [] });
  const hook = mount("slack");
  await waitFor(() => expect(mocks.associations).toHaveBeenCalledTimes(1));
  hook.rerender({
    mcpServer: { ...server, userSessionIssuerId: "replacement-issuer" },
  });
  await waitFor(() => expect(mocks.associations).toHaveBeenCalledTimes(2));
  await act(async () =>
    finishOld({
      associations: [
        {
          issuerUrl: "https://mcp.slack.com",
          remoteSessionClientId: "old-client",
          serverCount: 1,
        },
      ],
    }),
  );
  await waitFor(() => expect(hook.result.current.loading).toBe(false));
  expect(hook.result.current.state.saved).toBe(false);
  expect(mocks.client).not.toHaveBeenCalled();
});

it.each(["network", "diagnostic"] as const)(
  "restarts sign-in after a %s failure while the previous tab is still open",
  async (failure) => {
    saved("slack", scopesFor(initialState("slack")));
    mocks.probe.mockResolvedValueOnce(
      failure === "network"
        ? { ok: false, error: new Error("Connection interrupted") }
        : {
            ok: true,
            value: {
              status: "failed",
              code: "provider_unavailable",
              message: "",
            },
          },
    );
    const previous = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    const replacement = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open)
      .mockReturnValueOnce(previous as unknown as Window)
      .mockReturnValueOnce(replacement as unknown as Window);
    const { result } = mount("slack", { step: "test" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.signIn());
    await waitFor(() => expect(result.current.error).toContain("Try again"));
    expect(result.current.pending).toBeNull();
    expect(previous.close).not.toHaveBeenCalled();
    const readsBeforeRetry = mocks.associations.mock.calls.length;

    act(() => result.current.signIn());

    expect(previous.close).toHaveBeenCalledTimes(1);
    expect(window.open).toHaveBeenCalledTimes(2);
    await waitFor(() =>
      expect(replacement.location.href).toBe(
        "https://platform.example/mcp/demo-endpoint/connect/first-party",
      ),
    );
    expect(mocks.associations.mock.calls.length).toBeGreaterThan(
      readsBeforeRetry,
    );
    await waitFor(() => expect(result.current.state.authorized).toBe(true));
    expect(result.current.state.tested).toBe(false);
    expect(result.current.error).toBeNull();
  },
);

describe("GitHub real setup actions", () => {
  it.each(["new", "existing"])(
    "saves either app path with GitHub metadata (%s)",
    async (appChoice) => {
      mocks.provider.mockRejectedValue({ statusCode: 404 });
      const s = initialState("github");
      const { result } = mount("github", {
        step: "credentials",
        values: {
          ...s.values,
          org: "example-org",
          appChoice,
          clientId: "app-id",
          clientSecret: "secret",
        },
      });
      await waitFor(() => expect(result.current.loading).toBe(false));
      act(() => result.current.next());
      await waitFor(() => expect(result.current.state.saved).toBe(true));
      expect(mocks.commit).toHaveBeenCalledWith({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          targetServerOnly: true,
          createProvider: expect.objectContaining({
            issuer: "https://github.com/login/oauth",
            authorizationEndpoint: "https://github.com/login/oauth/authorize",
            tokenEndpoint: "https://github.com/login/oauth/access_token",
            authorizationResponseIssParameterSupported: true,
          }),
          clientConfiguration: expect.objectContaining({
            clientId: "app-id",
            clientSecret: "secret",
            scope: [],
          }),
        }),
      });
    },
  );
  it("verifies first-party sign-in separately from testing and avoids Slack scope mutations", async () => {
    saved("github", ["offline_access"]);
    const popup = {
      closed: false,
      location: { href: "" },
      focus: vi.fn(),
      close: vi.fn(),
    };
    vi.mocked(window.open).mockReturnValue(popup as unknown as Window);
    const s = initialState("github");
    const { result } = mount("github", {
      step: "test",
      values: { ...s.values, org: "example-org" },
    });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    act(() => result.current.signIn());
    await waitFor(() =>
      expect(popup.location.href).toBe(
        "https://platform.example/mcp/demo-endpoint/connect/first-party",
      ),
    );
    await waitFor(() => expect(result.current.state.authorized).toBe(true));
    expect(result.current.state.tested).toBe(false);
    act(() => result.current.testConnection());
    await waitFor(() => expect(result.current.state.tested).toBe(true));
    expect(result.current.toolCount).toBe(2);
    expect(mocks.updateClient).not.toHaveBeenCalled();
    expect(mocks.revoke).not.toHaveBeenCalled();
    expect(mocks.sessions).not.toHaveBeenCalled();
    act(() =>
      result.current.changeField({
        field: "appChoice",
        value: "existing",
      }),
    );
    expect([
      result.current.state.saved,
      result.current.state.authorized,
      result.current.state.tested,
    ]).toEqual([false, false, false]);
    expect(result.current.toolCount).toBeNull();
  });
});

describe("Slack permission choice hydration", () => {
  it("preserves a retained read-only choice after a failed scope update and retries it after reopening", async () => {
    const key = "failed-read-only-choice";
    const allScopes = scopesFor(initialState("slack"));
    const readScopes = scopesFor({ values: { access: "read" } });
    saved("slack", allScopes);
    mocks.updateClient.mockRejectedValueOnce(new Error("Scope update failed"));
    const first = mount("slack", { step: "app" });
    await waitFor(() => expect(first.result.current.state.saved).toBe(true));
    act(() =>
      first.result.current.changeField({ field: "access", value: "read" }),
    );
    await waitFor(() =>
      expect(first.result.current.error).toContain("permissions"),
    );
    saveProviderProgress(key, first.result.current.state);
    first.unmount();

    const reopened = mount("slack", loadProviderProgress("slack", key));
    await waitFor(() => expect(reopened.result.current.state.saved).toBe(true));
    expect(reopened.result.current.state.values.access).toBe("read");
    expect(reopened.result.current.state.configChanged).toBe(true);
    expect(reopened.result.current.state.authorized).toBe(false);
    expect(reopened.result.current.state.tested).toBe(false);
    expect(mocks.updateClient).toHaveBeenCalledTimes(1);

    act(() => reopened.result.current.next());
    await waitFor(() => expect(mocks.updateClient).toHaveBeenCalledTimes(2));
    expect(mocks.updateClient).toHaveBeenLastCalledWith({
      updateRemoteSessionClientForm: { id: "client", scope: readScopes },
    });
    await waitFor(() =>
      expect(reopened.result.current.state.step).toBe("credentials"),
    );
    expect(reopened.result.current.state.values.access).toBe("read");
    sessionStorage.removeItem(key);
  });

  it("keeps an access choice made while saved configuration is loading", async () => {
    saved("slack", scopesFor(initialState("slack")));
    let finish!: (client: { id: string; scope: string[] }) => void;
    mocks.client.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    const { result } = mount("slack", { step: "app" });
    await waitFor(() => expect(mocks.client).toHaveBeenCalled());
    act(() => result.current.changeField({ field: "access", value: "read" }));
    await act(async () =>
      finish({ id: "client", scope: scopesFor(initialState("slack")) }),
    );
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    expect(result.current.state.values.access).toBe("read");
    expect(mocks.updateClient).not.toHaveBeenCalled();
  });

  it("does not mark matching retained permissions as changed", async () => {
    const key = "matching-read-only-choice";
    const initial = initialState("slack");
    saved("slack", scopesFor({ values: { access: "read" } }));
    saveProviderProgress(key, {
      ...initial,
      accessChosen: true,
      values: { ...initial.values, access: "read" },
    });
    const { result } = mount("slack", loadProviderProgress("slack", key));
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    expect(result.current.state.values.access).toBe("read");
    expect(result.current.state.accessChosen).toBe(true);
    expect(result.current.state.configChanged).toBe(false);
    expect(mocks.updateClient).not.toHaveBeenCalled();
    sessionStorage.removeItem(key);
  });

  it("hydrates backend permissions when there is no retained or edited access choice", async () => {
    saved("slack", scopesFor({ values: { access: "read" } }));
    const { result } = mount("slack", { step: "app" });
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    expect(result.current.state.values.access).toBe("read");
    expect(result.current.state.configChanged).toBe(false);
    expect(result.current.state.accessChosen).toBe(false);
    expect(mocks.updateClient).not.toHaveBeenCalled();
  });
});

describe("Salesforce shared actions", () => {
  it("hydrates externally selected My Domain endpoints and clears the domain when returning to a standard endpoint", async () => {
    const source = {
      url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
      instanceUrl: "",
    };
    const mounted = mount("salesforce", {}, source);
    await waitFor(() => expect(mounted.result.current.loading).toBe(false));
    mounted.rerender({
      mcpServer: server,
      currentSource: {
        ...source,
        url: "https://api.salesforce.com/platform/mcp/v1/d/example--uat/sandbox/platform/sobject-all",
      },
    });
    await waitFor(() =>
      expect(mounted.result.current.state.values.salesforceDomain).toBe(
        "https://example--uat.sandbox.my.salesforce.com",
      ),
    );
    expect(mounted.result.current.state.values.environment).toBe("sandbox");
    expect(mounted.result.current.state.values.salesforceServer).toBe(
      "sobject-all",
    );
    mounted.rerender({ mcpServer: server, currentSource: source });
    await waitFor(() =>
      expect(mounted.result.current.state.values.salesforceDomain).toBe(""),
    );
    expect(mounted.result.current.state.values.environment).toBe("production");
    expect(mounted.result.current.state.saved).toBe(false);
  });
  it.each([
    [
      "production",
      "sobject-mutations",
      "platform/sobject-mutations",
      "https://login.salesforce.com",
      "",
    ],
    [
      "sandbox",
      "data360",
      "data/sandbox/data360",
      "https://test.salesforce.com",
      "",
    ],
    [
      "sandbox",
      "sobject-reads",
      "d/example--uat/sandbox/platform/sobject-reads",
      "https://example--uat.sandbox.my.salesforce.com",
      "example--uat.sandbox.my.salesforce.com",
    ],
    [
      "production",
      "sobject-reads",
      "d/example-dev-ed/develop/platform/sobject-reads",
      "https://example-dev-ed.develop.my.salesforce.com",
      "https://example-dev-ed.develop.my.salesforce-setup.com/",
    ],
  ])(
    "registers the %s OAuth provider and saves a native public client",
    async (environment, salesforceServer, path, issuer, salesforceDomain) => {
      mocks.provider.mockRejectedValue({ statusCode: 404 });
      const s = initialState("salesforce");
      const { result } = mount("salesforce", {
        step: "credentials",
        values: {
          ...s.values,
          environment,
          salesforceServer,
          salesforceDomain,
          clientId: "key",
        },
      });
      await waitFor(() => expect(result.current.loading).toBe(false));
      act(() => result.current.next());
      await waitFor(() => expect(result.current.state.saved).toBe(true));
      expect(mocks.updateRemote).toHaveBeenCalledWith({
        updateServerForm: {
          id: "upstream",
          url: `https://api.salesforce.com/platform/mcp/v1/${path}`,
        },
      });
      expect(mocks.commit).toHaveBeenCalledWith({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          mcpServerId: server.id,
          targetServerOnly: true,
          clientMode: "manual",
          createProvider: expect.objectContaining({
            issuer,
            authorizationEndpoint: `${issuer}/services/oauth2/authorize`,
            tokenEndpoint: `${issuer}/services/oauth2/token`,
          }),
          clientConfiguration: {
            clientId: "key",
            clientSecret: undefined,
            tokenEndpointAuthMethod: "none",
            scope: ["mcp_api", "refresh_token"],
          },
        }),
      });
      expect(result.current.state.authorized).toBe(false);
      expect(result.current.state.tested).toBe(false);
    },
  );
  it.each([
    { integration: "hosted", instanceUrl: "https://example.my.salesforce.com" },
  ])(
    "refuses an undocumented retained Salesforce choice without writes: %j",
    async (values) => {
      const initial = initialState("salesforce");
      const { result } = mount("salesforce", {
        step: "credentials",
        values: { ...initial.values, ...values, clientId: "key" },
      });
      await waitFor(() => expect(result.current.loading).toBe(false));
      act(() => result.current.next());
      await waitFor(() =>
        expect(result.current.state.errors.salesforceServer).toBeTruthy(),
      );
      expect(result.current.state.saved).toBe(false);
      expect(mocks.updateRemote).not.toHaveBeenCalled();
      expect(mocks.createHeader).not.toHaveBeenCalled();
      expect(mocks.commit).not.toHaveBeenCalled();
    },
  );
  it("does not save credentials or mutate the source on the documented support handoff path", async () => {
    const initial = initialState("salesforce");
    const { result } = mount("salesforce", {
      step: "credentials",
      values: { ...initial.values, path: "support", clientId: "key" },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.next());
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(result.current.state.saved).toBe(false);
    expect(mocks.updateRemote).not.toHaveBeenCalled();
    expect(mocks.commit).not.toHaveBeenCalled();
  });
  it("does not bind credentials after a configuration write fails and supports retry", async () => {
    mocks.updateRemote.mockRejectedValueOnce(new Error("unavailable"));
    const s = initialState("salesforce");
    const { result } = mount("salesforce", {
      step: "credentials",
      values: { ...s.values, environment: "sandbox", clientId: "key" },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.next());
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(mocks.commit).not.toHaveBeenCalled();
    expect(result.current.state.saved).toBe(false);
    act(() => result.current.next());
    await waitFor(() => expect(result.current.state.saved).toBe(true));
  });
  it("refuses an Authorization header and shared source changes", async () => {
    mocks.headers.mockResolvedValue({
      headers: [{ id: "auth", name: "authorization", isSecret: true }],
    });
    const s = initialState("salesforce");
    const { result } = mount("salesforce", {
      step: "credentials",
      values: { ...s.values, environment: "sandbox", clientId: "key" },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.next());
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(mocks.commit).not.toHaveBeenCalled();
    expect(mocks.updateRemote).not.toHaveBeenCalled();
    expect(result.current.recovery).toBe("settings");
  });
});

describe("Salesforce target changes", () => {
  it("rejects a rebound wrapper before changing its old source", async () => {
    mocks.server.mockResolvedValue({
      ...server,
      remoteMcpServerId: "another-upstream",
    });
    const s = initialState("salesforce");
    const { result } = mount("salesforce", {
      step: "credentials",
      values: { ...s.values, environment: "sandbox", clientId: "key" },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.next());
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(mocks.updateRemote).not.toHaveBeenCalled();
    expect(mocks.commit).not.toHaveBeenCalled();
  });
  it("uses distinct provider slugs for production and sandbox", async () => {
    const slugs: string[] = [];
    for (const values of [
      { environment: "production" },
      { environment: "sandbox" },
    ]) {
      mocks.remote.mockResolvedValue({
        id: "upstream",
        url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
      });
      mocks.provider.mockRejectedValue({ statusCode: 404 });
      const s = initialState("salesforce");
      const mounted = mount("salesforce", {
        step: "credentials",
        values: { ...s.values, ...values, clientId: "key" },
      });
      await waitFor(() => expect(mounted.result.current.loading).toBe(false));
      act(() => mounted.result.current.next());
      await waitFor(() =>
        expect(mounted.result.current.state.saved).toBe(true),
      );
      slugs.push(
        mocks.commit.mock.lastCall![0].commitServerIdentityConfigurationForm
          .createProvider.slug,
      );
      mounted.unmount();
    }
    expect(new Set(slugs).size).toBe(2);
  });
  it("restores the original source after a failed credential commit and keeps the pending draft", async () => {
    mocks.associations.mockResolvedValue({
      associations: [
        {
          issuerUrl: "https://login.salesforce.com",
          remoteSessionClientId: "old",
          serverCount: 1,
        },
      ],
    });
    mocks.client.mockResolvedValue({
      id: "old",
      clientId: "old-key",
      callbackUrl: "https://platform.example/callback",
    });
    mocks.commit.mockRejectedValue(new Error("save failed"));
    const s = initialState("salesforce");
    const source = {
      url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
      instanceUrl: "",
    };
    const mounted = mount(
      "salesforce",
      {
        step: "credentials",
        values: {
          ...s.values,
          salesforceServer: "sobject-all",
          clientId: "replacement-key",
        },
      },
      source,
    );
    await waitFor(() => expect(mounted.result.current.loading).toBe(false));
    act(() => mounted.result.current.next());
    await waitFor(() => expect(mounted.result.current.error).toBeTruthy());
    expect((await mocks.remote()).url).toBe(source.url);
    expect(mocks.updateRemote).toHaveBeenLastCalledWith({
      updateServerForm: { id: "upstream", url: source.url },
    });
    mounted.rerender({ mcpServer: server, currentSource: source });
    await waitFor(() => expect(mounted.result.current.loading).toBe(false));
    expect(mounted.result.current.state.saved).toBe(false);
    expect(mounted.result.current.state.configChanged).toBe(true);
  });
});

describe("Salesforce source safeguards", () => {
  it("refuses to change a source shared with another MCP server", async () => {
    mocks.linkedServers.mockResolvedValue({
      mcpServers: [server, { ...server, id: "other-server" }],
    });
    const s = initialState("salesforce");
    const { result } = mount("salesforce", {
      step: "credentials",
      values: { ...s.values, environment: "sandbox", clientId: "key" },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.next());
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(mocks.updateRemote).not.toHaveBeenCalled();
    expect(mocks.commit).not.toHaveBeenCalled();
    expect(result.current.recovery).toBe("settings");
  });

  it("rejects a late successful test after the upstream source changes externally", async () => {
    mocks.associations.mockResolvedValue({
      associations: [
        {
          issuerUrl: "https://login.salesforce.com",
          remoteSessionClientId: "client",
          serverCount: 1,
        },
      ],
    });
    mocks.client.mockResolvedValue({ id: "client", clientId: "key" });
    let finish!: (value: unknown) => void;
    mocks.probe.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    const source = {
      url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
      instanceUrl: "",
    };
    const mounted = mount("salesforce", { step: "test" }, source);
    await waitFor(() => expect(mounted.result.current.state.saved).toBe(true));
    act(() => mounted.result.current.testConnection());
    await waitFor(() => expect(mocks.probe).toHaveBeenCalled());
    mounted.rerender({
      mcpServer: server,
      currentSource: {
        ...source,
        url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-all",
      },
    });
    await act(async () =>
      finish({
        ok: true,
        value: { status: "ready", code: "ready", toolCount: 4 },
      }),
    );
    expect(mounted.result.current.state.tested).toBe(false);
    expect(mounted.result.current.toolCount).toBeNull();
    expect(mounted.result.current.state.values.salesforceServer).toBe(
      "sobject-all",
    );
  });
  it("requires review of an immutable refreshed source before retrying a conflicted save", async () => {
    const source = {
      url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
      instanceUrl: "",
    };
    const updated = {
      ...source,
      url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-all",
    };
    mocks.remote.mockResolvedValue({ id: "upstream", ...updated });
    const s = initialState("salesforce");
    const mounted = mount(
      "salesforce",
      { step: "credentials", values: { ...s.values, clientId: "key" } },
      source,
    );
    await waitFor(() => expect(mounted.result.current.loading).toBe(false));
    act(() => mounted.result.current.next());
    await waitFor(() => expect(mounted.result.current.error).toBeTruthy());
    expect(mocks.commit).not.toHaveBeenCalled();
    mounted.rerender({ mcpServer: server, currentSource: updated });
    await waitFor(() => expect(mounted.result.current.loading).toBe(false));
    expect(mounted.result.current.state.step).toBe("choices");
    expect(mounted.result.current.state.values.salesforceServer).toBe(
      "sobject-all",
    );
    expect(mounted.result.current.state.values.clientId).toBe("");
    act(() =>
      mounted.result.current.changeField({
        field: "clientId",
        value: "reviewed-key",
      }),
    );
    act(() =>
      mounted.result.current.dispatch({ type: "goto", step: "credentials" }),
    );
    act(() => mounted.result.current.next());
    await waitFor(() => expect(mounted.result.current.state.saved).toBe(true));
    expect(mocks.updateRemote).not.toHaveBeenCalled();
  });
});

describe("Salesforce review recovery", () => {
  it("keeps a superseded credential save locked until the old request settles", async () => {
    let rejectCommit!: (error: Error) => void;
    mocks.commit.mockImplementationOnce(
      () =>
        new Promise((_resolve, reject) => {
          rejectCommit = reject;
        }),
    );
    const state = initialState("salesforce");
    const mounted = mount("salesforce", {
      step: "credentials",
      values: { ...state.values, environment: "sandbox", clientId: "key" },
    });
    await waitFor(() => expect(mounted.result.current.loading).toBe(false));
    act(() => mounted.result.current.next());
    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    mounted.rerender({
      mcpServer: server,
      currentSource: {
        url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-all",
        instanceUrl: "",
      },
    });
    expect(mounted.result.current.pending).toBe("save");
    act(() => {
      mounted.result.current.next();
      mounted.result.current.signIn();
      mounted.result.current.testConnection();
    });
    expect(mocks.commit).toHaveBeenCalledOnce();
    expect(mocks.probe).not.toHaveBeenCalled();
    await act(async () => rejectCommit(new Error("old request rejected")));
    await waitFor(() => expect(mounted.result.current.pending).toBeNull());
    expect(mounted.result.current.state.saved).toBe(false);
  });
  it("refuses rollback if another server began sharing the source during credential commit", async () => {
    mocks.commit.mockImplementationOnce(async () => {
      mocks.linkedServers.mockResolvedValue({
        mcpServers: [server, { ...server, id: "new-wrapper" }],
      });
      throw new Error("commit failed");
    });
    const state = initialState("salesforce");
    const { result } = mount("salesforce", {
      step: "credentials",
      values: { ...state.values, environment: "sandbox", clientId: "key" },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.next());
    await waitFor(() =>
      expect(result.current.error).toContain("partially saved"),
    );
    expect(mocks.updateRemote).toHaveBeenCalledTimes(1);
    expect((await mocks.remote()).url).toContain(
      "sandbox/platform/sobject-reads",
    );
    expect(result.current.recovery).toBe("settings");
  });

  it("compensates a rejected commit after its own upstream query refresh during save", async () => {
    let rejectCommit!: (error: Error) => void;
    mocks.commit.mockImplementationOnce(
      () =>
        new Promise((_resolve, reject) => {
          rejectCommit = reject;
        }),
    );
    const source = {
      url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
      instanceUrl: "",
    };
    const selected =
      "https://api.salesforce.com/platform/mcp/v1/sandbox/platform/sobject-reads";
    const state = initialState("salesforce");
    const mounted = mount(
      "salesforce",
      {
        step: "credentials",
        values: { ...state.values, environment: "sandbox", clientId: "key" },
      },
      source,
    );
    await waitFor(() => expect(mounted.result.current.loading).toBe(false));
    act(() => mounted.result.current.next());
    await waitFor(() => expect(mocks.commit).toHaveBeenCalled());
    mounted.rerender({
      mcpServer: server,
      currentSource: { ...source, url: selected },
    });
    expect(mounted.result.current.state.values.clientId).toBe("key");
    await act(async () => rejectCommit(new Error("commit rejected")));
    await waitFor(() =>
      expect(mounted.result.current.error).toContain(
        "Couldn’t save Salesforce",
      ),
    );
    expect((await mocks.remote()).url).toBe(source.url);
    expect(mounted.result.current.state.saved).toBe(false);
  });

  it("restores the native URL after credential rejection and preserves unrelated headers", async () => {
    const url =
      "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads";
    const original = {
      id: "deployment",
      name: "X-Deployment",
      description: "Keep this description",
      value: "production",
      isRequired: false,
      isSecret: false,
    };
    mocks.remote.mockResolvedValue({ id: "upstream", url });
    mocks.headers.mockResolvedValue({ headers: [original] });
    mocks.commit.mockRejectedValueOnce(new Error("credential commit conflict"));
    const state = initialState("salesforce");
    const { result } = mount(
      "salesforce",
      {
        step: "credentials",
        values: {
          ...state.values,
          environment: "sandbox",
          clientId: "replacement",
        },
      },
      { url, instanceUrl: "" },
    );
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.next());
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect((await mocks.remote()).url).toBe(url);
    expect((await mocks.headers()).headers).toEqual([original]);
    expect(mocks.updateHeader).not.toHaveBeenCalled();
    expect(mocks.deleteHeader).not.toHaveBeenCalled();
    expect(result.current.state.saved).toBe(false);
    act(() => result.current.next());
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    expect((await mocks.remote()).url).toContain(
      "sandbox/platform/sobject-reads",
    );
  });
  it("does not overwrite an externally changed source during rollback", async () => {
    const changed =
      "https://api.salesforce.com/platform/mcp/v1/platform/sobject-all";
    mocks.commit.mockImplementation(async () => {
      mocks.remote.mockResolvedValue({ id: "upstream", url: changed });
      throw new Error("save failed");
    });
    const state = initialState("salesforce");
    const { result } = mount("salesforce", {
      step: "credentials",
      values: { ...state.values, environment: "sandbox", clientId: "key" },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.next());
    await waitFor(() =>
      expect(result.current.error).toContain("partially saved"),
    );
    expect((await mocks.remote()).url).toBe(changed);
    expect(mocks.updateRemote).toHaveBeenCalledTimes(1);
    expect(result.current.recovery).toBe("settings");
  });

  it.each(["duplicate", "secret", "request"])(
    "names invalid %s instance headers before saving",
    async (kind) => {
      const header = {
        id: "instance",
        name: "MCP-Instance-URL",
        value: "https://example.my.salesforce.com",
        isSecret: kind === "secret",
        valueFromRequestHeader: kind === "request" ? "X-Instance" : undefined,
      };
      mocks.headers.mockResolvedValue({
        headers:
          kind === "duplicate"
            ? [header, { ...header, id: "other" }]
            : [header],
      });
      const state = initialState("salesforce");
      const { result } = mount("salesforce", {
        step: "credentials",
        values: { ...state.values, clientId: "key" },
      });
      await waitFor(() => expect(result.current.loading).toBe(false));
      act(() => result.current.next());
      await waitFor(() =>
        expect(result.current.error).toContain("MCP-Instance-URL"),
      );
      expect(result.current.recovery).toBe("settings");
      expect(mocks.commit).not.toHaveBeenCalled();
      expect(mocks.updateRemote).not.toHaveBeenCalled();
      },
    );
  });

describe("Snowflake shared actions", () => {
  const oldUrl =
    "https://example.snowflakecomputing.com/api/v2/databases/OLD/schemas/PUBLIC/mcp-servers/MCP";
  const values = {
    ...initialState("snowflake").values,
    host: "example.snowflakecomputing.com",
    database: "APP",
    schema: "PUBLIC",
    serverName: "MCP",
    clientId: "app-id",
    clientSecret: "app-secret",
  };

  it.each([{ scope: [] }, { scope: ["session:role:OTHER"] }])(
    "requires corrected credentials for saved Snowflake scopes $scope",
    async ({ scope }) => {
      const url = oldUrl.replace("/OLD/", "/APP/");
      mocks.remote.mockResolvedValue({ id: "upstream", url });
      mocks.associations.mockResolvedValue({
        associations: [
          {
            issuerUrl: "https://example.snowflakecomputing.com",
            remoteSessionClientId: "client",
            serverCount: 1,
          },
        ],
      });
      mocks.client.mockResolvedValue({
        id: "client",
        clientId: "saved-app",
        scope,
        callbackUrl: "https://platform.example/callback",
      });
      const { result } = mount(
        "snowflake",
        { step: "test", values: { ...values, clientId: "", clientSecret: "" } },
        { url, instanceUrl: "" },
      );
      await waitFor(() => expect(result.current.loading).toBe(false));
      expect(result.current.state.saved).toBe(false);
      expect(result.current.state.configChanged).toBe(true);
      act(() => {
        result.current.signIn();
        result.current.testConnection();
      });
      expect(window.open).not.toHaveBeenCalled();
      expect(mocks.probe).not.toHaveBeenCalled();
    },
  );
  it("invalidates a restored Snowflake client when its saved scopes change", async () => {
    const url = oldUrl.replace("/OLD/", "/APP/");
    mocks.remote.mockResolvedValue({ id: "upstream", url });
    mocks.associations.mockResolvedValue({
      associations: [
        {
          issuerUrl: "https://example.snowflakecomputing.com",
          remoteSessionClientId: "client",
          serverCount: 1,
        },
      ],
    });
    const client = {
      id: "client",
      clientId: "saved-app",
      scope: ["session:role:all"],
      callbackUrl: "https://platform.example/callback",
    };
    mocks.client.mockResolvedValue(client);
    const { result } = mount(
      "snowflake",
      { step: "test", values: { ...values, clientId: "", clientSecret: "" } },
      { url, instanceUrl: "" },
    );
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    expect(result.current.state.configChanged).toBe(false);
    act(() => {
      clients
        .at(-1)
        ?.setQueryData(queryKeyRemoteSessionClient({ id: "client" }), {
          ...client,
          scope: ["session:role:OTHER"],
        });
    });
    await waitFor(() => expect(result.current.state.saved).toBe(false));
    expect(result.current.state.configChanged).toBe(true);
    act(() => result.current.signIn());
    expect(window.open).not.toHaveBeenCalled();
  });
  it("saves the native endpoint and confidential account OAuth configuration", async () => {
    mocks.remote.mockResolvedValue({ id: "upstream", url: oldUrl });
    mocks.provider.mockRejectedValue({ statusCode: 404 });
    const { result } = mount(
      "snowflake",
      { step: "credentials", values },
      { url: oldUrl, instanceUrl: "" },
    );
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.next());
    await waitFor(() => expect(result.current.state.saved).toBe(true));
    expect(mocks.updateRemote).toHaveBeenCalledWith({
      updateServerForm: {
        id: "upstream",
        url: oldUrl.replace("/OLD/", "/APP/"),
      },
    });
    expect(mocks.commit).toHaveBeenCalledWith({
      commitServerIdentityConfigurationForm: expect.objectContaining({
        targetServerOnly: true,
        createProvider: expect.objectContaining({
          issuer: "https://example.snowflakecomputing.com",
          authorizationEndpoint:
            "https://example.snowflakecomputing.com/oauth/authorize",
          tokenEndpoint:
            "https://example.snowflakecomputing.com/oauth/token-request",
          tokenEndpointAuthMethodsSupported: ["client_secret_post"],
        }),
        clientConfiguration: {
          clientId: "app-id",
          clientSecret: "app-secret",
          tokenEndpointAuthMethod: "client_secret_post",
          scope: ["session:role:all"],
        },
      }),
    });
    expect(mocks.createHeader).not.toHaveBeenCalled();
    expect(result.current.state.values.clientSecret).toBe("");
    expect(result.current.state.authorized).toBe(false);
  });
  it.each(["shared source", "external edit", "authorization header"])(
    "refuses a save after %s",
    async (reason) => {
      mocks.remote.mockResolvedValue({
        id: "upstream",
        url:
          reason === "external edit"
            ? oldUrl.replace("/OLD/", "/OTHER/")
            : oldUrl,
      });
      if (reason === "shared source")
        mocks.linkedServers.mockResolvedValue({
          mcpServers: [server, { ...server, id: "other-server" }],
        });
      if (reason === "authorization header")
        mocks.headers.mockResolvedValue({
          headers: [{ name: "Authorization", value: "redacted" }],
        });
      const { result } = mount(
        "snowflake",
        { step: "credentials", values },
        { url: oldUrl, instanceUrl: "" },
      );
      await waitFor(() => expect(result.current.loading).toBe(false));
      act(() => result.current.next());
      await waitFor(() => expect(result.current.error).toBeTruthy());
      expect(mocks.commit).not.toHaveBeenCalled();
      expect(mocks.updateRemote).not.toHaveBeenCalled();
      expect(result.current.recovery).toBe("settings");
    },
  );
});
