import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { parseFilters } from "@/pages/logs/log-filter-url";
import { OverviewTab } from "./OverviewTab";

const state = vi.hoisted(() => ({ enhanced: true }));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => state.enhanced }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ logs: { href: () => "/example/projects/test/logs" } }),
}));
vi.mock("@/pages/mcp/overview/PluginStatusBanner", () => ({
  PluginStatusBanner: () => <div>Marketplace</div>,
}));
// The real dashboard renders the marketplace banner unless told not to.
vi.mock("@/pages/mcp/overview/MCPOverviewTab", () => ({
  MCPOverviewTab: ({ showPluginBanner }: { showPluginBanner?: boolean }) => (
    <div>
      {showPluginBanner !== false && <div>Marketplace</div>}
      <div>Shared usage dashboard</div>
    </div>
  ),
}));
vi.mock("./UnproxiedMcpOverviewTab", () => ({
  UnproxiedMcpOverviewTab: () => <div>Unproxied overview</div>,
}));
vi.mock("./TunneledMcpConnectionsPanel", () => ({
  TunneledMcpConnectionsPanel: ({ logsHref }: { logsHref: string }) => (
    <a href={logsHref}>Tunnel status</a>
  ),
}));
afterEach(() => {
  cleanup();
  state.enhanced = true;
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

it("keeps the connections panel above the usage dashboard when tunnel observability is disabled", () => {
  state.enhanced = false;
  const { container } = render(
    <OverviewTab mcpServer={server} agentSetupHref="/settings" />,
  );
  expect(container.textContent).toBe(
    "Tunnel statusMarketplaceShared usage dashboard",
  );
  expect(screen.getAllByText("Marketplace")).toHaveLength(1);
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
