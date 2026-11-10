import { catalogPresetState } from "@/pages/security/server-guardrails/server-guardrail-policy";
import { writeSnowflakeHandoff } from "./snowflake-install";
import { StrictMode } from "react";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SnowflakeCatalogFlow } from "./SnowflakeCatalogSetup";
import { useSetup } from "./provider-setup/setup-context";
import { initialState, type State } from "./provider-setup/state";
import {
  providerProgressKey,
  providerSourceFingerprint,
  saveProviderProgress,
} from "@/pages/mcp/x/setup/provider-setup";

const api = vi.hoisted(() => ({
  remote: vi.fn(),
  create: vi.fn(),
  get: vi.fn(),
  upstream: vi.fn(),
  endpoint: vi.fn(),
  next: vi.fn(),
  live: vi.fn(),
  member: vi.fn(),
  members: vi.fn(),
  policy: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org" }),
  useProject: () => ({ id: "project" }),
  useSession: () => ({ user: { id: "person" } }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    risk: { policies: { create: api.policy } },
    remoteMcp: { createServer: api.remote, getServer: api.upstream },
    mcpServers: { create: api.create, get: api.get },
    metaMcp: { addMember: api.member, listMembers: api.members },
  }),
  useSlugs: () => ({ orgSlug: "example" }),
}));
vi.mock("@/pages/security/use-detector-mode", () => ({
  useDetectorMode: () => "presidio",
}));
vi.mock("@gram/client/react-query/riskListPolicies", () => ({
  invalidateAllRiskListPolicies: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    policyCenter: { new: { href: () => "/policy/new" } },
    mcp: {
      catalog: { detail: { href: () => "/catalog/snowflake" } },
      gateway: { overview: { href: (id: string) => `/gateway/${id}` } },
      details: {
        overview: { href: () => "/overview" },
        settings: { href: () => "/settings" },
      },
    },
  }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("@gram/client/react-query/metaMcpMembers", () => ({
  invalidateAllMetaMcpMembers: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@gram/client/react-query/mcpServers", () => ({
  invalidateAllMcpServers: vi.fn(),
}));
vi.mock("@gram/client/react-query/remoteMcpServers", () => ({
  invalidateAllRemoteMcpServers: vi.fn(),
}));
vi.mock("@gram/client/react-query/getMcpServer", () => ({
  useGetMcpServer: () => ({}),
}));
vi.mock("@gram/client/react-query/getRemoteMcpServer", () => ({
  useGetRemoteMcpServer: () => ({}),
}));
vi.mock("@/lib/mcpEndpoints", () => ({
  createDefaultMcpEndpoint: api.endpoint,
}));
vi.mock("@/pages/mcp/x/setup/useProviderConnection", () => ({
  useProviderConnection: (
    ...args: Parameters<
      typeof import("@/pages/mcp/x/setup/useProviderConnection").useProviderConnection
    >
  ) => {
    api.live(...args);
    return {
      next: () => {
        api.next();
        void args[4]?.();
      },
      loading: false,
      pending: null,
      error: null,
      dispatch: args[1],
    };
  },
}));
// Isolate lifecycle wiring while exercising the real reducer and secret-free progress.
// The actual shared shell/tasks have separate component tests and browser checks.
vi.mock("./provider-setup/SetupBody", () => ({
  SetupBody: () => {
    const { s, live, dispatch } = useSetup();
    return (
      <>
        <output>{s.step}</output>
        <input
          aria-label="Client ID"
          value={s.values.clientId}
          onChange={(e) =>
            live.changeField({ field: "clientId", value: e.target.value })
          }
        />
        <input
          aria-label="Client Secret"
          value={s.values.clientSecret}
          onChange={(e) =>
            live.changeField({ field: "clientSecret", value: e.target.value })
          }
        />
        <button disabled={!!live.pending} onClick={live.next}>
          Continue
        </button>
        <button
          onClick={() => {
            dispatch({
              type: "connection",
              saved: true,
              authorized: true,
              tested: true,
              configChanged: false,
            });
            dispatch({ type: "complete" });
          }}
        >
          Complete mocked connection
        </button>
        {live.error}
        <button onClick={live.retryLoad}>Retry load</button>
      </>
    );
  },
}));
const key = providerProgressKey(
  "snowflake",
  "person",
  "org",
  "project",
  "catalog:com.snowflake/cortex-mcp",
);
const url =
  "https://example.snowflakecomputing.com/api/v2/databases/APP/schemas/PUBLIC/mcp-servers/MCP";
const server = {
  id: "server",
  remoteMcpServerId: "remote",
  platformEndpointSlug: "example-mcp",
  name: "Snowflake",
  slug: "snowflake",
};
function seedCredentials() {
  const s: State = { ...initialState("snowflake"), step: "credentials" };
  Object.assign(s.values, {
    host: "example.snowflakecomputing.com",
    database: "APP",
    schema: "PUBLIC",
    serverName: "MCP",
    agent: "APP.PUBLIC.AGENT",
    accessRole: "MCP_ACCESS",
    oauthIntegration: "MCP_OAUTH",
  });
  saveProviderProgress(key, s);
}
function Location() {
  return <output aria-label="Location">{useLocation().pathname}</output>;
}
function show(entry = "/") {
  return render(
    <StrictMode>
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/" element={<SnowflakeCatalogFlow />} />
          <Route path="/overview" element={<div>Server overview</div>} />
          <Route path="/gateway/:id" element={<div>Gateway overview</div>} />
          <Route path="/policy/new" element={<div>Policy editor</div>} />
          <Route path="/catalog/snowflake" element={<div>Catalog entry</div>} />
        </Routes>
        <Location />
      </MemoryRouter>
    </StrictMode>,
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  sessionStorage.clear();
  writeSnowflakeHandoff(key, { name: "Snowflake" });
  api.remote.mockResolvedValue({ id: "remote" });
  api.create.mockResolvedValue(server);
  api.get.mockResolvedValue(server);
  api.upstream.mockResolvedValue({ url });
  api.endpoint.mockResolvedValue({ slug: "example-mcp" });
  api.members.mockResolvedValue({ members: [] });
  api.policy.mockResolvedValue({ id: "policy" });
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
async function credentials() {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Client ID"), "client-id");
  await user.type(screen.getByLabelText("Client Secret"), "test-secret");
  return user;
}
describe("Snowflake catalog draft lifecycle (mocked API)", () => {
  it("requires the modal’s User Identity handoff before opening draft setup", async () => {
    sessionStorage.removeItem(key + ":handoff");
    show();
    await waitFor(() =>
      expect(screen.getByLabelText("Location").textContent).toBe(
        "/catalog/snowflake",
      ),
    );
    expect(api.remote).not.toHaveBeenCalled();
    expect(api.create).not.toHaveBeenCalled();
  });
  it("opens the setup plan and collects values without probing or installing", async () => {
    show();
    expect(screen.getByText("begin")).toBeDefined();
    await userEvent.click(screen.getByText("Continue"));
    expect(screen.getByText("account")).toBeDefined();
    expect(api.remote).not.toHaveBeenCalled();
    expect(api.create).not.toHaveBeenCalled();
    expect(api.live).not.toHaveBeenCalled();
  });
  it("does not install without credentials", async () => {
    seedCredentials();
    show();
    await userEvent.click(screen.getByText("Continue"));
    expect(api.remote).not.toHaveBeenCalled();
    expect(api.create).not.toHaveBeenCalled();
  });
  it("installs the resolved source disabled, then delegates credential save once", async () => {
    seedCredentials();
    show();
    const user = await credentials();
    await user.click(screen.getByText("Continue"));
    await waitFor(() => expect(api.next).toHaveBeenCalledTimes(1));
    expect(api.remote).toHaveBeenCalledWith({
      createServerForm: {
        name: "Snowflake",
        url,
        transportType: "streamable-http",
      },
    });
    expect(api.create).toHaveBeenCalledWith({
      createMcpServerForm: {
        name: "Snowflake",
        remoteMcpServerId: "remote",
        visibility: "disabled",
      },
    });
    expect(api.live.mock.lastCall?.[0]).toMatchObject({
      values: { clientId: "client-id", clientSecret: "test-secret" },
    });
    expect(sessionStorage.getItem(key)).not.toContain("test-secret");
    expect(
      JSON.parse(sessionStorage.getItem(key + ":installation") ?? "null"),
    ).toMatchObject({ remoteId: "remote", serverId: "server" });
  });
  it("reuses a completed source after wrapper creation fails, including reload", async () => {
    seedCredentials();
    api.create.mockRejectedValueOnce(new Error("temporary failure"));
    let view = show();
    let user = await credentials();
    await user.click(screen.getByText("Continue"));
    await screen.findByText(/Couldn’t prepare/);
    view.unmount();
    show();
    user = await credentials();
    await user.click(screen.getByText("Continue"));
    await waitFor(() => expect(api.next).toHaveBeenCalledTimes(1));
    expect(api.remote).toHaveBeenCalledTimes(1);
    expect(api.create).toHaveBeenCalledTimes(2);
  });
  it("resumes the same installed source without automatically saving absent secrets", async () => {
    seedCredentials();
    sessionStorage.setItem(
      key + ":installation",
      JSON.stringify({ remoteId: "remote", serverId: "server" }),
    );
    show();
    await waitFor(() => expect(api.live).toHaveBeenCalled());
    expect(api.remote).not.toHaveBeenCalled();
    expect(api.create).not.toHaveBeenCalled();
    expect(api.next).not.toHaveBeenCalled();
  });
  it("stops provisioning after leaving setup and resumes the retained source", async () => {
    seedCredentials();
    let finish: (value: { id: string }) => void = () => {};
    api.remote.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const view = show();
    const user = await credentials();
    await user.click(screen.getByText("Continue"));
    view.unmount();
    finish({ id: "remote" });
    await waitFor(() =>
      expect(sessionStorage.getItem(key + ":installation")).toContain("remote"),
    );
    expect(api.create).not.toHaveBeenCalled();
  });

  it("retries endpoint preparation on the same server after reload", async () => {
    seedCredentials();
    sessionStorage.setItem(
      key + ":installation",
      JSON.stringify({ remoteId: "remote", serverId: "server" }),
    );
    let endpointCreated = false;
    api.get.mockImplementation(async () => ({
      ...server,
      platformEndpointSlug: endpointCreated
        ? server.platformEndpointSlug
        : null,
    }));
    api.endpoint
      .mockResolvedValueOnce(undefined)
      .mockImplementationOnce(async () => {
        endpointCreated = true;
        return { slug: server.platformEndpointSlug };
      });
    show();
    await screen.findByText(/Couldn’t load your saved/);
    await userEvent.click(screen.getByText("Retry load"));
    await waitFor(() => expect(api.live).toHaveBeenCalled());
    expect(api.endpoint).toHaveBeenCalledTimes(2);
    for (const attempt of [1, 2])
      expect(api.endpoint).toHaveBeenNthCalledWith(
        attempt,
        expect.anything(),
        expect.objectContaining({ id: server.id }),
        "example",
      );
    expect(api.remote).not.toHaveBeenCalled();
    expect(api.create).not.toHaveBeenCalled();
    expect(api.next).not.toHaveBeenCalled();
    expect(
      sessionStorage.getItem(
        providerProgressKey("snowflake", "person", "org", "project", "server"),
      ),
    ).toContain('"step":"credentials"');
  });
  it("clears stale setup metadata when the saved upstream changes outside this draft", async () => {
    seedCredentials();
    sessionStorage.setItem(
      key + ":installation",
      JSON.stringify({
        remoteId: "remote",
        serverId: "server",
        sourceFingerprint: providerSourceFingerprint("remote", {
          url: "https://previous.snowflakecomputing.com/api/v2/databases/APP/schemas/PUBLIC/mcp-servers/MCP",
        }),
      }),
    );
    show();
    await waitFor(() => expect(api.live).toHaveBeenCalled());
    expect(api.live.mock.lastCall?.[0]).toMatchObject({
      step: "account",
      saved: false,
      values: {
        host: "https://example.snowflakecomputing.com",
        agent: "",
        clientSecret: "",
      },
    });
    expect(api.next).not.toHaveBeenCalled();
  });
  it("preserves the name entered in the existing modal", async () => {
    writeSnowflakeHandoff(key, { name: "Snowflake connection" });
    seedCredentials();
    show();
    const user = await credentials();
    await user.click(screen.getByText("Continue"));
    await waitFor(() => expect(api.next).toHaveBeenCalledOnce());
    expect(api.remote).toHaveBeenCalledWith({
      createServerForm: {
        name: "Snowflake connection",
        url,
        transportType: "streamable-http",
      },
    });
    expect(api.create.mock.lastCall?.[0].createMcpServerForm.name).toBe(
      "Snowflake connection",
    );
  });
  it("applies a selected guardrail to the actual server before saving credentials", async () => {
    writeSnowflakeHandoff(key, {
      name: "Snowflake",
      guardrail: catalogPresetState([]),
    });
    seedCredentials();
    show();
    const user = await credentials();
    await user.click(screen.getByText("Continue"));
    await waitFor(() => expect(api.next).toHaveBeenCalledOnce());
    expect(api.policy).toHaveBeenCalledOnce();
    expect(api.policy.mock.lastCall?.[0]).toMatchObject({
      createRiskPolicyRequestBody: {
        name: "Snowflake guardrail",
        mcpScope: { allServers: false, servers: [{ mcpServerId: "server" }] },
      },
    });
    expect(api.create.mock.invocationCallOrder[0]).toBeLessThan(
      api.policy.mock.invocationCallOrder[0]!,
    );
    expect(api.policy.mock.invocationCallOrder[0]).toBeLessThan(
      api.next.mock.invocationCallOrder[0]!,
    );
  });
  it("does not create a policy when the existing customize action was selected", async () => {
    writeSnowflakeHandoff(key, {
      name: "Snowflake",
      guardrail: catalogPresetState([]),
      customize: true,
    });
    seedCredentials();
    show();
    const user = await credentials();
    await user.click(screen.getByText("Continue"));
    await waitFor(() => expect(api.next).toHaveBeenCalledOnce());
    expect(api.policy).not.toHaveBeenCalled();
  });
  it("keeps the accepted server and continues setup if the optional policy fails", async () => {
    writeSnowflakeHandoff(key, {
      name: "Snowflake",
      guardrail: catalogPresetState([]),
    });
    seedCredentials();
    api.policy.mockRejectedValueOnce(new Error("Policy unavailable"));
    show();
    const user = await credentials();
    await user.click(screen.getByText("Continue"));
    await waitFor(() => expect(api.next).toHaveBeenCalledOnce());
    expect(api.remote).toHaveBeenCalledOnce();
    expect(api.create).toHaveBeenCalledOnce();
    expect(
      JSON.parse(sessionStorage.getItem(key + ":installation") ?? "null"),
    ).toMatchObject({ serverId: "server", guardrailAttempted: true });
  });
  it("opens Overview even if storage becomes unavailable during completion", async () => {
    seedCredentials();
    show();
    const user = await credentials();
    await user.click(screen.getByText("Continue"));
    await waitFor(() => expect(api.next).toHaveBeenCalledOnce());
    const remove = vi.fn(() => {
      throw new Error("Storage unavailable");
    });
    const storage = sessionStorage;
    vi.stubGlobal("sessionStorage", {
      getItem: storage.getItem.bind(storage),
      setItem: storage.setItem.bind(storage),
      removeItem: remove,
    });
    await user.click(screen.getByText("Complete mocked connection"));
    await screen.findByText("Server overview");
    expect(remove).toHaveBeenCalled();
    expect(screen.getByLabelText("Location").textContent).toBe("/overview");
  });
  it("keeps edited endpoint values when reusing a partially created upstream", async () => {
    seedCredentials();
    sessionStorage.setItem(
      key + ":installation",
      JSON.stringify({ remoteId: "remote" }),
    );
    const previousUrl = url.replace("/APP/", "/OLD/");
    api.upstream.mockResolvedValue({ url: previousUrl });
    show();
    const user = await credentials();
    await user.click(screen.getByText("Continue"));
    await waitFor(() => expect(api.next).toHaveBeenCalledOnce());
    expect(api.remote).not.toHaveBeenCalled();
    expect(api.live.mock.lastCall?.[0]).toMatchObject({
      step: "credentials",
      values: {
        database: "APP",
        clientId: "client-id",
        clientSecret: "test-secret",
      },
    });
    expect(api.live.mock.lastCall?.[3]).toEqual({ url: previousUrl });
  });
  it("attaches the configured source to the gateway retained by the modal handoff", async () => {
    seedCredentials();
    show("/?attachToGateway=gateway-id");
    const user = await credentials();
    await user.click(screen.getByText("Continue"));
    await waitFor(() => expect(api.next).toHaveBeenCalledOnce());
    expect(api.members).toHaveBeenCalledWith({ metaMcpServerId: "gateway-id" });
    await waitFor(() =>
      expect(api.member).toHaveBeenCalledWith({
        addMetaMcpMemberForm: {
          metaMcpServerId: "gateway-id",
          mcpServerId: "server",
        },
      }),
    );
  });
  it.each([false, true])(
    "returns to the originating gateway unless customize was explicitly selected (customize=%s)",
    async (customize) => {
      writeSnowflakeHandoff(key, {
        name: "Snowflake",
        guardrail: customize ? catalogPresetState([]) : undefined,
        customize,
      });
      seedCredentials();
      show("/?attachToGateway=gateway-id");
      const user = await credentials();
      await user.click(screen.getByText("Continue"));
      await waitFor(() => expect(api.member).toHaveBeenCalled());
      await user.click(screen.getByText("Complete mocked connection"));
      await screen.findByText(customize ? "Policy editor" : "Gateway overview");
      expect(screen.getByLabelText("Location").textContent).toBe(
        customize ? "/policy/new" : "/gateway/gateway-id",
      );
    },
  );
});
