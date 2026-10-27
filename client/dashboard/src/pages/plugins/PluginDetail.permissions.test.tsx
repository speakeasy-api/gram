import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import type { ReactNode } from "react";
import PluginDetail from "./PluginDetail";
vi.mock("@gram/client/react-query/requestAccess", () => ({
  useRequestAccessMutation: () => ({ mutateAsync: vi.fn() }),
}));

const state = vi.hoisted(() => ({
  loading: false,
  orgRead: false,
  pluginWrite: false,
  orgAdmin: false,
  canReadServers: false,
}));
vi.mock("@/hooks/usePluginWriteAccess", () => ({
  usePluginWriteAccess: () => state.orgAdmin || state.pluginWrite,
}));
const features = vi.hoisted(() => vi.fn(() => ({})));
const publish = vi.hoisted(() => vi.fn());
const update = vi.hoisted(() => vi.fn());
const remove = vi.hoisted(() => vi.fn());
const deletion = vi.hoisted(() => ({
  success: undefined as (() => Promise<void>) | undefined,
  invalidate: vi.fn(),
}));
const adminQuery = vi.hoisted(() => vi.fn());
const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock("sonner", () => ({
  toast: Object.assign(vi.fn(), { success: toastSuccess, error: vi.fn() }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: state.loading,
    hasAnyScope: () => false,
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
  invalidateAllPlugin: deletion.invalidate,
}));

const assignments = vi.hoisted(() => ({
  roles: vi.fn(() => ({})),
  members: vi.fn(() => ({})),
  audiences: vi.fn(() => ({})),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "session-a" }),
  useOrganization: () => ({ id: "org-a" }),
  useProject: () => ({ id: "project-a", slug: "project-a" }),
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    plugins: {
      href: () => "/plugins",
      detail: {
        overview: { href: (id: string) => `/plugins/${id}/overview` },
        servers: { href: (id: string) => `/plugins/${id}/servers` },
        skills: { href: (id: string) => `/plugins/${id}/skills` },
        assignments: { href: (id: string) => `/plugins/${id}/assignments` },
        settings: { href: (id: string) => `/plugins/${id}/settings` },
      },
    },
  }),
}));
vi.mock("@/components/command-palette/recentlyVisited", () => ({
  useRecentLabelOverride: () => {},
}));
vi.mock("@/components/page-layout", async (importOriginal) => {
  const { Page: ActualPage } =
    await importOriginal<typeof import("@/components/page-layout")>();
  const Container = ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  );
  return {
    Page: Object.assign(Container, {
      Toolbar: ActualPage.Toolbar,
      Header: Object.assign(Container, { Breadcrumbs: () => null }),
      Body: Container,
    }),
  };
});
vi.mock("./use-plugin-assignments-visible", () => ({
  usePluginAssignmentsVisible: () => true,
}));
const serverData = vi.hoisted(() => ({
  toolsets: undefined as unknown,
  mcpServers: undefined as unknown,
  mcpEndpoints: undefined as unknown,
}));
vi.mock("./usePluginServerQueries", () => ({
  usePluginServerQueries: () => ({
    canReadServers: state.canReadServers,
    toolsetsQuery: { data: serverData.toolsets },
    serversQuery: { data: serverData.mcpServers },
    endpointsQuery: { data: serverData.mcpEndpoints },
  }),
}));
vi.mock("@/components/sources/SourceCard", () => ({
  SourceMcpIcon: () => null,
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
  useProductFeatures: features,
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  invalidateAllPublishStatus: vi.fn(),
  usePublishStatus: () => ({ data: { connected: true, configured: true } }),
}));
vi.mock("@gram/client/react-query/publishPlugins", () => ({
  usePublishPluginsMutation: () => ({ mutate: publish }),
}));
vi.mock("@gram/client/react-query/updatePlugin", () => ({
  useUpdatePluginMutation: () => ({ mutate: update }),
}));
vi.mock("@gram/client/react-query/deletePlugin", () => ({
  useDeletePluginMutation: (options: { onSuccess: () => Promise<void> }) => {
    deletion.success = options.onSuccess;
    return { mutate: remove };
  },
}));
const addServer = vi.hoisted(() => vi.fn());
vi.mock("@gram/client/react-query/addPluginServer", () => ({
  useAddPluginServerMutation: () => ({ mutateAsync: addServer }),
}));
vi.mock("@gram/client/react-query/removePluginServer", () => ({
  useRemovePluginServerMutation: () => ({ mutate: vi.fn() }),
}));
const hostedServer = {
  id: "server-a",
  displayName: "Example server",
  toolsetId: "toolset-a",
  mcpServerId: "toolset-a",
  createdAt: new Date(),
};
function renderAdmin(section = "overview", servers: object[] = [hostedServer]) {
  adminQuery.mockReturnValue({
    data: { name: "Example plugin", slug: "example-plugin", servers },
  });
  return render(
    <TooltipProvider>
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={[`/plugins/plugin-a/${section}`]}>
          <Routes>
            <Route path="/plugins" element={<h1>Plugins</h1>} />
            <Route
              path="/plugins/:pluginId/:section"
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
  state.pluginWrite = false;
  state.orgAdmin = false;
  state.canReadServers = false;
  serverData.toolsets = undefined;
  serverData.mcpServers = undefined;
  serverData.mcpEndpoints = undefined;
  vi.clearAllMocks();
  adminQuery.mockClear();
});

