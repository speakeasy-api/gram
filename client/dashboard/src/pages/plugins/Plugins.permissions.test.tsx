import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
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
  invalidateAllPlugins: vi.fn(),
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatusSuspense: () => ({
    data: {
      configured: true,
      connected: true,
      repoUrl: "https://example.com/repo",
      marketplaceUrl: "https://example.com/private-token",
    },
  }),
  invalidateAllPublishStatus: vi.fn(),
}));
vi.mock("@gram/client/react-query/marketplaceSettings", () => ({
  useMarketplaceSettingsSuspense: state.settingsRead,
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
  ResourceListPage: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
}));
vi.mock("@/components/filters", async (original) => ({
  ...(await original<typeof import("@/components/filters")>()),
  useFilterState: () => ({ values: {} }),
}));
vi.mock("./PluginCard", () => ({ PluginCard: () => <div>Plugin card</div> }));
vi.mock("./MarketplaceCard", () => ({
  MarketplaceCard: ({
    onSync,
    onRename,
  }: {
    onSync?: () => void;
    onRename?: () => void;
  }) => (
    <>
      <button onClick={onSync}>Sync marketplace</button>
      {onRename && <button onClick={onRename}>Rename marketplace</button>}
    </>
  ),
  UninitializedMarketplaceCard: () => null,
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
    <TooltipProvider>
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <Plugins />
        </MemoryRouter>
      </QueryClientProvider>
    </TooltipProvider>
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
  state.list.mockReturnValue({ data: { plugins: [] } });
});
describe("Plugins index authorization", () => {
  it("unmounts cached marketplace settings after an admin becomes a writer", () => {
    state.admin = true;
    const view = render(page());
    expect(state.settingsRead).toHaveBeenCalled();
    state.admin = false;
    state.settingsRead.mockClear();
    view.rerender(page());
    expect(state.settingsRead).not.toHaveBeenCalled();
    expect(screen.queryByText("Rename marketplace")).toBeNull();
    expect(screen.queryByText("Sync marketplace")).toBeNull();
    expect(screen.queryByText("Observability")).toBeNull();
    expect(view.container.innerHTML).not.toContain("private-token");
  });
  it("loads and offers creation and publishing for a writer without org read", () => {
    render(page());
    expect(state.list).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Sync plugins" }));
    expect(state.publish).toHaveBeenCalled();
    fireEvent.click(screen.getByText("New Plugin"));
    expect(screen.getByRole("heading", { name: "Create Plugin" })).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Example plugin" },
    });
    fireEvent.submit(screen.getByLabelText("Name").closest("form")!);
    expect(state.create).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          createPluginForm: { name: "Example plugin", description: undefined },
        },
      }),
    );
    expect(screen.queryByText("Rename marketplace")).toBeNull();
    expect(state.settings).not.toHaveBeenCalled();
    expect(state.settingsRead).not.toHaveBeenCalled();
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
