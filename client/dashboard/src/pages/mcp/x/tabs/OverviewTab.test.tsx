import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { parseFilters } from "@/pages/logs/log-filter-url";
import { OverviewTab } from "./OverviewTab";

const logs = vi.hoisted(() => ({ canView: true, enabled: true }));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project" }) }));
vi.mock("@/components/observe/observeDeepLink", () => ({
  useCanViewToolLogs: () => logs.canView,
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
// The Tool Logs probe succeeds when the organization records logs and answers
// 404 (an error) when it does not.
vi.mock("@tanstack/react-query", async (original) => ({
  ...(await original<typeof import("@tanstack/react-query")>()),
  useQuery: ({ enabled }: { enabled?: boolean }) => ({
    isSuccess: enabled !== false && logs.enabled,
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ logs: { href: () => "/example/projects/test/logs" } }),
}));
vi.mock("@/pages/mcp/overview/PluginStatusBanner", () => ({
  PluginStatusBanner: () => <div>Marketplace</div>,
}));
// The real dashboard renders the marketplace banner unless told not to.
vi.mock("@/pages/mcp/overview/MCPOverviewTab", () => ({
  MCPOverviewTab: ({
    showPluginBanner,
    server,
    pluginServer,
  }: {
    showPluginBanner?: boolean;
    server: { kind: string; slug: string };
    pluginServer?: { kind: string; id: string };
  }) => (
    <div>
      {showPluginBanner !== false && <div>Marketplace</div>}
      <div>Shared usage dashboard</div>
      <div
        data-testid="scope"
        data-scope={`${server.kind}:${server.slug}`}
        data-plugin={
          pluginServer ? `${pluginServer.kind}:${pluginServer.id}` : ""
        }
      />
    </div>
  ),
}));
vi.mock("./UnproxiedMcpOverviewTab", () => ({
  UnproxiedMcpOverviewTab: () => <div>Unproxied overview</div>,
}));
vi.mock("./TunneledMcpConnectionsPanel", () => ({
  TunneledMcpConnectionsPanel: ({ logsHref }: { logsHref?: string }) =>
    logsHref ? <a href={logsHref}>Tunnel status</a> : <div>Tunnel status</div>,
}));
afterEach(() => {
  cleanup();
  logs.canView = true;
  logs.enabled = true;
});
const server = {
  id: "server",
  slug: "sample",
  name: "Sample",
  tunneledMcpServerId: "tunnel",
} as McpServer;

it("places the marketplace above tunnel status and keeps the usage dashboard below it", () => {
  const { container } = render(
    <OverviewTab mcpServer={server} agentSetupHref="/settings" />,
  );
  expect(container.textContent).toBe(
    "MarketplaceTunnel statusShared usage dashboard",
  );
  expect(screen.getAllByText("Marketplace")).toHaveLength(1);
  const url = new URL(
    screen.getByRole("link").getAttribute("href")!,
    "https://example.test",
  );
  expect(url.pathname).toBe("/example/projects/test/logs");
  expect(parseFilters(url.searchParams.get("af"))).toMatchObject([
    { path: "gram.tunneled_mcp_server.id", op: "eq", value: "tunnel" },
  ]);
});

it.each([
  ["the organization does not record logs", { canView: true, enabled: false }],
  ["the user cannot read logs", { canView: false, enabled: true }],
])("offers no Tool Logs link when %s", (_, state) => {
  Object.assign(logs, state);
  render(<OverviewTab mcpServer={server} agentSetupHref="/settings" />);
  expect(screen.getByText("Tunnel status")).toBeTruthy();
  expect(screen.queryByRole("link")).toBeNull();
});

it("shows the tunnel status without a usage dashboard when the server has no slug", () => {
  const { container } = render(
    <OverviewTab
      mcpServer={{ ...server, slug: undefined }}
      agentSetupHref="/settings"
    />,
  );
  expect(container.textContent).toBe("MarketplaceTunnel status");
});

it("retains the shared overview for servers without tunnels", () => {
  render(
    <OverviewTab
      mcpServer={{ ...server, tunneledMcpServerId: undefined }}
      agentSetupHref="/settings"
    />,
  );
  expect(screen.getByText("Shared usage dashboard")).toBeTruthy();
  expect(screen.getAllByText("Marketplace")).toHaveLength(1);
  expect(screen.queryByText("Tunnel status")).toBeNull();
});

it.each([
  ["its canonical wrapper", "toolset-1", "toolset:toolset-1"],
  [
    "a hosting server that is not the canonical wrapper",
    "host-1",
    "mcp-server:host-1",
  ],
])(
  "scopes a hosted server's overview to its toolset when it is %s",
  (_, id, plugin) => {
    render(
      <OverviewTab
        mcpServer={
          {
            id,
            slug: "server-slug",
            name: "Server",
            toolsetId: "toolset-1",
            toolset: {
              id: "toolset-1",
              slug: "toolset-slug",
              name: "Tools",
              hostingMcpServerId: id,
            },
          } as McpServer
        }
        agentSetupHref="/settings"
      />,
    );
    expect(screen.getByTestId("scope").getAttribute("data-scope")).toBe(
      "toolset:toolset-slug",
    );
    expect(screen.getByTestId("scope").getAttribute("data-plugin")).toBe(
      plugin,
    );
  },
);
