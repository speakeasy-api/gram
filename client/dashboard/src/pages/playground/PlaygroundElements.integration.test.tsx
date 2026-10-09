import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  focusManager,
  onlineManager,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import { queryKeyGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { useToolsetMcpTarget } from "@/hooks/useToolsetUrl";
import { PlaygroundElements } from "./PlaygroundElements";

const mocks = vi.hoisted(() => ({
  server: vi.fn(),
  mint: vi.fn(),
  probe: vi.fn(),
  createClient: vi.fn(),
  mounted: vi.fn(),
  unmounted: vi.fn(),
}));
const toolset = {
  id: "T",
  slug: "selected",
  name: "Provider",
  mcpSlug: "canonical",
  userSessionIssuerId: "issuer-C",
};
const selected = {
  id: "S",
  visibility: "private",
  platformEndpointSlug: "selected",
  userSessionIssuerId: "issuer-S",
};
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project", slug: "project" }),
  useSession: () => ({ user: { id: "user" }, session: "session" }),
}));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://platform.example",
  mcpConnectionUrl: (url: string | undefined) => url,
  firstPartyConnectUrl: (url: string | undefined) =>
    url ? `${url}/connect/first-party` : undefined,
}));
// Keep the SDK React Query hooks and the entire connection pipeline real. Only
// the SDK request functions and MCP transport are substituted.
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/funcs/mcpServersGet.js", () => ({
  mcpServersGet: mocks.server,
}));
vi.mock("@gram/client/funcs/userSessionsMint.js", () => ({
  userSessionsMint: mocks.mint,
}));
vi.mock("@ai-sdk/mcp", () => ({ createMCPClient: mocks.createClient }));
vi.mock("@/hooks/toolTypes", () => ({ useToolset: () => ({ data: toolset }) }));
vi.mock("@/hooks/useMissingEnvironmentVariables", () => ({
  useMissingRequiredEnvVars: () => 0,
}));
vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: () => ({}),
}));
vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: () => ({}),
}));
vi.mock("@/routes", () => ({ useRoutes: vi.fn() }));
vi.mock("./PlaygroundChat", () => ({
  PlaygroundChat: function Chat({
    mcpUrl,
    gatewayToken,
  }: {
    mcpUrl: string;
    gatewayToken: string;
  }) {
    const [draft, setDraft] = useState("");
    useEffect(() => {
      mocks.mounted();
      return mocks.unmounted;
    }, []);
    return (
      <>
        <input
          aria-label="Draft"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
        />
        <span>{mcpUrl}</span>
        <span>{gatewayToken}</span>
      </>
    );
  },
}));
function serviceError(status: number) {
  return new ServiceError(
    {
      fault: false,
      id: "error",
      message: "lookup failed",
      name: "lookup",
      temporary: false,
      timeout: false,
    },
    {
      request: new Request("https://platform.example"),
      response: new Response(null, { status }),
      body: "",
    },
  );
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
const clients: QueryClient[] = [];
const serverKey = queryKeyGetMcpServer({ toolsetId: "T" });
function Observer({
  source = toolset,
  label = "observer",
}: {
  source?: typeof toolset;
  label?: string;
}) {
  const target = useToolsetMcpTarget(source);
  return <span data-testid={label}>{JSON.stringify(target)}</span>;
}
function readTarget(label: string) {
  return JSON.parse(screen.getByTestId(label).textContent ?? "{}");
}
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, throwOnError: true } },
  });
  clients.push(client);
  const tree = (observer = false) => (
    <QueryClientProvider client={client}>
      <PlaygroundElements
        toolsetSlug="selected"
        environmentSlug={null}
        model="test"
      />
      {observer && <Observer />}
    </QueryClientProvider>
  );
  const view = render(tree());
  return { client, addObserver: () => view.rerender(tree(true)) };
}
beforeEach(() => {
  vi.clearAllMocks();
  focusManager.setFocused(true);
  onlineManager.setOnline(true);
  mocks.server.mockResolvedValue({ ok: true, value: selected });
  mocks.mint.mockResolvedValue({ ok: true, value: { accessToken: "token-S" } });
  mocks.probe.mockResolvedValue({ tools: [] });
  mocks.createClient.mockImplementation(async () => ({
    listTools: mocks.probe,
    close: vi.fn().mockResolvedValue(undefined),
  }));
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
  focusManager.setFocused(undefined);
  onlineManager.setOnline(true);
});