vi.mock("@gram/client/react-query/plugins", () => ({
  invalidateAllPlugins: vi.fn(),
}));
describe("plugin detail permission boundary", () => {
  it("does not refetch the deleted plugin before navigation", async () => {
    state.pluginWrite = true;
    renderAdmin("settings");
    await act(async () => {
      await deletion.success!();
    });
    expect(deletion.invalidate).toHaveBeenCalledWith(expect.any(QueryClient), {
      refetchType: "none",
    });
    // A connected marketplace republishes on its own, so the toast must not
    // ask for a manual publish.
    expect(toastSuccess).toHaveBeenCalledWith("Plugin deleted", {
      description: "Changes sync to your marketplace automatically.",
    });
    expect(publish).not.toHaveBeenCalled();
  });
  it("allows writer-only edit and delete actions", () => {
    state.pluginWrite = true;
    renderAdmin("settings");
    fireEvent.click(screen.getByRole("button", { name: "Edit details" }));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Renamed plugin" },
    });
    fireEvent.submit(screen.getByLabelText("Name").closest("form")!);
    expect(update).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete plugin" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(remove).toHaveBeenCalled();
    for (const query of Object.values(assignments))
      expect(query).not.toHaveBeenCalled();
  });
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
    expect(adminQuery).toHaveBeenCalledWith({
      id: "plugin-a",
      gramProject: "project-a",
      gramSession: "session-a",
    });
    expect(screen.getByText("Example server")).toBeTruthy();
    expect(screen.getByText("Server metadata unavailable")).toBeTruthy();
    for (const query of Object.values(assignments))
      expect(query).not.toHaveBeenCalled();
  });
  it("allows plugin writers to manage references without assignment queries", () => {
    state.pluginWrite = true;
    state.canReadServers = true;
    renderAdmin();
    expect(screen.getByRole("button", { name: "Add Server" })).toBeTruthy();
    for (const query of Object.values(assignments))
      expect(query).not.toHaveBeenCalled();
  });
  it("adds several servers at once and reports each failure on its row", async () => {
    state.pluginWrite = true;
    state.canReadServers = true;
    serverData.toolsets = {
      toolsets: [
        {
          id: "toolset-a",
          name: "Already added",
          slug: "already-added",
          tools: [],
        },
        {
          id: "toolset-b",
          name: "Billing",
          slug: "billing",
          mcpEnabled: true,
          mcpSlug: "billing",
          tools: [],
        },
        {
          id: "toolset-c",
          name: "Payroll",
          slug: "payroll",
          mcpEnabled: false,
          tools: [],
        },
        {
          id: "toolset-d",
          name: "Expenses",
          slug: "expenses",
          mcpEnabled: true,
          tools: [],
        },
        {
          id: "toolset-e",
          name: "Ledger",
          slug: "ledger",
          mcpEnabled: true,
          mcpSlug: "ledger",
          tools: [],
        },
      ],
    };
    serverData.mcpServers = {
      mcpServers: [
        // Billing's canonical MCP server; Ledger has none.
        {
          id: "toolset-b",
          name: "Billing",
          slug: "billing",
          toolsetId: "toolset-b",
          visibility: "private",
        },
        {
          id: "remote-a",
          name: "Linear",
          slug: "linear",
          remoteMcpServerId: "r-a",
          visibility: "private",
        },
        {
          id: "remote-b",
          name: "Notion",
          slug: "notion",
          remoteMcpServerId: "r-b",
          visibility: "private",
        },
      ],
    };
    serverData.mcpEndpoints = {
      mcpEndpoints: [{ mcpServerId: "remote-a" }, { mcpServerId: "remote-b" }],
    };
    addServer.mockImplementation(
      async (args: {
        request: { addPluginServerForm: { mcpServerId?: string } };
      }) => {
        if (args.request.addPluginServerForm.mcpServerId === "remote-b")
          throw new Error("Server is not reachable");
      },
    );
    renderAdmin();
    fireEvent.click(screen.getByRole("button", { name: "Add Server" }));
    // Servers already in the plugin are not offered.
    expect(
      screen.queryByRole("checkbox", { name: "Already added" }),
    ).toBeNull();
    // The backend refuses hosted servers with MCP off, so they can't be picked.
    expect(
      (screen.getByRole("checkbox", { name: "Payroll" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(screen.getByRole("img", { name: "MCP off" })).toBeTruthy();
    expect(screen.getByRole("img", { name: "No MCP address" })).toBeTruthy();
    expect(
      screen.getByText(
        "Set an MCP address for this server before adding it to a plugin.",
      ),
    ).toBeTruthy();
    // A hosted toolset without a canonical MCP server is rejected by the API too.
    expect(
      (screen.getByRole("checkbox", { name: "Ledger" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(screen.getByRole("img", { name: "No MCP server" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Select all" }));
    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", { name: "Add selected servers" }),
      );
    });
    expect(addServer).toHaveBeenCalledTimes(3);
    expect(
      addServer.mock.calls.map(
        ([args]) =>
          (
            args as {
              request: {
                addPluginServerForm: {
                  toolsetId?: string;
                  mcpServerId?: string;
                };
              };
            }
          ).request.addPluginServerForm,
      ),
    ).toEqual([
      expect.objectContaining({
        toolsetId: "toolset-b",
        displayName: "Billing",
      }),
      expect.objectContaining({
        mcpServerId: "remote-a",
        displayName: "Linear",
      }),
      expect.objectContaining({
        mcpServerId: "remote-b",
        displayName: "Notion",
      }),
    ]);
    expect(screen.getByText("Notion: Server is not reachable")).toBeTruthy();
    expect(
      (
        screen.getByRole("checkbox", { name: "Notion" }) as HTMLElement
      ).getAttribute("data-state"),
    ).toBe("checked");
  });
  it.each(["overview", "servers", "skills", "assignments", "settings"])(
    "loads writer-only %s without assignment queries",
    (section) => {
      state.pluginWrite = true;
      renderAdmin(section);
      if (section === "overview" || section === "servers") {
        expect(screen.getByText("Overview")).toBeTruthy();
        expect(screen.getByText("Example server")).toBeTruthy();
      }
      expect(adminQuery).toHaveBeenCalled();
      expect(features).not.toHaveBeenCalled();
      expect(screen.queryByText("Distribution-only skills")).toBeNull();
      for (const query of Object.values(assignments))
        expect(query).not.toHaveBeenCalled();
    },
  );
  it("allows publishing with plugin write without granting admin queries", () => {
    state.pluginWrite = true;
    renderAdmin("overview");
    fireEvent.click(screen.getByRole("button", { name: "Sync" }));
    expect(publish).toHaveBeenCalled();
    for (const query of Object.values(assignments))
      expect(query).not.toHaveBeenCalled();
  });
  it("does not offer MCP discovery to plugin-only writers", () => {
    state.pluginWrite = true;
    renderAdmin();
    expect(screen.queryByRole("button", { name: "Add Server" })).toBeNull();
  });
  it("hides publishing from read-only full-editor users", () => {
    state.orgRead = true;
    renderAdmin("overview");
    expect(screen.queryByRole("button", { name: "Sync" })).toBeNull();
    expect(publish).not.toHaveBeenCalled();
  });
  it("retains assignment queries for org admins", () => {
    state.orgRead = true;
    state.orgAdmin = true;
    renderAdmin();
    expect(adminQuery).toHaveBeenCalled();
    for (const query of Object.values(assignments))
      expect(query).toHaveBeenCalled();
  });
  it("labels gateway-backed members as gateways", () => {
    state.orgRead = true;
    state.canReadServers = true;
    renderAdmin("overview", [
      {
        id: "server-g",
        displayName: "Example gateway",
        metaMcpServerId: "gateway-a",
        createdAt: new Date(),
      },
    ]);
    expect(screen.getByText("Gateway")).toBeTruthy();
    expect(screen.queryByText("Remote MCP")).toBeNull();
  });
});

vi.mock("@/components/sources/SourceCard", () => ({
  SourceMcpIcon: () => null,
}));
vi.mock("@/components/mcp/MCPToolsetIcon", () => ({
  MCPToolsetIcon: () => null,
}));
