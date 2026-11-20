import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { parseFilters } from "@/pages/logs/log-filter-url";
import { OverviewTab } from "./OverviewTab";

const logs = vi.hoisted(() => ({ enabled: true }));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project" }) }));
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
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "example", projectSlug: "test" }),
}));
vi.mock("@/pages/mcp/overview/MCPOverviewTab", () => ({
  MCPOverviewTab: ({
    server,
    toolbarStart,
  }: {
    server: { kind: string; slug: string };
    toolbarStart?: ReactNode;
  }) => (
    <div>
      {toolbarStart}
      <div>Shared usage dashboard</div>
      <div data-testid="scope" data-scope={`${server.kind}:${server.slug}`} />
    </div>
  ),
}));
vi.mock("@/pages/mcp/overview/PluginDistributionLine", () => ({
  PluginDistributionLine: () => <span>Plugins</span>,
}));
vi.mock("./UnproxiedMcpOverviewTab", () => ({
  UnproxiedMcpOverviewTab: ({ header }: { header?: ReactNode }) => (
    <div>
      {header}
      Unproxied overview
    </div>
  ),
}));
vi.mock("./TunneledMcpConnectionsPanel", () => ({
  TunneledMcpConnectionsPanel: ({ logsHref }: { logsHref?: string }) =>
    logsHref ? <a href={logsHref}>Tunnel status</a> : <div>Tunnel status</div>,
}));
afterEach(() => {
  cleanup();
  logs.enabled = true;
});
const server = {
  id: "server",
  slug: "sample",
  name: "Sample",
  tunneledMcpServerId: "tunnel",
} as McpServer;

it("places tunnel status above the usage dashboard", () => {
  const { container } = render(
    <OverviewTab
      mcpServer={server}
      agentSetupHref="/settings"
      pluginSettingsHref="/settings#publishing"
    />,
    { wrapper: MemoryRouter },
  );
  expect(container.textContent).toBe(
    "Tunnel statusPluginsShared usage dashboard",
  );
  const url = new URL(
    screen.getByRole("link").getAttribute("href")!,
    "https://example.test",
  );
  expect(url.pathname).toBe("/example/projects/test/logs");
  expect(parseFilters(url.searchParams.get("af"))).toMatchObject([
    { path: "gram.tunneled_mcp_server.id", op: "eq", value: "tunnel" },
  ]);
});

it("offers no Tool Logs link when the organization does not record logs", () => {
  logs.enabled = false;
  render(
    <OverviewTab
      mcpServer={server}
      agentSetupHref="/settings"
      pluginSettingsHref="/settings#publishing"
    />,
    {
      wrapper: MemoryRouter,
    },
  );
  expect(screen.getByText("Tunnel status")).toBeTruthy();
  expect(screen.queryByRole("link")).toBeNull();
});

it("shows the tunnel status without a usage dashboard when the server has no slug", () => {
  const { container } = render(
    <OverviewTab
      mcpServer={{ ...server, slug: undefined }}
      agentSetupHref="/settings"
      pluginSettingsHref="/settings#publishing"
    />,
    { wrapper: MemoryRouter },
  );
  expect(container.textContent).toBe("PluginsTunnel status");
});

it("shows plugin status above an unproxied server's activity", () => {
  const { container } = render(
    <OverviewTab
      mcpServer={{ ...server, unproxiedMcpServerId: "unproxied" }}
      agentSetupHref="/settings"
      pluginSettingsHref="/settings#publishing"
    />,
    { wrapper: MemoryRouter },
  );
  expect(container.textContent).toBe("PluginsUnproxied overview");
});

it("retains the shared overview for servers without tunnels", () => {
  render(
    <OverviewTab
      mcpServer={{ ...server, tunneledMcpServerId: undefined }}
      agentSetupHref="/settings"
      pluginSettingsHref="/settings#publishing"
    />,
    { wrapper: MemoryRouter },
  );
  expect(screen.getByText("Shared usage dashboard")).toBeTruthy();
  expect(screen.queryByText("Tunnel status")).toBeNull();
});

it.each([
  ["its canonical wrapper", "toolset-1"],
  ["a hosting server that is not the canonical wrapper", "host-1"],
])(
  "scopes a hosted server's overview to its toolset when it is %s",
  (_, id) => {
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
        pluginSettingsHref="/settings#publishing"
      />,
      { wrapper: MemoryRouter },
    );
    expect(screen.getByTestId("scope").getAttribute("data-scope")).toBe(
      "toolset:toolset-slug",
    );
  },
);