describe("Elements real connection refresh", () => {
  it.each(["selected", "legacy"])(
    "preserves %s chat and draft through focus, reconnect, mount and invalidation",
    async (kind) => {
      const response =
        kind === "legacy"
          ? { ok: false, error: serviceError(404) }
          : { ok: true, value: selected };
      mocks.server.mockResolvedValue(response);
      const { client, addObserver } = mount();
      const draft = await screen.findByRole("textbox", { name: "Draft" });
      fireEvent.change(draft, { target: { value: "unsent message" } });
      expect(mocks.server.mock.calls[0]?.[1]).toEqual({ toolsetId: "T" });
      expect(mocks.mint.mock.calls[0]?.[1].mintUserSessionRequestBody).toEqual(
        kind === "legacy" ? { toolsetId: "T" } : { mcpServerId: "S" },
      );
      expect(mocks.createClient.mock.calls[0]?.[0].transport).toMatchObject({
        url: `https://platform.example/mcp/${kind === "legacy" ? "canonical" : "selected"}`,
        headers: { Authorization: "Bearer token-S" },
      });
      for (const trigger of ["focus", "reconnect", "mount", "invalidate"]) {
        const pending = deferred<typeof response>();
        mocks.server.mockReturnValueOnce(pending.promise);
        act(() => {
          if (trigger === "focus") {
            focusManager.setFocused(false);
            focusManager.setFocused(true);
          } else if (trigger === "reconnect") {
            onlineManager.setOnline(false);
            onlineManager.setOnline(true);
          } else if (trigger === "mount") addObserver();
          else void client.invalidateQueries({ queryKey: serverKey });
        });
        await waitFor(() =>
          expect(client.isFetching({ queryKey: serverKey })).toBe(1),
        );
        // Flush observer notifications while the request is deliberately unresolved.
        await act(async () => {
          await new Promise((resolve) => {
            setTimeout(resolve, 10);
          });
        });
        expect(screen.getByRole("textbox", { name: "Draft" })).toBe(draft);
        expect((draft as HTMLInputElement).value).toBe("unsent message");
        expect(mocks.mounted).toHaveBeenCalledTimes(1);
        expect(mocks.unmounted).not.toHaveBeenCalled();
        await act(async () => pending.resolve(response));
        await waitFor(() =>
          expect(client.isFetching({ queryKey: serverKey })).toBe(0),
        );
      }
    },
  );

  it.each(["no previous server", "previous server"])(
    "shares settled 404 classification with an observer joining an in-flight refresh (%s)",
    async (initial) => {
      if (initial === "no previous server")
        mocks.server.mockResolvedValue({ ok: false, error: serviceError(404) });
      const { client, addObserver } = mount();
      await screen.findByRole("textbox", { name: "Draft" });
      if (initial === "previous server") {
        client.setQueryData(serverKey, selected);
        mocks.server.mockResolvedValue({ ok: false, error: serviceError(404) });
        await act(async () => {
          await client.invalidateQueries({ queryKey: serverKey });
        });
        await screen.findByText("https://platform.example/mcp/canonical");
      }
      // Normalization clears the prior server, if any; it cannot retain S's
      // identity as React Query normally would after a failed refetch.
      expect(
        client.getQueryData([...serverKey, "connectionTarget"]),
      ).toBeNull();
      expect(client.getQueryData(serverKey)).toEqual(
        initial === "previous server" ? selected : undefined,
      );
      const pending = deferred<{ ok: false; error: ServiceError }>();
      mocks.server.mockReturnValueOnce(pending.promise);
      act(() => {
        void client.invalidateQueries({ queryKey: serverKey });
      });
      await waitFor(() =>
        expect(client.isFetching({ queryKey: serverKey })).toBe(1),
      );
      await act(async () => {
        await new Promise((resolve) => {
          setTimeout(resolve, 10);
        });
      });
      const requests = mocks.server.mock.calls.length;
      addObserver(); // Deliberately join AFTER the request has already started.
      expect(mocks.server).toHaveBeenCalledTimes(requests);
      expect(readTarget("observer")).toMatchObject({
        status: "ready",
        legacy: true,
        isLoading: false,
        url: "https://platform.example/mcp/canonical",
        userSessionIssuerId: "issuer-C",
      });
      expect(readTarget("observer").serverId).toBeUndefined();
      expect(screen.getByRole("textbox", { name: "Draft" })).toBeTruthy();
      await act(async () =>
        pending.resolve({ ok: false, error: serviceError(503) }),
      );
      await screen.findByRole("alert");
      await waitFor(() =>
        expect(readTarget("observer")).toMatchObject({
          status: "error",
          legacy: false,
        }),
      );
      expect(readTarget("observer").url).toBeUndefined();
      expect(readTarget("observer").userSessionIssuerId).toBeUndefined();
      expect(screen.queryByRole("textbox")).toBeNull();
    },
  );

  it("does not leak A's legacy fallback or late completion into a pending B selection", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    clients.push(client);
    mocks.server.mockResolvedValue({ ok: false, error: serviceError(404) });
    const b = {
      ...toolset,
      id: "T2",
      mcpSlug: "b",
      userSessionIssuerId: "issuer-B-legacy",
    };
    const tree = (source: typeof toolset) => (
      <QueryClientProvider client={client}>
        <Observer source={source} label="selection" />
        <Observer source={toolset} label="retained-A" />
      </QueryClientProvider>
    );
    const { rerender } = render(tree(toolset));
    await waitFor(() => expect(readTarget("selection").legacy).toBe(true));
    const aRequest = deferred<{ ok: true; value: typeof selected }>();
    const bRequest = deferred<{ ok: true; value: typeof selected }>();
    mocks.server.mockImplementation((_sdk, request: { toolsetId: string }) =>
      request.toolsetId === "T" ? aRequest.promise : bRequest.promise,
    );
    act(() => {
      void client.invalidateQueries({ queryKey: serverKey });
    });
    await waitFor(() =>
      expect(client.isFetching({ queryKey: serverKey })).toBe(1),
    );
    rerender(tree(b));
    await waitFor(() =>
      expect(
        client.isFetching({
          queryKey: queryKeyGetMcpServer({ toolsetId: "T2" }),
        }),
      ).toBe(1),
    );
    expect(readTarget("selection")).toMatchObject({
      status: "loading",
      legacy: false,
    });
    expect(readTarget("selection").url).toBeUndefined();
    expect(readTarget("selection").userSessionIssuerId).toBeUndefined();
    await act(async () => aRequest.resolve({ ok: true, value: selected }));
    await waitFor(() => expect(readTarget("retained-A").serverId).toBe("S"));
    expect(readTarget("selection")).toMatchObject({
      status: "loading",
      legacy: false,
    });
    expect(readTarget("selection").serverId).toBeUndefined();
    expect(readTarget("selection").url).toBeUndefined();
    expect(readTarget("selection").userSessionIssuerId).toBeUndefined();
    await act(async () =>
      bRequest.resolve({
        ok: true,
        value: {
          ...selected,
          id: "S2",
          platformEndpointSlug: "selected-b",
          userSessionIssuerId: "issuer-B",
        },
      }),
    );
    await waitFor(() =>
      expect(readTarget("selection")).toMatchObject({
        status: "ready",
        legacy: false,
        serverId: "S2",
        url: "https://platform.example/mcp/selected-b",
        userSessionIssuerId: "issuer-B",
      }),
    );
  });

  it.each(["mint", "probe"])(
    "preserves chat through a healthy real %s refresh and fails closed on failure",
    async (stage) => {
      const { client } = mount();
      const draft = await screen.findByRole("textbox", { name: "Draft" });
      fireEvent.change(draft, { target: { value: "unsent message" } });
      const queryKey = [
        stage === "mint" ? "userSessionToken" : "proxiedMcpTools",
      ];
      const transport = stage === "mint" ? mocks.mint : mocks.probe;
      const response =
        stage === "mint"
          ? { ok: true, value: { accessToken: "token-S" } }
          : { tools: [] };
      const pending = deferred<typeof response>();
      transport.mockReturnValueOnce(pending.promise);
      act(() => {
        void client.invalidateQueries({ queryKey });
      });
      await waitFor(() => expect(client.isFetching({ queryKey })).toBe(1));
      await act(async () => {
        await new Promise((resolve) => {
          setTimeout(resolve, 10);
        });
      });
      expect(screen.getByRole("textbox", { name: "Draft" })).toBe(draft);
      expect((draft as HTMLInputElement).value).toBe("unsent message");
      expect(mocks.unmounted).not.toHaveBeenCalled();
      await act(async () => pending.resolve(response));
      await waitFor(() => expect(client.isFetching({ queryKey })).toBe(0));

      expect(screen.getByRole("textbox", { name: "Draft" })).toBe(draft);
      expect((draft as HTMLInputElement).value).toBe("unsent message");
      expect(mocks.mounted).toHaveBeenCalledTimes(1);
      expect(mocks.unmounted).not.toHaveBeenCalled();

      transport.mockRejectedValueOnce(
        new Error("HTTP 503 Service Unavailable"),
      );
      await act(async () => {
        await client.invalidateQueries({ queryKey });
      });
      expect((await screen.findByRole("alert")).textContent).toContain(
        stage === "mint" ? "Unable to create a session" : "Unable to connect",
      );
      expect(screen.queryByRole("textbox")).toBeNull();
      const retry = deferred<typeof response>();
      transport.mockReturnValueOnce(retry.promise);
      fireEvent.click(screen.getByRole("button", { name: "Retry" }));
      expect((await screen.findByRole("status")).textContent).toContain(
        "Connecting",
      );
      expect(client.isFetching({ queryKey })).toBe(1);
      expect(screen.queryByRole("textbox")).toBeNull();
      await act(async () => retry.resolve(response));
      await screen.findByRole("textbox", { name: "Draft" });
    },
  );

  it.each(["selected", "legacy"])(
    "fails closed on a real %s lookup refetch failure and retries to a new authoritative tuple",
    async (kind) => {
      if (kind === "legacy")
        mocks.server.mockResolvedValue({ ok: false, error: serviceError(404) });
      const { client } = mount();
      await screen.findByRole("textbox", { name: "Draft" });
      mocks.server.mockResolvedValue({ ok: false, error: serviceError(503) });
      await act(async () => {
        await client.invalidateQueries({ queryKey: serverKey });
      });
      expect((await screen.findByRole("alert")).textContent).toContain(
        "Unable to load",
      );
      expect(screen.queryByRole("textbox")).toBeNull();
      const retry = deferred<{ ok: true; value: typeof selected }>();
      mocks.server.mockReturnValueOnce(retry.promise);
      fireEvent.click(screen.getByRole("button", { name: "Retry" }));
      await waitFor(() =>
        expect(client.isFetching({ queryKey: serverKey })).toBe(1),
      );
      expect((await screen.findByRole("status")).textContent).toContain(
        "Connecting",
      );
      expect(screen.queryByRole("textbox")).toBeNull();
      await act(async () =>
        retry.resolve({
          ok: true,
          value: {
            ...selected,
            id: "S2",
            platformEndpointSlug: "selected-2",
            userSessionIssuerId: "issuer-S2",
          },
        }),
      );
      await screen.findByRole("textbox", { name: "Draft" });
      expect(
        screen.getByText("https://platform.example/mcp/selected-2"),
      ).toBeTruthy();
      expect(mocks.mint.mock.lastCall?.[1].mintUserSessionRequestBody).toEqual({
        mcpServerId: "S2",
      });
      expect(
        client.getQueryData([
          "userSessionToken",
          "mcpServer",
          "project",
          "S2",
          "issuer-S2",
          "user",
        ]),
      ).toEqual({ accessToken: "token-S" });
    },
  );

  it.each(["disabled", "custom-only"])(
    "keeps settled %s notice during background refresh, but shows explicit retry progress",
    async (kind) => {
      const value = {
        ...selected,
        ...(kind === "disabled"
          ? { visibility: "disabled" }
          : { platformEndpointSlug: undefined }),
      };
      mocks.server.mockResolvedValue({ ok: true, value });
      const { client } = mount();
      const alert = await screen.findByRole("alert");
      const pending = deferred<{ ok: true; value: typeof value }>();
      mocks.server.mockReturnValueOnce(pending.promise);
      act(() => {
        void client.invalidateQueries({ queryKey: serverKey });
      });
      await waitFor(() =>
        expect(client.isFetching({ queryKey: serverKey })).toBe(1),
      );
      await act(async () => {
        await new Promise((resolve) => {
          setTimeout(resolve, 10);
        });
      });
      expect(screen.getByRole("alert")).toBe(alert);
      expect(screen.queryByRole("status")).toBeNull();
      await act(async () => pending.resolve({ ok: true, value }));
      await waitFor(() =>
        expect(client.isFetching({ queryKey: serverKey })).toBe(0),
      );
      const retry = deferred<{ ok: true; value: typeof selected }>();
      mocks.server.mockReturnValueOnce(retry.promise);
      fireEvent.click(screen.getByRole("button", { name: "Retry" }));
      expect((await screen.findByRole("status")).textContent).toContain(
        "Connecting",
      );
      await act(async () => retry.resolve({ ok: true, value: selected }));
      await screen.findByRole("textbox", { name: "Draft" });
    },
  );
});
