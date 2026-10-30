import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { ConfigProvider } from "@/components/ui/context/ConfigContext";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import type { ReactNode } from "react";
const state = vi.hoisted(() => ({
  writer: true,
  admin: false,
  read: false,
  loading: false,
  list: vi.fn(),
  create: vi.fn(),
  publish: vi.fn(),
  settings: vi.fn(),
  settingsRead: vi.fn(),
}));
vi.mock("@gram/client/react-query/organizationGateway.js", () => ({
  useOrganizationGateway: () => ({
    data: state.read
      ? { available: true, url: "https://gateway.example.test/mcp" }
      : undefined,
  }),
}));
vi.mock("@/hooks/usePluginWriteAccess", () => ({
  usePluginWriteAccess: () => state.writer || state.admin,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: state.loading,
    hasAnyScope: () => state.admin,
    hasScope: (scope: string) =>
      scope === "org:admin" ? state.admin : scope === "org:read" && state.read,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "session-a" }),
  useProject: () => ({ id: "project-a", slug: "project-a" }),
  useOrganization: () => ({ id: "org-a" }),
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@/contexts/Fetcher", () => ({
  useFetcher: () => ({ fetch: vi.fn() }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    plugins: { detail: { href: (id: string) => `/plugins/${id}` } },
  }),
}));
vi.mock("@gram/client/react-query/plugins", () => ({
  usePluginsSuspense: state.list,
  usePlugins: state.list,
  invalidateAllPlugins: vi.fn(),
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatusSuspense: () => ({
    data: {
      configured: true,
      connected: true,
      hasCollaborators: true,
      upToDate: false,
      repoOwner: "example",
      repoName: "plugins",
      repoUrl: "https://example.com/repo",
      marketplaceUrl: "https://example.com/private-token",
    } satisfies PublishStatusResult,
  }),
  invalidateAllPublishStatus: vi.fn(),
}));
vi.mock("@gram/client/react-query/marketplaceSettings", () => ({
  useMarketplaceSettingsSuspense: state.settingsRead,
  useMarketplaceSettings: () => ({ data: { effectiveName: "example" } }),
  invalidateAllMarketplaceSettings: vi.fn(),
}));
vi.mock("@gram/client/react-query/createPlugin", () => ({
  useCreatePluginMutation: () => ({ mutate: state.create }),
}));
vi.mock("@gram/client/react-query/publishPlugins", () => ({
  usePublishPluginsMutation: () => ({ mutate: state.publish }),
}));
vi.mock("@gram/client/react-query/updateMarketplaceSettings", () => ({
  useUpdateMarketplaceSettingsMutation: () => ({ mutate: state.settings }),
}));
vi.mock("@/components/page-templates", () => ({
  ResourceListPage: ({
    children,
    primaryAction,
  }: {
    children: ReactNode;
    primaryAction?: ReactNode;
  }) => (
    <div>
      {primaryAction}
      {children}
    </div>
  ),
}));
vi.mock("@/components/filters", async (original) => ({
  ...(await original<typeof import("@/components/filters")>()),
  useFilterState: () => ({ values: { servers: [] } }),
}));
vi.mock("./PublishDialog", () => ({ PublishDialog: () => null }));
vi.mock("../setup/components/platform-instrumentation-sheet", () => ({
  PlatformInstrumentationSheet: () => null,
}));
vi.mock("../org/PlatformMCP", () => ({
  PlatformMCPOnboardingContent: () => null,
}));
import Plugins from "./Plugins";
function page() {
  return (
    <ConfigProvider theme="light" setTheme={() => undefined}>
      <TooltipProvider>
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter>
            <Plugins />
          </MemoryRouter>
        </QueryClientProvider>
      </TooltipProvider>
    </ConfigProvider>
  );
}
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.writer = true;
  state.admin = false;
  state.read = false;
  state.loading = false;
  state.settingsRead.mockReturnValue({
    data: { defaultName: "Example", observabilityEnabled: false },
  });
  state.list.mockReturnValue({
    data: {
      plugins: [
        {
          id: "plugin-a",
          name: "Example plugin",
          slug: "example-plugin",
          description: "A populated plugin card",
          createdAt: new Date("2026-01-01"),
          updatedAt: new Date("2026-01-01"),
          serverCount: 1,
          skillCount: 0,
          agentPluginsV1Compatible: true,
          autoCreated: false,
          autoSyncEnabled: true,
        } satisfies Plugin,
      ],
    },
  });
});
describe("Plugins index authorization", () => {
  it("removes a rendered private install URL and marketplace settings after admin downgrade", async () => {
    state.admin = true;
    const view = render(page());
    expect(state.settingsRead).toHaveBeenCalled();
    const marketplaceToggle = screen.getByRole("button", {
      name: "Toggle project marketplace details",
    });
    expect(marketplaceToggle.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByRole("button", { name: "Sync changes" })).toBeNull();
    fireEvent.click(marketplaceToggle);
    expect(marketplaceToggle.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByRole("button", { name: "Sync changes" })).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Rename marketplace" }),
    ).toBeTruthy();
    fireEvent.pointerDown(
      screen.getAllByRole("button", { name: "Install" })[0]!,
      { button: 0, ctrlKey: false },
    );
    fireEvent.click(
      screen.getByRole("menuitem", { name: "GitHub installation (preferred)" }),
    );
    fireEvent.click(await screen.findByRole("button", { name: "Claude Code" }));
    expect(document.body.innerHTML).toContain(
      "https://example.com/private-token",
    );
    state.admin = false;
    state.settingsRead.mockClear();
    view.rerender(page());
    expect(state.settingsRead).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("button", { name: "Rename marketplace" }),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "Sync changes" })).toBeNull();
    expect(screen.queryByText("Observability")).toBeNull();
    // The install sheet is portalled outside the page container.
    expect(document.body.innerHTML).not.toContain("private-token");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByText("A populated plugin card")).toBeTruthy();
    fireEvent.pointerDown(screen.getByRole("button", { name: "Install" }), {
      button: 0,
      ctrlKey: false,
    });
    expect(
      screen
        .getByRole("menuitem", {
          name: "GitHub installation (preferred) Requires marketplace setup",
        })
        .getAttribute("data-disabled"),
    ).not.toBeNull();
    expect(document.body.innerHTML).not.toContain("private-token");
  });
  it("loads and offers creation and publishing for a writer without org read", () => {
    render(page());
    expect(state.list).toHaveBeenCalled();
    expect(
      screen.queryByRole("button", { name: "Connect all servers" }),
    ).toBeNull();
    expect(screen.getByText("A populated plugin card")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Sync plugins" }));
    expect(state.publish).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Create plugin" }));
    expect(screen.getByRole("heading", { name: "Create Plugin" })).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Example plugin" },
    });
    fireEvent.submit(screen.getByLabelText("Name").closest("form")!);
    expect(state.create).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          createPluginForm: {
            name: "Example plugin",
            description: undefined,
            autoSyncEnabled: true,
          },
        },
      }),
    );
    expect(
      screen.queryByRole("button", { name: "Rename marketplace" }),
    ).toBeNull();
    expect(state.settings).not.toHaveBeenCalled();
    expect(state.settingsRead).not.toHaveBeenCalled();
  });
  it("creates a plugin with auto sync off when the switch is turned off", () => {
    render(page());
    fireEvent.click(screen.getByRole("button", { name: "Create plugin" }));
    const toggle = screen.getByRole("switch", {
      name: "Sync changes automatically",
    });
    expect(toggle.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(toggle);
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Staged plugin" },
    });
    fireEvent.submit(screen.getByLabelText("Name").closest("form")!);
    expect(state.create).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          createPluginForm: expect.objectContaining({
            name: "Staged plugin",
            autoSyncEnabled: false,
          }),
        },
      }),
    );
  });
  it("tells admins which plugins wait for Sync changes", () => {
    state.admin = true;
    state.list.mockReturnValue({
      data: {
        plugins: [
          {
            id: "plugin-a",
            name: "Staged plugin",
            slug: "staged-plugin",
            createdAt: new Date("2026-01-01"),
            updatedAt: new Date("2026-01-01"),
            agentPluginsV1Compatible: true,
            autoCreated: false,
            autoSyncEnabled: false,
          } satisfies Plugin,
        ],
      },
    });
    render(page());
    fireEvent.click(
      screen.getByRole("button", {
        name: "Toggle project marketplace details",
      }),
    );
    expect(
      screen.getByText(
        "Auto sync is off for Staged plugin, so its changes publish only when someone clicks Sync changes.",
      ),
    ).toBeTruthy();
  });
  it("does not mount index queries without plugin read access", () => {
    state.writer = false;
    render(page());
    expect(state.list).not.toHaveBeenCalled();
  });
  it("does not mount index queries while grants load", () => {
    state.loading = true;
    render(page());
    expect(state.list).not.toHaveBeenCalled();
  });
});

vi.mock("@gram/client/react-query/plugin", () => ({
  usePlugin: () => ({ data: undefined }),
}));
