import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { SettingsNav } from "./settings-nav";
import type { ReactNode } from "react";
import type { ProductTier } from "@/hooks/useProductTier";

const mocks = vi.hoisted(() => ({
  features: vi.fn(() => ({})),
  active: "agents",
  isPlatformAdmin: false,
  productTier: "enterprise" as ProductTier,
  adminOrganizationId: null as string | null,
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () =>
    new Proxy(
      {},
      {
        get: (_, key: string) => ({
          title: key === "identity" ? "IDP and SSO" : key,
          url: key,
          Icon: () => null,
          active: key === mocks.active,
          href: () =>
            key === "identity" || key === "setup"
              ? `/example/${key}`
              : `/${key}`,
        }),
      },
    ),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org_example",
    name: "Example",
    slug: "example",
  }),
  useIsPlatformAdmin: () => mocks.isPlatformAdmin,
}));
vi.mock("@/hooks/useRBAC", async (importOriginal) => {
  const { hasScopeInGrants } =
    await importOriginal<typeof import("@/hooks/useRBAC")>();
  type Scope = Parameters<typeof hasScopeInGrants>[1];
  const hasScope = (scope: Scope, resourceId?: string) =>
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
    );
  return {
    useRBAC: () => ({
      isLoading: false,
      hasScope,
      // Nav visibility is a UI filter; the pages enforce access themselves.
      hasAnyScope: () => true,
    }),
  };
});
vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => mocks.productTier,
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => false }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: mocks.features,
}));
vi.mock("react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({
    children,
    to,
    ...rest
  }: {
    children: ReactNode;
    to: string;
    "aria-current"?: "page";
  }) => (
    <a href={to} aria-current={rest["aria-current"]}>
      {children}
    </a>
  ),
}));
afterEach(() => {
  cleanup();
  mocks.active = "agents";
  mocks.isPlatformAdmin = false;
  mocks.productTier = "enterprise";
  mocks.adminOrganizationId = null;
});

it("lists one SSO entry under People and no vendor entry", () => {
  render(<SettingsNav />);
  expect(screen.queryByRole("link", { name: "okta" })).toBeNull();
  expect(
    screen.queryByRole("link", { name: "Enterprise Managed Auth" }),
  ).toBeNull();
  const identity = screen.getByRole("link", { name: "SSO & Directory" });
  expect(identity.getAttribute("href")).toBe("/example/identity");
});

it("keeps Network Access visible without a staff entitlement", () => {
  render(<SettingsNav />);
  expect(screen.getByText("Network Access")).toBeTruthy();
});

it.each([
  ["team", "Members"],
  ["access", "access"],
  ["createRole", "access"],
  ["identity", "SSO & Directory"],
  ["auditLogs", "auditLogs"],
])("marks the current page for %s", (active, label) => {
  mocks.active = active;
  render(<SettingsNav />);
  const current = screen
    .getAllByRole("link")
    .filter((link) => link.getAttribute("aria-current") === "page");
  expect(current.map((link) => link.textContent)).toEqual([label]);
});

it.each([false, true])(
  "omits platform issuer management for platform admin=%s",
  (isPlatformAdmin) => {
    mocks.isPlatformAdmin = isPlatformAdmin;
    mocks.active = "remoteIdentityProviders";
    render(<SettingsNav />);
    expect(
      screen.queryByRole("link", { name: "platformRemoteIdentityProviders" }),
    ).toBeNull();
    expect(
      screen.queryByRole("link", { name: "remoteIdentityProviders" }),
    ).toBeNull();
    expect(screen.queryByRole("link", { name: "agents" })).toBeNull();
    if (isPlatformAdmin)
      expect(
        screen.getByRole("link", { name: "OpenRouter Keys" }),
      ).toBeTruthy();
  },
);

it("does not request organization features for baseline project users", () => {
  render(<SettingsNav />);
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
    render(<SettingsNav />);
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

it("finds the page that holds a setting from its search terms", () => {
  render(<SettingsNav />);
  fireEvent.change(screen.getByPlaceholderText("Search"), {
    target: { value: "saml" },
  });
  const [first] = screen.getAllByRole("option");
  expect(first?.textContent).toContain("SSO & Directory");
  expect(first?.textContent).toContain("SAML");
});

it("says so when nothing matches", () => {
  render(<SettingsNav />);
  fireEvent.change(screen.getByPlaceholderText("Search"), {
    target: { value: "zzzzqqq" },
  });
  expect(screen.getByText(/No settings match/)).toBeTruthy();
});
