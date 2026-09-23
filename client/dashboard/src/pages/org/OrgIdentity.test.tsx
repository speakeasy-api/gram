import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";
import OrgIdentity from "./OrgIdentity";
import { TooltipProvider } from "@/components/ui/Tooltip";

const mocks = vi.hoisted(() => ({
  admin: true,
  enabled: true,
  ema: vi.fn(),
  features: vi.fn(() => ({ data: {} as Record<string, boolean> })),
  onboarding: { domainVerified: false, verifiedDomains: [] as string[] },
  ssoActive: false,
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
  useRBAC: () => ({
    hasScope: () => mocks.admin,
    hasAnyScope: () => mocks.admin,
    hasAllScopes: () => mocks.admin,
    isLoading: false,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "test-org", ssoEnabled: mocks.ssoActive }),
  useSessionData: () => ({ session: null }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    setupTask: {
      Link: ({ children }: { children: ReactNode }) => (
        <a href="/example/setup/idp">{children}</a>
      ),
    },
  }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => mocks.features(),
}));
vi.mock("@gram/client/react-query/onboardingStatus", () => ({
  useOnboardingStatus: () => ({ data: mocks.onboarding }),
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
  mocks.onboarding = { domainVerified: false, verifiedDomains: [] };
  mocks.ssoActive = false;
  mocks.features.mockImplementation(() => ({ data: {} }));
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

describe("domain verification gate", () => {
  function ssoSection() {
    const heading = screen.getByRole("heading", { name: "Single Sign-On" });
    return within(heading.closest("section") as HTMLElement);
  }

  it("renders the domain card and blocks SSO setup until verified", () => {
    mocks.features.mockImplementation(() => ({ data: { ssoEnabled: true } }));
    show();
    expect(
      screen.getByRole("heading", { name: "Domain verification" }),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Verify domain" })).toBeTruthy();
    expect(screen.queryByRole("list", { name: "Verified domains" })).toBeNull();

    const sso = ssoSection();
    expect(sso.getByText("Verify a domain first.")).toBeTruthy();
    expect(
      sso.getByRole("button", { name: "Configure" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(
      sso.getByRole("button", { name: "Configure" }).closest("a"),
    ).toBeNull();
  });

  it("enables SSO setup once the domain is verified", () => {
    mocks.features.mockImplementation(() => ({ data: { ssoEnabled: true } }));
    mocks.onboarding = {
      domainVerified: true,
      verifiedDomains: ["example.com", "example.org"],
    };
    show();
    expect(screen.getByText("Verified")).toBeTruthy();
    const domains = within(
      screen.getByRole("list", { name: "Verified domains" }),
    );
    expect(domains.getByText("example.com")).toBeTruthy();
    expect(domains.getByText("example.org")).toBeTruthy();
    expect(
      screen.getByText("SSO applies to users on these domains."),
    ).toBeTruthy();

    const sso = ssoSection();
    expect(sso.queryByText("Verify a domain first.")).toBeNull();
    expect(
      sso.getByRole("button", { name: "Configure" }).hasAttribute("disabled"),
    ).toBe(false);
  });

  it("keeps SSO manageable when SSO is already active", () => {
    mocks.features.mockImplementation(() => ({ data: { ssoEnabled: true } }));
    mocks.ssoActive = true;
    show();
    expect(
      ssoSection()
        .getByRole("button", { name: "Configure" })
        .hasAttribute("disabled"),
    ).toBe(false);
  });
});
