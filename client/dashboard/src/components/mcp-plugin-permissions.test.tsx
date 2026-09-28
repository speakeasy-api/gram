import { ConfigProvider } from "@/components/ui/context/ConfigContext";
import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  grants: [] as { scope: string; selectors?: { resourceId: string }[] }[],
  plugins: vi.fn(() => ({
    data: {
      plugins: [
        {
          id: "plugin-a",
          name: "Cached plugin",
          slug: "cached-plugin",
          servers: [{ mcpServerId: "server-a" }],
        },
      ],
    },
  })),
  publishStatus: vi.fn(() => ({
    data: {
      repoOwner: "example",
      repoName: "plugins",
      marketplaceUrl:
        "https://example.test/marketplace?token=synthetic-admin-token",
    },
  })),
  settings: vi.fn(() => ({ data: { effectiveName: "example-marketplace" } })),
  mutation: vi.fn(() => ({ isPending: false, mutateAsync: vi.fn() })),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-a" }),
  useProject: () => ({ id: "project-a", slug: "project-slug" }),
  useSession: () => ({ session: "session-a" }),
  useIsPlatformAdmin: () => false,
}));
vi.mock("@gram/client/react-query/grants.js", () => ({
  useGrants: () => ({ data: { grants: state.grants }, isLoading: false }),
}));
vi.mock("@gram/client/react-query/plugins", () => ({
  usePlugins: state.plugins,
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: state.publishStatus,
}));
vi.mock("@gram/client/react-query/addPluginServer", () => ({
  useAddPluginServerMutation: state.mutation,
}));
vi.mock("@gram/client/react-query/removePluginServer", () => ({
  useRemovePluginServerMutation: state.mutation,
}));
vi.mock("@gram/client/react-query/publishPlugins", () => ({
  usePublishPluginsMutation: state.mutation,
}));
vi.mock("@gram/client/react-query/marketplaceSettings", () => ({
  useMarketplaceSettings: state.settings,
}));
vi.mock("@/contexts/Fetcher", () => ({
  useFetcher: () => ({ fetch: vi.fn() }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    plugins: {
      href: () => "/plugins",
      Link: ({ children }: { children: ReactNode }) => (
        <a href="/plugins">{children}</a>
      ),
    },
    playground: { Link: () => null },
    mcp: { href: () => "/mcp" },
  }),
}));
vi.mock("react-router", () => ({
  useParams: () => ({ mcpServerSlug: "server-a" }),
  useLocation: () => ({ pathname: "/mcp/server-a" }),
}));
vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  useGetMcpServer: () => ({ data: { id: "server-a", name: "Server" } }),
}));
vi.mock("@gram/client/react-query/getRemoteMcpServer.js", () => ({
  useGetRemoteMcpServer: () => ({}),
}));
vi.mock("@gram/client/react-query/getUnproxiedMcpServer.js", () => ({
  useGetUnproxiedMcpServer: () => ({}),
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: () => ({}),
}));
vi.mock("@/hooks/usePrivateMcpServerUrls", () => ({
  usePrivateMcpServerUrls: () => ({
    privateMcpUrls: [],
    privateInstallPageUrls: [],
  }),
  mcpServerInstallPageLinks: () => [],
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  useResolvedMcpServerUrl: () => ({}),
}));
vi.mock("@/pages/mcp/x/MCPServerDetails", () => ({
  MCPServerStatusDropdown: () => null,
  MCPServerAvailabilityToggle: () => null,
}));
vi.mock("@/pages/mcp/x/MCPServerDetailsRouting", () => ({
  activeTabFromPath: () => "overview",
  mcpServerTabHref: () => "/mcp",
}));
vi.mock(
  "@/pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection",
  () => ({ MCP_AUTHENTICATION_SECTION_ID: "auth" }),
);
vi.mock("@/pages/mcp/x/tabs/settings/sections/ServerUrlSection", () => ({
  MCP_SERVER_URL_SECTION_ID: "url",
}));
vi.mock("@/lib/remote-identity", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/remote-identity")>()),
  useAllRemoteSessionClients: () => ({
    items: [],
    isLoading: false,
    isError: false,
  }),
  useUpstreamProbe: () => "idle",
}));
vi.mock("@gram/client/react-query/remoteMcpServerHeaders.js", () => ({
  useRemoteMcpServerHeaders: () => ({
    data: { headers: [] },
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@/components/sources/SourceCard", () => ({
  SourceMcpIcon: () => null,
}));
vi.mock("@/components/setup-guide/SetupGuideCard", () => ({
  SetupGuideCard: () => null,
}));
vi.mock("@/components/mcp-server-readiness-bar", () => ({
  McpServerReadinessBar: ({ checks }: { checks: { label: string }[] }) => (
    <>
      {checks.map((check) => (
        <span key={check.label}>{check.label}</span>
      ))}
    </>
  ),
}));
vi.mock("@/components/detail/detail-sidebar-nav", () => ({
  DetailSidebarInfoLabel: () => null,
  DetailSidebarNav: ({
    items,
    topContent,
  }: {
    items: { title: string }[];
    topContent: ReactNode;
  }) => (
    <>
      {items.map((item) => (
        <span key={item.title}>{item.title}</span>
      ))}
      {topContent}
    </>
  ),
}));

import { McpServerXSidebarNav } from "./mcp-server-x-sidebar-nav";
import { PluginStatusBanner } from "@/pages/mcp/overview/PluginStatusBanner";

beforeEach(() => {
  vi.clearAllMocks();
  state.grants = [];
});
afterEach(cleanup);
const scope = { gramProject: "project-slug", gramSession: "session-a" };
function banner() {
  const client = new QueryClient();
  const content = () => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <ConfigProvider theme="light" setTheme={() => {}}>
          <PluginStatusBanner
            server={{
              kind: "mcp-server",
              id: "server-a",
              slug: "server-a",
              name: "Server",
            }}
          />
        </ConfigProvider>
      </TooltipProvider>
    </QueryClientProvider>
  );
  const view = render(content());
  return { ...view, refresh: () => view.rerender(content()) };
}
describe("MCP plugin permission boundaries", () => {
  it("unmounts the real token-bearing install dialog on admin-to-writer downgrade", async () => {
    state.grants = [{ scope: "org:admin" }, { scope: "plugin:write" }];
    const view = banner();
    fireEvent.click(screen.getByRole("button", { name: "Install" }));
    fireEvent.click(screen.getByRole("button", { name: "Claude Code" }));
    await waitFor(() =>
      expect(document.body.textContent).toContain("synthetic-admin-token"),
    );
    expect(state.settings).toHaveBeenCalled();
    vi.clearAllMocks();
    state.grants = [{ scope: "plugin:write" }];
    view.refresh();
    expect(document.body.textContent).not.toContain("synthetic-admin-token");
    expect(screen.queryByRole("button", { name: "Install" })).toBeNull();
    expect(state.settings).not.toHaveBeenCalled();
    expect(state.plugins).toHaveBeenCalledWith(
      scope,
      undefined,
      expect.objectContaining({ throwOnError: false }),
    );
    expect(screen.getByRole("button", { name: "Cached plugin" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Update" })).toBeTruthy();
  });

  it("does not mount install queries for a writer who administers another organization", () => {
    state.grants = [
      { scope: "plugin:write" },
      { scope: "org:admin", selectors: [{ resourceId: "org-b" }] },
    ];
    banner();
    expect(state.settings).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Install" })).toBeNull();
    expect(screen.getByRole("button", { name: "Update" })).toBeTruthy();
  });
  it.each(["org:read", "plugin:write", "org:admin"])(
    "scopes sidebar queries for %s",
    (grant) => {
      state.grants = [{ scope: grant }];
      render(<McpServerXSidebarNav />);
      expect(screen.getByText("Included in Plugin")).toBeTruthy();
      expect(state.plugins).toHaveBeenCalledWith(
        scope,
        undefined,
        expect.objectContaining({ enabled: true }),
      );
      expect(state.publishStatus).toHaveBeenCalledWith(
        scope,
        undefined,
        expect.objectContaining({ enabled: true }),
      );
    },
  );
  it.each([false, true])(
    "disables sidebar reads and cached plugin content (org block: %s)",
    (blocked) => {
      state.grants = blocked
        ? [
            { scope: "org:read" },
            { scope: "org:blocked_read", selectors: [{ resourceId: "org-a" }] },
          ]
        : [];
      render(<McpServerXSidebarNav />);
      expect(state.plugins).toHaveBeenCalledWith(
        scope,
        undefined,
        expect.objectContaining({ enabled: false }),
      );
      expect(state.publishStatus).toHaveBeenCalledWith(
        scope,
        undefined,
        expect.objectContaining({ enabled: false }),
      );
      expect(screen.queryByText("Included in Plugin")).toBeNull();
    },
  );
  it("honors org-ID read denial for team access", () => {
    state.grants = [
      { scope: "org:read" },
      { scope: "mcp:read" },
      { scope: "org:blocked_read", selectors: [{ resourceId: "org-a" }] },
    ];
    render(<McpServerXSidebarNav />);
    expect(screen.queryByText("Team Access")).toBeNull();
  });
  it.each([
    { grants: [] },
    { grants: [{ scope: "org:read" }] },
    {
      grants: [
        { scope: "plugin:write" },
        {
          scope: "plugin:blocked_write",
          selectors: [{ resourceId: "project-a" }],
        },
      ],
    },
  ])(
    "does not mount banner queries or mutation controls without write access: %j",
    ({ grants }) => {
      state.grants = grants;
      const view = banner();
      expect(state.plugins).not.toHaveBeenCalled();
      expect(state.publishStatus).not.toHaveBeenCalled();
      expect(state.mutation).not.toHaveBeenCalled();
      expect(view.container.innerHTML).toBe("");
    },
  );
  it.each(["plugin:write", "org:admin"])(
    "scopes writer banner queries for %s",
    (grant) => {
      state.grants = [{ scope: grant }];
      banner();
      expect(state.plugins).toHaveBeenCalledWith(
        scope,
        undefined,
        expect.objectContaining({ throwOnError: false }),
      );
      expect(state.publishStatus).toHaveBeenCalledWith(
        scope,
        undefined,
        expect.objectContaining({ refetchInterval: 5000 }),
      );
      expect(state.mutation).toHaveBeenCalled();
      if (grant === "org:admin") {
        expect(screen.getByRole("button", { name: "Install" })).toBeTruthy();
      } else {
        expect(screen.queryByRole("button", { name: "Install" })).toBeNull();
        expect(state.settings).not.toHaveBeenCalled();
      }
    },
  );
});
