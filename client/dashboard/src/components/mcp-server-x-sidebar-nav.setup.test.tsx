import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { McpServerXSidebarNav } from "./mcp-server-x-sidebar-nav";

const state = vi.hoisted(() => ({
  rollout: "enabled" as "enabled" | "disabled" | "loading",
  canWrite: true,
  url: "https://mcp.slack.com/mcp",
}));
vi.mock("@/hooks/useProviderSetupRollout", () => ({
  useProviderSetupRollout: () => state.rollout,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org" }),
  useProject: () => ({ id: "project" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "example", projectSlug: "default" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) => scope === "mcp:write" && state.canWrite,
    hasAnyScope: () => false,
  }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "disabled" }),
}));
vi.mock("@/hooks/usePluginWriteAccess", () => ({
  usePluginWriteAccess: () => false,
}));
vi.mock("@/pages/plugins/usePluginQueryScope", () => ({
  usePluginQueryScope: () => ({}),
}));
vi.mock("@/pages/skills/use-skills-over-mcp", () => ({
  useSkillsOverMcpEnabled: () => false,
}));
vi.mock("@/hooks/useUpstreamAuthEnforcement", () => ({
  useUpstreamAuthEnforcement: () => "legacy",
}));
vi.mock("@/hooks/usePrivateMcpServerUrls", () => ({
  usePrivateMcpServerUrls: () => ({
    privateMcpUrls: [],
    privateInstallPageUrls: [],
    canReadPrivateUrls: false,
  }),
  mcpServerInstallPageLinks: () => [],
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  useResolvedMcpServerUrl: () => ({}),
}));
vi.mock("@/hooks/toolTypes", () => ({ useToolset: () => ({}) }));
vi.mock("@/hooks/useMissingEnvironmentVariables", () => ({
  useMissingRequiredEnvVars: () => 0,
}));
vi.mock("@/pages/mcp/x/MCPServerDetails", () => ({
  MCPServerAvailabilityToggle: () => null,
  MCPServerStatusDropdown: () => null,
}));
vi.mock("@/pages/mcp/x/tabs/tunnelStatus", () => ({
  useTunneledMcpConnections: () => ({}),
  tunnelAgentReadiness: () => ({}),
}));
vi.mock("@/lib/remote-identity", () => ({
  useAllRemoteSessionClients: () => ({ items: [] }),
  useUpstreamProbe: () => "unknown",
  deriveIdentityMode: () => "none",
  findPassThroughAuthorizationHeader: () => undefined,
  IdentityExplainerDialog: () => null,
}));
vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  useGetMcpServer: () => ({
    data: { id: "server", slug: "slack", remoteMcpServerId: "remote" },
  }),
}));
vi.mock("@gram/client/react-query/getRemoteMcpServer.js", () => ({
  useGetRemoteMcpServer: () => ({ data: { url: state.url } }),
}));
vi.mock("@gram/client/react-query/getUnproxiedMcpServer.js", () => ({
  useGetUnproxiedMcpServer: () => ({}),
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: () => ({ data: { mcpEndpoints: [] } }),
}));
vi.mock("@gram/client/react-query/remoteMcpServerHeaders.js", () => ({
  useRemoteMcpServerHeaders: () => ({}),
}));
vi.mock("@gram/client/react-query/plugins", () => ({ usePlugins: () => ({}) }));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => ({}),
}));
vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: () => ({}),
}));
vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: () => ({}),
}));
vi.mock("@gram/client/react-query/mcpServerAuthentication.js", () => ({
  useMcpServerAuthentication: () => ({}),
}));
vi.mock("@/components/detail/detail-sidebar-nav", () => ({
  DetailSidebarInfoLabel: () => null,
  DetailSidebarNav: ({
    items,
  }: {
    items: { key: string; title: string; href: string }[];
  }) => (
    <nav>
      {items.map((item) => (
        <a key={item.key} href={item.href}>
          {item.title}
        </a>
      ))}
    </nav>
  ),
}));
function Sidebar() {
  return (
    <MemoryRouter
      initialEntries={["/example/projects/default/mcp/slack/overview"]}
    >
      <Routes>
        <Route
          path="/example/projects/default/mcp/:mcpServerSlug/:tab"
          element={<McpServerXSidebarNav />}
        />
      </Routes>
    </MemoryRouter>
  );
}
afterEach(cleanup);
beforeEach(() => {
  state.rollout = "enabled";
  state.canWrite = true;
  state.url = "https://mcp.slack.com/mcp";
});
it.each(["disabled", "loading"] as const)(
  "hides Setup while the rollout is %s",
  (status) => {
    state.rollout = status;
    render(<Sidebar />);
    expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
    expect(screen.getByRole("link", { name: "Overview" })).toBeDefined();
  },
);
it("shows Setup only after the rollout resolves enabled, and removes it when disabled", () => {
  state.rollout = "loading";
  const view = render(<Sidebar />);
  expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
  state.rollout = "enabled";
  view.rerender(<Sidebar />);
  expect(screen.getByRole("link", { name: "Setup" }).getAttribute("href")).toBe(
    "/example/projects/default/mcp/slack/setup",
  );
  state.rollout = "disabled";
  view.rerender(<Sidebar />);
  expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
});
it("retains the write permission requirement when the rollout is enabled", () => {
  state.canWrite = false;
  render(<Sidebar />);
  expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
});
it("does not offer Setup for unsupported providers", () => {
  state.url = "https://other.example/mcp";
  render(<Sidebar />);
  expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
});
