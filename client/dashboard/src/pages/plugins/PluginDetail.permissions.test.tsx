import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import type { ReactNode } from "react";
import PluginDetail from "./PluginDetail";

const state = vi.hoisted(() => ({
  loading: false,
  orgRead: false,
  orgAdmin: false,
  canReadServers: false,
}));
const adminQuery = vi.hoisted(() => vi.fn());
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: state.loading,
    hasScope: (scope: string) =>
      (scope === "org:read" && state.orgRead) ||
      (scope === "org:admin" && state.orgAdmin),
  }),
}));
vi.mock("./PluginDistributionDetail", () => ({
  PluginDistributionDetail: () => <div>Distribution-only skills</div>,
}));
vi.mock("@gram/client/react-query/plugin", () => ({
  usePluginSuspense: adminQuery,
}));

const assignments = vi.hoisted(() => ({
  roles: vi.fn(() => ({})),
  members: vi.fn(() => ({})),
  audiences: vi.fn(() => ({})),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-a" }),
  useProject: () => ({ id: "project-a" }),
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@/routes", () => ({ useRoutes: () => ({}) }));
vi.mock("@/components/command-palette/recentlyVisited", () => ({
  useRecentLabelOverride: () => {},
}));
vi.mock("@/components/page-layout", () => {
  const Container = ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  );
  return {
    Page: Object.assign(Container, {
      Header: Object.assign(Container, { Breadcrumbs: () => null }),
      Body: Container,
    }),
  };
});
vi.mock("./use-plugin-assignments-visible", () => ({
  usePluginAssignmentsVisible: () => true,
}));
vi.mock("./usePluginServerQueries", () => ({
  usePluginServerQueries: () => ({
    canReadServers: state.canReadServers,
    toolsetsQuery: {},
    serversQuery: {},
    endpointsQuery: {},
  }),
}));
vi.mock("@gram/client/react-query/roles", () => ({
  useRoles: assignments.roles,
}));
vi.mock("@gram/client/react-query/members", () => ({
  useMembers: assignments.members,
}));
vi.mock("@gram/client/react-query/audiences", () => ({
  useAudiences: assignments.audiences,
}));
vi.mock("@gram/client/react-query/syncedAgentUsers.js", () => ({
  useSyncedAgentUsers: () => ({}),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({}),
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => ({}),
}));
vi.mock("@gram/client/react-query/publishPlugins", () => ({
  usePublishPluginsMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/updatePlugin", () => ({
  useUpdatePluginMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/deletePlugin", () => ({
  useDeletePluginMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/addPluginServer", () => ({
  useAddPluginServerMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/removePluginServer", () => ({
  useRemovePluginServerMutation: () => ({ mutate: vi.fn() }),
}));
function renderAdmin() {
  adminQuery.mockReturnValue({
    data: {
      name: "Example plugin",
      slug: "example-plugin",
      servers: [
        {
          id: "server-a",
          displayName: "Example server",
          toolsetId: "toolset-a",
          createdAt: new Date(),
        },
      ],
    },
  });
  return render(
    <TooltipProvider>
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={["/plugins/plugin-a/servers"]}>
          <Routes>
            <Route
              path="/plugins/:pluginId/servers"
              element={<PluginDetail />}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    </TooltipProvider>,
  );
}
afterEach(cleanup);
beforeEach(() => {
  state.loading = false;
  state.orgRead = false;
  state.orgAdmin = false;
  state.canReadServers = false;
  vi.clearAllMocks();
  adminQuery.mockClear();
});

describe("plugin detail permission boundary", () => {
  it("mounts only distribution UI without org:read", () => {
    render(<PluginDetail />);
    expect(screen.getByText("Distribution-only skills")).toBeTruthy();
    expect(adminQuery).not.toHaveBeenCalled();
    expect(screen.queryByText("Publish now")).toBeNull();
  });

  it("does not mount either query tree until grants are loaded", () => {
    state.loading = true;
    const { container } = render(<PluginDetail />);
    expect(container.innerHTML).toBe("");
    expect(adminQuery).not.toHaveBeenCalled();
  });
  it("mounts admin detail but not assignment queries for org readers", () => {
    state.orgRead = true;
    renderAdmin();
    expect(adminQuery).toHaveBeenCalledWith({ id: "plugin-a" });
    expect(screen.getByText("Example server")).toBeTruthy();
    expect(screen.getByText("Server metadata unavailable")).toBeTruthy();
    expect(screen.queryByText("Toolset missing")).toBeNull();
    for (const query of Object.values(assignments))
      expect(query).not.toHaveBeenCalled();
  });
  it("retains assignment queries for org admins", () => {
    state.orgRead = true;
    state.orgAdmin = true;
    renderAdmin();
    expect(adminQuery).toHaveBeenCalled();
    for (const query of Object.values(assignments))
      expect(query).toHaveBeenCalled();
  });
  it("reports missing toolsets only when metadata is available", () => {
    state.orgRead = true;
    state.canReadServers = true;
    renderAdmin();
    expect(screen.getByText("Toolset missing")).toBeTruthy();
    expect(screen.queryByText("Server metadata unavailable")).toBeNull();
  });
});
