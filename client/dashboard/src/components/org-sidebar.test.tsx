import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  useSession: vi.fn(),
  useProductFeatures: vi.fn(() => ({ data: undefined })),
  routes: new Proxy(
    {},
    {
      get: () => ({
        active: false,
        href: () => "/",
        title: "Route",
        Icon: () => null,
      }),
    },
  ),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => false }),
}));
vi.mock("@/hooks/useRBAC", () => ({ useRBAC: () => ({ isLoading: false }) }));
vi.mock("@/routes", () => ({ useOrgRoutes: () => mocks.routes }));
vi.mock("@/contexts/Auth", () => ({
  useSession: mocks.useSession,
  useIsPlatformAdmin: () => false,
}));
vi.mock("./enterprise-trial-status-card", () => ({
  EnterpriseTrialStatusCard: () =>
    mocks.useSession().enterpriseTrial ? (
      <div role="group" aria-label="Enterprise trial" />
    ) : null,
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: mocks.useProductFeatures,
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
vi.mock("@/components/ui/Icon", () => ({ Icon: () => null }));
vi.mock("./gram-logo", () => ({ GramLogo: () => null }));
vi.mock("./command-palette/CommandPaletteTrigger", () => ({
  CommandPaletteTrigger: () => null,
}));
vi.mock("./sidebar-nav-skeleton", () => ({ SidebarNavSkeleton: () => null }));
vi.mock("./onboarding-resume-button", () => ({
  OnboardingResumeButton: () => null,
}));
vi.mock("./sidebar-user-menu", () => ({ SidebarUserMenu: () => null }));
vi.mock("./workspace-switcher", () => ({ WorkspaceSwitcher: () => null }));
vi.mock("react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => children,
}));

import { OrgSidebar } from "./org-sidebar";

const activeTrial = {
  startedAt: new Date("2026-08-05T00:00:00.000Z"),
  endsAt: new Date("2026-08-19T00:00:00.000Z"),
};

describe("OrgSidebar enterprise trial status", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("shows the active trial card in the organization navigation footer", () => {
    mocks.useSession.mockReturnValue({ enterpriseTrial: activeTrial });

    render(<OrgSidebar />);

    expect(
      screen.getByRole("group", { name: /Enterprise trial/ }),
    ).toBeTruthy();
  });

  it("does not render a trial card when trial data is absent", () => {
    mocks.useSession.mockReturnValue({ enterpriseTrial: null });

    render(<OrgSidebar />);

    expect(
      screen.queryByRole("group", { name: /Enterprise trial/ }),
    ).toBeNull();
  });
});
