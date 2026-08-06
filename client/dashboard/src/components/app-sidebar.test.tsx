import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => {
  const route = {
    active: false,
    href: () => "/",
    title: "Route",
    Icon: () => null,
    details: { active: false },
    detail: { active: false },
    x: { active: false },
    builtIn: { active: false },
  };
  const routes = new Proxy({}, { get: () => route });

  return {
    routes,
    useSession: vi.fn(),
    useSidebar: () => ({ state: "expanded" }),
  };
});

vi.mock("@/contexts/Auth", () => ({ useSession: mocks.useSession }));
vi.mock("./enterprise-trial-status-card", () => ({
  EnterpriseTrialStatusCard: () =>
    mocks.useSession().enterpriseTrial ? (
      <div role="group" aria-label="Enterprise trial" />
    ) : null,
}));
vi.mock("@/contexts/Sdk", () => ({ useSlugs: () => ({ orgSlug: "org" }) }));
vi.mock("@/components/ui/Sidebar/sidebar-context", () => ({
  useSidebar: mocks.useSidebar,
}));
vi.mock("@/hooks/useRBAC", () => ({ useRBAC: () => ({ isLoading: false }) }));
vi.mock("@/hooks/useProductTier", () => ({ useProductTier: () => "pro" }));
vi.mock("@/hooks/useProjectNavRoutes", () => ({
  useProjectNavRoutes: () => [],
}));
vi.mock("@/routes", () => ({
  useRoutes: () => mocks.routes,
  useOrgRoutes: () => mocks.routes,
}));
vi.mock("@gram/client/react-query/getPeriodUsage.js", () => ({
  useGetPeriodUsage: () => ({ data: undefined }),
}));
vi.mock("@/components/nav-menu", () => ({
  NavButton: () => null,
  NavGroupProvider: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("@/components/scope-gated-nav-group", () => ({
  ScopeGatedNavGroup: () => null,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("@/components/ui/Sidebar", () => {
  const Container = ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  );
  return {
    Sidebar: Container,
    SidebarContent: Container,
    SidebarFooter: Container,
    SidebarHeader: Container,
    SidebarMenu: Container,
    SidebarMenuItem: Container,
  };
});
vi.mock("./gram-logo", () => ({ GramLogo: () => null }));
vi.mock("./command-palette/CommandPaletteTrigger", () => ({
  CommandPaletteTrigger: () => null,
}));
vi.mock("./workspace-switcher", () => ({ WorkspaceSwitcher: () => null }));
vi.mock("./insights-dock-resume-button", () => ({
  InsightsDockResumeButton: () => null,
}));
vi.mock("./built-in-mcp-sidebar-nav", () => ({
  BuiltInMcpSidebarNav: () => null,
}));
vi.mock("./mcp-detail-sidebar-nav", () => ({
  McpDetailSidebarNav: () => null,
}));
vi.mock("./mcp-server-x-sidebar-nav", () => ({
  McpServerXSidebarNav: () => null,
}));
vi.mock("./plugin-detail-sidebar-nav", () => ({
  PluginDetailSidebarNav: () => null,
}));
vi.mock("./skill-detail-sidebar-nav", () => ({
  SkillDetailSidebarNav: () => null,
}));
vi.mock("./onboarding-resume-button", () => ({
  OnboardingResumeButton: () => null,
}));
vi.mock("./sidebar-footer-action", () => ({ SidebarFooterAction: () => null }));
vi.mock("./sidebar-user-menu", () => ({ SidebarUserMenu: () => null }));
vi.mock("./FeatureRequestModal", () => ({ FeatureRequestModal: () => null }));
vi.mock("./ui/Button", () => ({ Button: () => null }));
vi.mock("@/components/ui/Icon", () => ({ Icon: () => null }));
vi.mock("@/components/ui/Stack", () => ({ Stack: () => null }));
vi.mock("@/components/ui/Text", () => ({ Text: () => null }));
vi.mock("react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => children,
}));

import { AppSidebar } from "./app-sidebar";

const activeTrial = {
  startedAt: new Date("2026-08-05T00:00:00.000Z"),
  endsAt: new Date("2026-08-19T00:00:00.000Z"),
};

describe("AppSidebar enterprise trial status", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-05T00:00:00.000Z"));
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it("shows the active trial card in the project navigation footer", () => {
    mocks.useSession.mockReturnValue({ enterpriseTrial: activeTrial });

    render(<AppSidebar />);

    expect(
      screen.getByRole("group", { name: /Enterprise trial/ }),
    ).toBeTruthy();
  });

  it("does not render a trial card when trial data is absent", () => {
    mocks.useSession.mockReturnValue({ enterpriseTrial: null });

    render(<AppSidebar />);

    expect(
      screen.queryByRole("group", { name: /Enterprise trial/ }),
    ).toBeNull();
  });
});
