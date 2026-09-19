import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import type { ReactNode } from "react";
import OrgIdentity from "./OrgIdentity";
import { TooltipProvider } from "@/components/ui/Tooltip";

const mocks = vi.hoisted(() => ({
  admin: true,
  enabled: true,
  ema: vi.fn(),
  features: vi.fn(() => ({ data: {} })),
}));
vi.mock("./identity-provider/EnterpriseManagedAuth", () => ({
  EnterpriseManagedAuth: () => {
    mocks.ema();
    return <div>EMA workspace</div>;
  },
}));
vi.mock("nuqs", async (importOriginal) => {
  const actual = await importOriginal<typeof import("nuqs")>();
  return {
    ...actual,
    useQueryState: (
      key: string,
      parser?: {
        parse: (value: string) => string | null;
        defaultValue: string;
      },
    ) => {
      const value = new URLSearchParams(useLocation().search).get(key);
      return [
        parser
          ? ((value ? parser.parse(value) : null) ?? parser.defaultValue)
          : value,
      ];
    },
  };
});
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: mocks.enabled ? "enabled" : "disabled" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.admin }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "test-org" }),
  useSessionData: () => ({ session: null }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));
vi.mock("@/routes", () => ({ useOrgRoutes: () => ({}) }));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => mocks.features(),
}));
vi.mock("@gram/client/react-query/generateWorkOSAdminPortalLink.js", () => ({
  useGenerateWorkOSAdminPortalLinkMutation: () => ({}),
}));
vi.mock("@/components/page-templates", () => ({
  TabbedPage: ({
    title,
    tabs,
    activeTab,
    children,
  }: {
    title: string;
    tabs: { value: string; label: string; href: string; stage?: string }[];
    activeTab: string;
    children: ReactNode;
  }) => (
    <main>
      <h1>{title}</h1>
      <nav>
        {tabs.map((tab) => (
          <a
            key={tab.value}
            href={tab.href}
            data-stage={tab.stage}
            aria-current={activeTab === tab.value ? "page" : undefined}
          >
            {tab.label}
          </a>
        ))}
      </nav>
      {children}
    </main>
  ),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.admin = true;
  mocks.enabled = true;
});

function Location() {
  const location = useLocation();
  return (
    <output data-testid="location">
      {location.search}
      {location.hash}
    </output>
  );
}
function show(search = "") {
  render(
    <MemoryRouter initialEntries={[`/example/identity${search}`]}>
      <TooltipProvider>
        <OrgIdentity />
        <Location />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

describe("identity top-level tabs", () => {
  it("shows only employee SSO and preview EMA without mounting EMA on SSO", () => {
    show();
    expect(screen.getByRole("heading", { name: "IDP and SSO" })).toBeTruthy();
    const nav = screen.getByRole("navigation");
    expect(nav.querySelectorAll("a")).toHaveLength(2);
    expect(
      screen.getByRole("link", { name: "Single sign-on" }).getAttribute("href"),
    ).toBe("?tab=sso");
    const ema = screen.getByRole("link", { name: "Enterprise Managed Auth" });
    expect(ema.getAttribute("href")).toBe("?tab=enterprise-managed-auth");
    expect(ema.getAttribute("data-stage")).toBe("preview");
    expect(mocks.ema).not.toHaveBeenCalled();
    expect(mocks.features).toHaveBeenCalled();
  });

  it("mounts EMA without fetching employee SSO features", () => {
    show("?tab=enterprise-managed-auth");
    expect(screen.getByText("EMA workspace")).toBeTruthy();
    expect(mocks.features).not.toHaveBeenCalled();
  });

  it.each(["admin", "enabled"] as const)(
    "requires %s before mounting EMA",
    (gate) => {
      mocks[gate] = false;
      show("?tab=enterprise-managed-auth&provider=okta&view=setup");
      expect(
        screen.queryByRole("link", { name: "Enterprise Managed Auth" }),
      ).toBeNull();
      expect(mocks.ema).not.toHaveBeenCalled();
      expect(mocks.features).toHaveBeenCalled();
    },
  );

  it("canonicalizes a legacy link for non-admins without mounting EMA", () => {
    mocks.admin = false;
    show("?tab=applications#connection");
    expect(screen.getByTestId("location").textContent).toBe(
      "?tab=enterprise-managed-auth&provider=okta&view=applications#connection",
    );
    expect(mocks.ema).not.toHaveBeenCalled();
    expect(mocks.features).toHaveBeenCalled();
  });

  it("falls back to SSO for arbitrary unsupported tabs", () => {
    show("?tab=bogus");
    expect(mocks.ema).not.toHaveBeenCalled();
    expect(
      screen
        .getByRole("link", { name: "Single sign-on" })
        .getAttribute("aria-current"),
    ).toBe("page");
  });

  it.each([
    ["provider", "setup"],
    ["applications", "applications"],
    ["cross-app-access", "cross-app-access"],
    ["okta&okta=connection", "setup"],
    ["okta&okta=applications", "applications"],
    ["okta&okta=cross-app-access", "cross-app-access"],
    ["okta&okta=unknown", "setup"],
  ])(
    "canonicalizes legacy %s with query parameters and router hash",
    (tab, view) => {
      show(`?tab=${tab}&filter=active#connection`);
      expect(screen.getByTestId("location").textContent).toBe(
        `?tab=enterprise-managed-auth&filter=active&provider=okta&view=${view}#connection`,
      );
      expect(screen.getByText("EMA workspace")).toBeTruthy();
    },
  );
});
