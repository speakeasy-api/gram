import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { useState } from "react";
import type { GatewayInspection } from "./useGatewayInspection";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { GatewayInspectTab } from "./GatewayInspectTab";

type MintInput = {
  target: { discoveryMode?: string; connectionVersion?: string };
};

/** The main session's latest mint, skipping the Code Mode catalog session. */
function lastMainMint(): MintInput | undefined {
  return (state.mint.mock.calls as [MintInput][])
    .map(([input]) => input)
    .filter((input) => !input.target.connectionVersion?.endsWith(":catalog"))
    .at(-1);
}

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  mcpConnectionUrl: (value: string) => value,
}));

vi.mock("@/components/ui/CodeSnippet", () => ({
  CodeSnippet: ({ code }: { code: string }) => <pre>{code}</pre>,
}));

const state = vi.hoisted(() => ({
  mint: vi.fn(),
  data: undefined as GatewayInspection | undefined,
  loading: false,
  accessToken: undefined as string | undefined,
}));
vi.mock("./GatewayCodeExecution", () => ({
  GatewayCodeExecution: ({
    disabled,
    headers,
  }: {
    disabled: boolean;
    headers?: Record<string, string>;
  }) => {
    const [source, setSource] = useState("initial");
    return (
      <input
        aria-label="Persistent source"
        data-authorization={headers?.Authorization ?? ""}
        value={source}
        disabled={disabled}
        onChange={(event) => setSource(event.target.value)}
      />
    );
  },
}));
vi.mock("../inspect/useToolHealth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../inspect/useToolHealth")>()),
  useToolHealth: () => ({ healthFor: () => undefined, isLoading: false }),
}));
vi.mock("./useGatewayMemberTools", () => ({
  useGatewayMemberTools: () => [],
}));
vi.mock("@/pages/playground/PlaygroundChat", () => ({
  PlaygroundChat: () => <div data-testid="playground-chat" />,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-test" }),
  useSession: () => ({ user: { id: "user-test" } }),
}));
vi.mock("@/hooks/useUserSessionToken", () => ({
  useUserSessionToken: (input: { target: { discoveryMode?: string } }) => {
    state.mint(input);
    return {
      accessToken: state.accessToken,
      isLoading: false,
      isError: !!input.target.discoveryMode,
      error: input.target.discoveryMode
        ? new Error("Gateway discovery settings are not available")
        : null,
      refetch: vi.fn(),
    };
  },
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  useResolvedMcpServerUrl: () => ({
    mcpUrl: "https://example.com/mcp/gateway",
    loading: false,
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));
vi.mock("./GatewayDetailsRouting", () => ({
  gatewayTabHref: () => "/settings",
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
  return <span aria-label="location">{useLocation().search}</span>;
}
afterEach(() => {
  cleanup();
  state.data = undefined;
  state.loading = false;
  state.accessToken = undefined;
});
it("applies discovery choices to the URL and the minted connection", async () => {
  state.mint.mockClear();
  const gateway = {
    id: "gateway-test",
    userSessionIssuerId: "issuer-test",
    discoveryMode: "progressive",
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
    screen.getByRole("button", { name: "Preview a mode: Progressive" }),
  );
  fireEvent.click(await screen.findByRole("button", { name: /^Direct/ }));
  expect(lastMainMint()).toEqual(
    expect.objectContaining({
      target: expect.objectContaining({ discoveryMode: "direct" }),
    }),
  );
  await waitFor(() =>
    expect(screen.getByLabelText("location").textContent).toBe(
      "?keep=value&discovery_mode=direct",
    ),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Preview a mode: Direct" }),
  );
  fireEvent.click(
    await screen.findByRole("button", { name: /gateway default/ }),
  );
  expect(lastMainMint()).toEqual(
    expect.objectContaining({
      target: expect.objectContaining({ discoveryMode: undefined }),
    }),
  );
  await waitFor(() =>
    expect(screen.getByLabelText("location").textContent).toBe("?keep=value"),
  );
});

it.each([
  ["direct", "Direct"],
  ["progressive", "Progressive"],
  ["code_mode", "Code Mode"],
  ["future_mode", "Progressive"],
  ["constructor", "Progressive"],
  ["__proto__", "Progressive"],
])(
  "labels gateway default %s as %s without choosing an override",
  (mode, label) => {
    state.mint.mockClear();
    render(
      <TooltipProvider>
        <MemoryRouter>
          <GatewayInspectTab
            metaMcpServer={
              {
                id: "gateway-test",
                userSessionIssuerId: "issuer-test",
                discoveryMode: unrecognized(mode),
              } as MetaMcpServer
            }
            endpoints={[]}
            isLoadingEndpoints={false}
          />
        </MemoryRouter>
      </TooltipProvider>,
    );
    expect(
      screen.getByRole("button", { name: `Preview a mode: ${label}` })
        .textContent,
    ).toBe(label);
    expect(lastMainMint()).toEqual(
      expect.objectContaining({
        target: expect.objectContaining({ discoveryMode: undefined }),
      }),
    );
  },
);

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
  state.accessToken = "first-token";
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
  state.accessToken = "refreshed-token";
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
  const source = screen.getByLabelText("Persistent source") as HTMLInputElement;
  expect(source.value).toBe("my Python");
  expect(source.dataset.authorization).toBe("Bearer refreshed-token");
});

it("explains Progressive fallback when the saved Code runtime is unavailable", () => {
  render(
    <TooltipProvider>
      <MemoryRouter>
        <GatewayInspectTab
          metaMcpServer={
            {
              id: "gateway-test",
              discoveryMode: "code_mode",
              codeModeEnabled: false,
            } as MetaMcpServer
          }
          endpoints={[]}
          isLoadingEndpoints={false}
        />
      </MemoryRouter>
    </TooltipProvider>,
  );
  expect(
    screen.getByText(
      /Code Mode isn't available for this gateway, so this connection uses Progressive/i,
    ),
  ).toBeTruthy();
});
