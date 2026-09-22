import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";
import OrgIdentity from "./OrgIdentity";
import { TooltipProvider } from "@/components/ui/Tooltip";

const mocks = vi.hoisted(() => ({
  admin: true,
  enabled: true,
  ema: vi.fn(),
  features: vi.fn(() => ({ data: {} })),
}));
vi.mock("nuqs", async (importOriginal) => ({
  ...(await importOriginal<typeof import("nuqs")>()),
  useQueryState: (await import("./identity-provider/nuqsRouterMock"))
    .useRouterQueryState,
}));
vi.mock("./identity-provider/EnterpriseManagedAuth", () => ({
  EnterpriseManagedAuth: () => {
    mocks.ema();
    return <div>EMA workspace</div>;
  },
}));
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

function show(search = "") {
  render(
    <MemoryRouter initialEntries={[`/example/identity${search}`]}>
      <TooltipProvider>
        <OrgIdentity />
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

  it("falls back to SSO for arbitrary unsupported tabs", () => {
    show("?tab=bogus");
    expect(mocks.ema).not.toHaveBeenCalled();
    expect(
      screen
        .getByRole("link", { name: "Single sign-on" })
        .getAttribute("aria-current"),
    ).toBe("page");
  });
});
