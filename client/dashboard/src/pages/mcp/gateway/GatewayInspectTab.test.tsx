import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import type { GatewayInspection } from "./useGatewayInspection";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { GatewayInspectTab } from "./GatewayInspectTab";

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  mcpConnectionUrl: (value: string) => value,
}));

vi.mock("@/components/ui/CodeSnippet", () => ({
  CodeSnippet: ({ code }: { code: string }) => <pre>{code}</pre>,
}));

const state = vi.hoisted(() => ({
  mint: vi.fn(),
  frozen: vi.fn(),
  data: undefined as GatewayInspection | undefined,
  loading: false,
}));
vi.mock("./GatewayCodeExecution", () => ({
  GatewayCodeExecution: ({ disabled }: { disabled: boolean }) => {
    const [source, setSource] = useState("initial");
    return (
      <input
        aria-label="Persistent source"
        value={source}
        disabled={disabled}
        onChange={(event) => setSource(event.target.value)}
      />
    );
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-test" }),
  useSession: () => ({ user: { id: "user-test" } }),
}));
vi.mock("@/hooks/useUserSessionToken", () => ({
  useUserSessionToken: (input: { target: { discoveryMode?: string } }) => {
    state.mint(input);
    return {
      accessToken: undefined,
      isLoading: false,
      error: input.target.discoveryMode
        ? new Error("Gateway discovery settings are not available")
        : null,
      retry: vi.fn(),
    };
  },
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  useResolvedMcpServerUrl: () => ({
    mcpUrl: "https://example.com/mcp/gateway",
    loading: false,
  }),
}));
vi.mock("@/routes", () => ({ useRoutes: () => ({}) }));
vi.mock("./GatewayDetailsRouting", () => ({
  gatewayTabHref: () => "/settings",
}));
vi.mock("./GatewayFrozenToolset", () => ({
  GatewayFrozenToolset: (props: unknown) => {
    state.frozen(props);
    return null;
  },
}));
vi.mock("./GatewaySettingsTab", () => ({
  GATEWAY_INSTRUCTIONS_SECTION_ID: "instructions",
}));
vi.mock("./useGatewayMemberRows", () => ({
  useGatewayMemberRows: () => ({ rows: [] }),
}));
vi.mock("./useGatewayInspection", () => ({
  useGatewayInspection: () => ({
    data: state.data,
    isLoading: state.loading,
    isError: false,
    needsAuth: false,
    error: null,
    refetch: vi.fn(),
  }),
  useGatewayDescribeServer: () => ({ data: undefined }),
}));
vi.mock("@/components/page-layout", () => {
  const Block = ({ children }: { children?: React.ReactNode }) => (
    <div>{children}</div>
  );
  return {
    Page: {
      Toolbar: Object.assign(Block, { Leading: Block, Actions: Block }),
      Section: Object.assign(Block, {
        Title: Block,
        Description: Block,
        CTA: Block,
        Body: Block,
      }),
    },
  };
});
function Location() {
  return <output aria-label="location">{useLocation().search}</output>;
}
afterEach(() => {
  cleanup();
  state.data = undefined;
  state.loading = false;
});
it("lets a saved mode fall back to the gateway default when its product feature is disabled", () => {
  state.mint.mockClear();
  const gateway = {
    id: "gateway-test",
    userSessionIssuerId: "issuer-test",
    discoveryMode: "progressive",
  } as MetaMcpServer;
  render(
    <MemoryRouter
      initialEntries={["/inspect?discovery_mode=direct&keep=value"]}
    >
      <GatewayInspectTab
        metaMcpServer={gateway}
        endpoints={[]}
        isLoadingEndpoints={false}
      />
      <Location />
    </MemoryRouter>,
  );
  expect(state.mint).toHaveBeenLastCalledWith(
    expect.objectContaining({
      target: expect.objectContaining({ discoveryMode: "direct" }),
    }),
  );
  expect(screen.queryByRole("combobox")).toBeNull();
  expect(state.frozen).toHaveBeenLastCalledWith(
    expect.objectContaining({ reconnectFailed: true }),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Use gateway default and reconnect" }),
  );
  expect(state.mint).toHaveBeenLastCalledWith(
    expect.objectContaining({
      target: expect.objectContaining({ discoveryMode: undefined }),
    }),
  );
  expect(screen.getByLabelText("location").textContent).toBe("?keep=value");
  expect(state.frozen).toHaveBeenLastCalledWith(
    expect.objectContaining({ reconnectFailed: false }),
  );
  expect(
    screen.queryByText("Gateway discovery settings are not available"),
  ).toBeNull();
});
it("applies enabled discovery choices to the URL and the minted connection", async () => {
  state.mint.mockClear();
  const gateway = {
    id: "gateway-test",
    userSessionIssuerId: "issuer-test",
    discoveryMode: "progressive",
    discoveryModesEnabled: true,
  } as MetaMcpServer;
  render(
    <TooltipProvider>
      <MemoryRouter initialEntries={["/inspect?keep=value"]}>
        <GatewayInspectTab
          metaMcpServer={gateway}
          endpoints={[]}
          isLoadingEndpoints={false}
        />
        <Location />
      </MemoryRouter>
    </TooltipProvider>,
  );
  fireEvent.click(
    screen.getByRole("combobox", { name: "Inspect discovery mode" }),
  );
  fireEvent.click(await screen.findByRole("option", { name: "Direct" }));
  fireEvent.click(screen.getByRole("button", { name: "Apply and reconnect" }));
  expect(state.mint).toHaveBeenLastCalledWith(
    expect.objectContaining({
      target: expect.objectContaining({ discoveryMode: "direct" }),
    }),
  );
  expect(screen.getByLabelText("location").textContent).toBe(
    "?keep=value&discovery_mode=direct",
  );
  fireEvent.click(
    screen.getByRole("combobox", { name: "Inspect discovery mode" }),
  );
  fireEvent.click(
    await screen.findByRole("option", { name: "Use gateway default" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Apply and reconnect" }));
  expect(state.mint).toHaveBeenLastCalledWith(
    expect.objectContaining({
      target: expect.objectContaining({ discoveryMode: undefined }),
    }),
  );
  expect(screen.getByLabelText("location").textContent).toBe("?keep=value");
});

it("preserves Python source while inspection reloads for a refreshed credential", () => {
  const data: GatewayInspection = {
    discoveryMode: "code_mode",
    tools: [{ name: "execute", description: "Run Python" }],
    instructions: "Python",
    protocolVersion: undefined,
    serverName: undefined,
    servers: undefined,
  };
  state.data = data;
  const gateway = {
    id: "gateway-test",
    discoveryMode: "code_mode",
  } as MetaMcpServer;
  const view = () => (
    <MemoryRouter>
      <GatewayInspectTab
        metaMcpServer={gateway}
        endpoints={[]}
        isLoadingEndpoints={false}
      />
    </MemoryRouter>
  );
  const { rerender } = render(view());
  fireEvent.change(screen.getByLabelText("Persistent source"), {
    target: { value: "my Python" },
  });
  state.data = undefined;
  state.loading = true;
  rerender(view());
  expect(
    (screen.getByLabelText("Persistent source") as HTMLInputElement).value,
  ).toBe("my Python");
  expect(
    (screen.getByLabelText("Persistent source") as HTMLInputElement).disabled,
  ).toBe(true);
  state.data = data;
  state.loading = false;
  rerender(view());
  expect(
    (screen.getByLabelText("Persistent source") as HTMLInputElement).value,
  ).toBe("my Python");
});
