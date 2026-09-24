import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import { OrgSidebar } from "./org-sidebar";
import type { ReactNode } from "react";

const mocks = vi.hoisted(() => ({
  features: vi.fn(() => ({})),
  active: "agents",
  isPlatformAdmin: false,
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () =>
    new Proxy(
      {},
      {
        get: (_, key: string) => ({
          title: key === "identity" ? "IDP and SSO" : key,
          active: key === mocks.active,
          href: () => (key === "identity" ? "/example/identity" : `/${key}`),
        }),
      },
    ),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
  useIsPlatformAdmin: () => mocks.isPlatformAdmin,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ isLoading: false, hasScope: () => false }),
}));
vi.mock("@/hooks/useCanSetUpOrg", () => ({ useCanSetUpOrg: () => false }));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => false }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: mocks.features,
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
    items: {
      item: { title: string; href: () => string };
      label?: string;
    }[];
  }) => (
    <>
      {items.map(({ item, label }) => (
        <a key={item.title} href={item.href()} aria-label={item.title}>
          {label ?? item.title}
        </a>
      ))}
    </>
  ),
}));
vi.mock("./sidebar-footer-action", () => ({ SidebarFooterAction: () => null }));
vi.mock("./sidebar-user-menu", () => ({ SidebarUserMenu: () => null }));
vi.mock("./trial-status-card", () => ({ TrialStatusCard: () => null }));

afterEach(() => {
  cleanup();
  mocks.active = "agents";
  mocks.isPlatformAdmin = false;
});

it("lists one IDP and SSO entry under Team and no vendor entry", () => {
  render(<OrgSidebar />);
  expect(screen.queryByRole("link", { name: "okta" })).toBeNull();
  expect(
    screen.queryByRole("link", { name: "Enterprise Managed Auth" }),
  ).toBeNull();
  const identity = screen.getByRole("link", { name: "IDP and SSO" });
  expect(identity.getAttribute("href")).toBe("/example/identity");
});

it("keeps Network Access visible without a staff entitlement", () => {
  render(<OrgSidebar />);
  expect(screen.getByText("Network Access")).toBeTruthy();
});

it.each([
  ["team", "Team"],
  ["access", "Team"],
  ["identity", "Team"],
  ["auditLogs", "Secure"],
])("selects the correct group for %s", (active, group) => {
  mocks.active = active;
  render(<OrgSidebar />);
  expect(screen.getByTestId("selection").getAttribute("data-group")).toBe(
    group,
  );
  expect(screen.getByTestId("selection").getAttribute("data-item")).toBe(
    active === "identity" ? "IDP and SSO" : active,
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
      screen.queryByRole("link", { name: "remoteIdentityProviders" }),
    ).toBeNull();
    expect(screen.queryByRole("link", { name: "agents" })).toBeNull();
    if (isPlatformAdmin)
      expect(
        screen.getByRole("link", { name: "platformAdminOpenRouterKeys" }),
      ).toBeTruthy();
  },
);

it("does not request organization features for baseline project users", () => {
  render(<OrgSidebar />);
  expect(mocks.features).toHaveBeenLastCalledWith(
    expect.anything(),
    undefined,
    expect.objectContaining({ enabled: false }),
  );
});
