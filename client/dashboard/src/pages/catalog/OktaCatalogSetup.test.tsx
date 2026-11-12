import { writeCatalogSetupHandoff } from "./catalog-setup-handoff";
import { StrictMode } from "react";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CatalogProviderFlow } from "./AccountCatalogSetup";
import { useSetup } from "./provider-setup/setup-context";
import { initialState } from "./provider-setup/state";
import {
  providerProgressKey,
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
        {s.provider === "okta" && (
          <input
            aria-label="Okta organization"
            value={s.values.oktaOrg}
            onChange={(e) =>
              live.changeField({ field: "oktaOrg", value: e.target.value })
            }
          />
        )}
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
function Location() {
  return <output aria-label="Location">{useLocation().pathname}</output>;
}
function show(entry = "/") {
  return render(
    <StrictMode>
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/" element={<CatalogProviderFlow provider="okta" />} />
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
  writeCatalogSetupHandoff(key, { name: "Snowflake" });
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
describe("Okta catalog draft lifecycle (mocked API)", () => {
  const oktaKey = providerProgressKey(
    "okta",
    "person",
    "org",
    "project",
    "catalog:com.okta/managed-mcp",
  );
  function seedOkta() {
    writeCatalogSetupHandoff(oktaKey, { name: "Okta" });
    const s = initialState("okta");
    s.step = "credentials";
    Object.assign(s.values, {
      oktaOrg: "example.okta.com",
      oktaAppType: "web",
    });
    saveProviderProgress(oktaKey, s);
    api.upstream.mockResolvedValue({ url: "https://example.okta.com/mcp" });
  }
  it("creates the resolved org source only at credential save and retains partial writes for retry", async () => {
    seedOkta();
    api.create.mockRejectedValueOnce(new Error("temporary"));
    show("/");
    expect(api.remote).not.toHaveBeenCalled();
    const user = await credentials();
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByText(/Couldn’t prepare your Okta connection/);
    expect(api.remote).toHaveBeenCalledWith({
      createServerForm: {
        name: "Okta",
        url: "https://example.okta.com/mcp",
        transportType: "streamable-http",
      },
    });
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await waitFor(() => expect(api.next).toHaveBeenCalled());
    expect(api.remote).toHaveBeenCalledTimes(1);
    expect(api.live.mock.calls.at(-1)![0].provider).toBe("okta");
    expect(sessionStorage.getItem(oktaKey)).not.toContain("test-secret");
  });
  it("passes an edited org to the shared source updater on partial-install retry", async () => {
    seedOkta();
    api.create.mockRejectedValueOnce(new Error("temporary"));
    show("/");
    const user = await credentials();
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByText(/Couldn’t prepare your Okta connection/);
    await user.clear(screen.getByLabelText("Okta organization"));
    await user.type(
      screen.getByLabelText("Okta organization"),
      "other.okta.com",
    );
    await credentials();
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await waitFor(() => expect(api.next).toHaveBeenCalled());
    expect(api.remote).toHaveBeenCalledTimes(1);
    const [state, , , source] = api.live.mock.calls.at(-1)!;
    expect(state.values.oktaOrg).toBe("other.okta.com");
    expect(source.url).toBe("https://example.okta.com/mcp");
    // The shared controller reconciles this source with the selected org before saving credentials.
  });
});
