import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { OrgSidebar } from "./org-sidebar";
import type { ReactNode } from "react";
import type { ProductTier } from "@/hooks/useProductTier";

const mocks = vi.hoisted(() => ({
  features: vi.fn(() => ({})),
  isPlatformAdmin: false,
  productTier: "enterprise" as ProductTier,
  adminOrganizationId: null as string | null,
  userSessionIssuersFlag: "disabled" as "enabled" | "disabled",
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "example", projectSlug: "project" }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
  useIsPlatformAdmin: () => mocks.isPlatformAdmin,
}));
vi.mock("@/hooks/useRBAC", async (importOriginal) => {
  const { hasScopeInGrants } =
    await importOriginal<typeof import("@/hooks/useRBAC")>();
  return {
    useRBAC: () => ({
      isLoading: false,
      hasScope: (
        scope: Parameters<typeof hasScopeInGrants>[1],
        resourceId?: string,
      ) =>
        hasScopeInGrants(
          mocks.adminOrganizationId
            ? [
                {
                  scope: "org:admin",
                  selectors: [
                    {
                      resourceKind: "org",
                      resourceId: mocks.adminOrganizationId,
                    },
                  ],
                },
              ]
            : [],
          scope,
          resourceId,
        ),
    }),
  };
});
vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => mocks.productTier,
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => false }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: mocks.userSessionIssuersFlag }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: mocks.features,
}));
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  Link: ({ children, to }: { children: ReactNode; to: string }) => (
    <a href={to}>{children}</a>
  ),
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
    label: groupLabel,
    items,
  }: {
    label: string;
    items: {
      item: { title: string; href: () => string };
      label?: string;
      scope?: string | string[];
    }[];
  }) => (
    <nav aria-label={groupLabel}>
      {items.map(({ item, label, scope }) => (
        <a
          key={item.title}
          href={item.href()}
          aria-label={item.title}
          data-scope={[scope ?? []].flat().join(" ")}
        >
          {label ?? item.title}
        </a>
      ))}
    </nav>
  ),
}));
vi.mock("./sidebar-user-menu", () => ({ SidebarUserMenu: () => null }));
vi.mock("./trial-status-card", () => ({ TrialStatusCard: () => null }));

function renderSidebar(path = "/example") {
  render(
    <MemoryRouter initialEntries={[path]}>
      <OrgSidebar />
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  mocks.isPlatformAdmin = false;
  mocks.productTier = "enterprise";
  mocks.adminOrganizationId = null;
  mocks.userSessionIssuersFlag = "disabled";
});

it("lists one IDP and SSO entry under Team and no vendor entry", () => {
  renderSidebar();
  expect(screen.queryByRole("link", { name: "okta" })).toBeNull();
  expect(
    screen.queryByRole("link", { name: "Enterprise Managed Auth" }),
  ).toBeNull();
  const identity = screen.getByRole("link", { name: "IDP and SSO" });
  expect(identity.getAttribute("href")).toBe("/example/identity");
});

it("keeps Network Access visible without a staff entitlement", () => {
  renderSidebar();
  expect(screen.getByText("Network Access")).toBeTruthy();
});

it.each([
  ["/example/team", "Team", "Team"],
  ["/example/access", "Team", "Roles & Permissions"],
  ["/example/identity", "Team", "IDP and SSO"],
  ["/example/audit-logs", "Secure", "Audit Logs"],
])("selects the correct group for %s", (path, group, item) => {
  renderSidebar(path);
  expect(screen.getByTestId("selection").getAttribute("data-group")).toBe(
    group,
  );
  expect(screen.getByTestId("selection").getAttribute("data-item")).toBe(item);
});

it("lists Speakeasy authorization servers under Settings only once rolled out", () => {
  renderSidebar();
  expect(
    screen.queryByRole("link", { name: "Speakeasy Authorization Servers" }),
  ).toBeNull();
  cleanup();

  mocks.userSessionIssuersFlag = "enabled";
  renderSidebar();
  const link = within(
    screen.getByRole("navigation", { name: "Settings" }),
  ).getByRole("link", { name: "Speakeasy Authorization Servers" });
  expect(link.textContent).toBe("Authorization Servers");
  expect(link.getAttribute("data-scope")).toBe("org:read org:admin");
});

it.each([false, true])(
  "omits platform issuer management for platform admin=%s",
  (isPlatformAdmin) => {
    mocks.isPlatformAdmin = isPlatformAdmin;
    renderSidebar("/example/remote-identity-providers/issuer-1");
    expect(
      screen.queryByRole("link", { name: /Remote Identity Providers/ }),
    ).toBeNull();
    expect(
      screen.queryByRole("link", { name: "Authorization Servers" }),
    ).toBeNull();
    expect(screen.queryByRole("link", { name: "Agent Identity" })).toBeNull();
    if (isPlatformAdmin)
      expect(
        screen.getByRole("link", { name: "OpenRouter Keys" }),
      ).toBeTruthy();
  },
);

it("does not request organization features for baseline project users", () => {
  renderSidebar();
  expect(mocks.features).toHaveBeenLastCalledWith(
    expect.anything(),
    undefined,
    expect.objectContaining({ enabled: false }),
  );
});

it.each([
  ["enterprise", "org_example", true],
  ["payg", "org_example", true],
  ["base", "org_example", false],
  ["base_PAID", "org_example", false],
  ["__deprecated__pro", "org_example", false],
  ["enterprise", null, false],
  ["payg", null, false],
  ["enterprise", "org_other", false],
  ["payg", "org_other", false],
] as const)(
  "shows setup footer for tier=%s adminOrganization=%s: %s",
  (tier, adminOrganizationId, visible) => {
    mocks.productTier = tier;
    mocks.adminOrganizationId = adminOrganizationId;
    renderSidebar();
    const link = screen.queryByRole("link", {
      name: "Finish organization setup",
    });
    if (visible) {
      expect(link?.getAttribute("href")).toBe("/example/setup");
    } else {
      expect(link).toBeNull();
    }
  },
);
