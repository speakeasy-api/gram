import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { OrgSidebar } from "./org-sidebar";

const mocks = vi.hoisted(() => ({ active: "agents", isPlatformAdmin: false }));
vi.mock("@/routes", () => ({
  useOrgRoutes: () =>
    new Proxy(
      {},
      {
        get: (_, key: string) => ({
          title: key,
          active: key === mocks.active,
          href: () => `/${key}`,
        }),
      },
    ),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
  useIsPlatformAdmin: () => mocks.isPlatformAdmin,
}));
vi.mock("@/hooks/useRBAC", () => ({ useRBAC: () => ({ isLoading: false }) }));
vi.mock("@/hooks/useCanSetUpOrg", () => ({ useCanSetUpOrg: () => false }));
vi.mock("@/hooks/useKillswitchAccess", () => ({
  useKillswitchAccess: () => ({}),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => false }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({}),
}));
vi.mock("react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock("@/components/nav-menu", () => ({
  NavButton: () => null,
  NavGroupProvider: ({
    activeGroup,
    activeItem,
    children,
  }: {
    activeGroup: string;
    activeItem: string;
    children: ReactNode;
  }) => (
    <div
      data-testid="selection"
      data-group={activeGroup}
      data-item={activeItem}
    >
      {children}
    </div>
  ),
}));
vi.mock("@/components/ui/Sidebar", () => {
  const Part = ({ children }: { children: ReactNode }) => <div>{children}</div>;
  return {
    Sidebar: Part,
    SidebarContent: Part,
    SidebarFooter: Part,
    SidebarHeader: Part,
    SidebarMenu: Part,
    SidebarMenuItem: Part,
    SidebarTrigger: () => null,
  };
});
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/scope-gated-nav-group", () => ({
  ScopeGatedNavGroup: ({
    items,
  }: {
    items: { item: { title: string; href: () => string } }[];
  }) => (
    <>
      {items.map(({ item }) => (
        <a key={item.title} href={item.href()}>
          {item.title}
        </a>
      ))}
    </>
  ),
}));
vi.mock("./sidebar-footer-action", () => ({ SidebarFooterAction: () => null }));
vi.mock("./sidebar-user-menu", () => ({ SidebarUserMenu: () => null }));
vi.mock("./trial-status-card", () => ({ TrialStatusCard: () => null }));

afterEach(cleanup);
it.each([
  ["agents", "Identity"],
  ["auditLogs", "Secure"],
])("selects the correct group for %s", (active, group) => {
  mocks.active = active;
  render(<OrgSidebar />);
  expect(screen.getByTestId("selection").getAttribute("data-group")).toBe(
    group,
  );
  expect(screen.getByTestId("selection").getAttribute("data-item")).toBe(
    active,
  );
});

it.each([false, true])(
  "omits platform issuer management for platform admin=%s",
  (isPlatformAdmin) => {
    mocks.isPlatformAdmin = isPlatformAdmin;
    mocks.active = "remoteIdentityProviders";
    render(<OrgSidebar />);
    expect(
      screen.queryByRole("link", { name: "platformRemoteIdentityProviders" }),
    ).toBeNull();
    expect(
      screen.getByRole("link", { name: "remoteIdentityProviders" }),
    ).toBeTruthy();
    if (isPlatformAdmin)
      expect(
        screen.getByRole("link", { name: "platformAdminOpenRouterKeys" }),
      ).toBeTruthy();
  },
);
