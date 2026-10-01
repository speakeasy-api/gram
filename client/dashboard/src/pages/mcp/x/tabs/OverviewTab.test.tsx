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
vi.mock("@/pages/mcp/overview/MCPOverviewTab", () => ({
  MCPOverviewTab: () => <div>Shared usage dashboard</div>,
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

it("places the marketplace above tunnel status without a second usage dashboard", () => {
  const { container } = render(
    <OverviewTab mcpServer={server} agentSetupHref="/settings" />,
  );
  expect(container.textContent).toBe("MarketplaceTunnel status");
  expect(screen.queryByText("Shared usage dashboard")).toBeNull();
  const url = new URL(
    screen.getByRole("link").getAttribute("href")!,
    "https://example.test",
  );
  expect(url.pathname).toBe("/example/projects/test/logs");
  expect(parseFilters(url.searchParams.get("af"))).toMatchObject([
    { path: "gram.tunneled_mcp_server.id", op: "eq", value: "tunnel" },
  ]);
});

it("retains the usage dashboard when tunnel observability is disabled", () => {
  state.enhanced = false;
  render(<OverviewTab mcpServer={server} agentSetupHref="/settings" />);
  expect(screen.getByText("Shared usage dashboard")).toBeTruthy();
  expect(screen.queryByText("Marketplace")).toBeNull();
});

it("retains the shared overview for servers without tunnels", () => {
  render(
    <OverviewTab
      mcpServer={{ ...server, tunneledMcpServerId: undefined }}
      agentSetupHref="/settings"
    />,
  );
  expect(screen.getByText("Shared usage dashboard")).toBeTruthy();
  expect(screen.queryByText("Tunnel status")).toBeNull();
});
