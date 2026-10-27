import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";
import OrgIdentity from "./OrgIdentity";
import { TooltipProvider } from "@/components/ui/Tooltip";

const mocks = vi.hoisted(() => ({
  admin: true,
  enabled: true,
  issuerAvailable: false,
  issuerQuery: undefined as
    | { data?: { state: string; issuer?: string }; isError?: boolean }
    | undefined,
  ema: vi.fn(),
  slack: vi.fn(),
  features: vi.fn(() => ({ data: {} as Record<string, boolean> })),
  onboarding: {
    domainVerified: false,
    ssoConfigured: false,
    verifiedDomains: [] as string[],
  },
  onboardingQuery: undefined as
    | { data: undefined; isLoading: boolean; isError: boolean }
    | undefined,
  portal: { isPending: false, mutate: vi.fn() },
  ssoActive: false,
  scimActive: false,
}));
vi.mock("nuqs", async (importOriginal) => ({
  ...(await importOriginal<typeof import("nuqs")>()),
  useQueryState: (await import("./identity-provider/nuqsRouterMock"))
    .useRouterQueryState,
}));
vi.mock("./identity-provider/DirectoryRoleMappings", () => ({
  DirectoryRoleMappings: ({ footerAction }: { footerAction?: ReactNode }) => (
    <div>
      Role mappings panel
      {footerAction}
    </div>
  ),
}));
vi.mock("@/pages/identities/DirectoryPeopleSummary", () => ({
  DirectoryPeopleSummary: () => null,
}));
vi.mock("./identity-provider/useEnterpriseIssuer", () => ({
  useEnterpriseIssuer: () =>
    mocks.issuerQuery ?? {
      data: mocks.issuerAvailable ? { state: "setup_required" } : undefined,
    },
}));
vi.mock("./identity-provider/IdentityProviders", () => ({
  IdentityProviders: (props: unknown) => {
    mocks.ema(props);
    return <div>EMA workspace</div>;
  },
}));
vi.mock("./slack-workspaces/SlackWorkspaces", () => ({
  SlackWorkspaces: () => {
    mocks.slack();
    return <div>Slack workspace connections</div>;
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
  useOrganization: () => ({
    id: "test-org",
    ssoEnabled: mocks.ssoActive,
    scimEnabled: mocks.scimActive,
  }),
  useSessionData: () => ({ session: null }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    setup: {
      Link: ({ children }: { children: ReactNode }) => (
        <a href="/example/setup?task=identity-provider">{children}</a>
      ),
    },
  }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => mocks.features(),
}));
vi.mock("@gram/client/react-query/onboardingStatus", () => ({
  useOnboardingStatus: () =>
    mocks.onboardingQuery ?? {
      data: mocks.onboarding,
      isLoading: false,
      isError: false,
    },
}));
vi.mock("@gram/client/react-query/generateWorkOSAdminPortalLink.js", () => ({
  useGenerateWorkOSAdminPortalLinkMutation: () => mocks.portal,
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
  mocks.issuerAvailable = false;
  mocks.issuerQuery = undefined;
  mocks.onboarding = {
    domainVerified: false,
    ssoConfigured: false,
    verifiedDomains: [],
  };
  mocks.onboardingQuery = undefined;
  mocks.ssoActive = false;
  mocks.scimActive = false;
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

function section(name: string) {
  const heading = screen.getByRole("heading", { name });
  return within(heading.closest("section") as HTMLElement);
}

describe("identity top-level tabs", () => {
  it("shows SSO and preview connection tabs without mounting them on SSO", () => {
    show();
    expect(screen.getByRole("heading", { name: "IDP and SSO" })).toBeTruthy();
    const nav = screen.getByRole("navigation");
    expect(nav.querySelectorAll("a")).toHaveLength(3);
    expect(
      screen.getByRole("link", { name: "Single sign-on" }).getAttribute("href"),
    ).toBe("?tab=sso");
    const ema = screen.getByRole("link", { name: "Identity providers" });
    expect(ema.getAttribute("href")).toBe("?tab=identity-providers");
    expect(ema.getAttribute("data-stage")).toBe("preview");
    expect(mocks.ema).not.toHaveBeenCalled();
    expect(mocks.features).toHaveBeenCalled();
  });

  it("mounts EMA without fetching employee SSO features", () => {
    show("?tab=identity-providers");
    expect(screen.getByText("EMA workspace")).toBeTruthy();
    expect(mocks.features).not.toHaveBeenCalled();
  });

  it.each(["admin", "enabled"] as const)(
    "requires %s before mounting EMA",
    (gate) => {
      mocks[gate] = false;
      show("?tab=identity-providers&provider=okta&view=setup");
      expect(
        screen.queryByRole("link", { name: "Identity providers" }),
      ).toBeNull();
      expect(mocks.ema).not.toHaveBeenCalled();
      expect(mocks.features).toHaveBeenCalled();
    },
  );

  it("lists the Speakeasy provider when the issuer is offered without the Okta rollout", () => {
    mocks.enabled = false;
    mocks.issuerAvailable = true;
    show("?tab=identity-providers");
    expect(
      screen.getByRole("link", { name: "Identity providers" }),
    ).toBeTruthy();
    expect(mocks.ema).toHaveBeenCalledWith({
      showExternal: false,
      showSpeakeasy: true,
    });
  });

  it.each([
    [
      "an existing issuer reports unavailable",
      { data: { state: "unavailable", issuer: "https://issuer.example.com" } },
    ],
    ["its status failed to load", { isError: true }],
  ])("keeps the Speakeasy provider reachable when %s", (_, query) => {
    mocks.enabled = false;
    mocks.issuerQuery = query;
    show("?tab=identity-providers");
    expect(mocks.ema).toHaveBeenCalledWith({
      showExternal: false,
      showSpeakeasy: true,
    });
  });

  it("hides the Speakeasy provider when the issuer is not offered", () => {
    mocks.enabled = false;
    mocks.issuerQuery = { data: { state: "unavailable" } };
    show("?tab=identity-providers");
    expect(
      screen.queryByRole("link", { name: "Identity providers" }),
    ).toBeNull();
  });

  it("keeps the issuer off the Single sign-on tab", () => {
    mocks.issuerAvailable = true;
    show();
    expect(screen.queryByText(/Enterprise Managed Authorization/)).toBeNull();
    expect(mocks.ema).not.toHaveBeenCalled();
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
});

describe("domain verification gate", () => {
  const ssoSection = () => section("Single Sign-On");

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
      ssoConfigured: false,
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

describe("directory sync domain gate", () => {
  const directorySyncSection = () => section("Directory Sync");

  function configureButton() {
    return directorySyncSection().getByRole<HTMLButtonElement>("button", {
      name: "Configure",
    });
  }

  it("blocks Directory Sync setup until a domain is verified", () => {
    mocks.features.mockImplementation(() => ({ data: { scimEnabled: true } }));
    show();
    const dsync = directorySyncSection();
    expect(dsync.getByText("Verify a domain first.")).toBeTruthy();
    expect(
      dsync.getByText(
        "Verify a domain above before setting up Directory Sync.",
      ),
    ).toBeTruthy();
    expect(configureButton().disabled).toBe(true);
  });

  it("enables Directory Sync setup once the domain is verified", () => {
    mocks.features.mockImplementation(() => ({ data: { scimEnabled: true } }));
    mocks.onboarding = {
      domainVerified: true,
      ssoConfigured: false,
      verifiedDomains: ["example.com"],
    };
    show();
    const dsync = directorySyncSection();
    expect(dsync.queryByText("Verify a domain first.")).toBeNull();
    expect(
      dsync.queryByText(
        "Verify a domain above before setting up Directory Sync.",
      ),
    ).toBeNull();
    expect(configureButton().disabled).toBe(false);
  });

  // Matches the server: active SSO proves a domain was verified, even when
  // no verified domain was tracked for the org.
  it("treats an active SSO connection as a verified domain", () => {
    mocks.features.mockImplementation(() => ({ data: { scimEnabled: true } }));
    mocks.onboarding = {
      domainVerified: false,
      ssoConfigured: true,
      verifiedDomains: [],
    };
    show();
    const dsync = directorySyncSection();
    expect(dsync.queryByText("Verify a domain first.")).toBeNull();
    expect(
      dsync.queryByText(
        "Verify a domain above before setting up Directory Sync.",
      ),
    ).toBeNull();
    expect(configureButton().disabled).toBe(false);
  });

  it("keeps Directory Sync manageable when it is already active", () => {
    mocks.features.mockImplementation(() => ({ data: { scimEnabled: true } }));
    mocks.scimActive = true;
    show();
    const dsync = directorySyncSection();
    expect(dsync.queryByText("Verify a domain first.")).toBeNull();
    expect(
      dsync.getByRole<HTMLButtonElement>("button", {
        name: "Manage connection",
      }).disabled,
    ).toBe(false);
    expect(dsync.getByText("Role mappings panel")).toBeTruthy();
  });

  it("keeps role mappings from non-admins and shows them the SCIM card", () => {
    mocks.features.mockImplementation(() => ({ data: { scimEnabled: true } }));
    mocks.scimActive = true;
    mocks.admin = false;
    show();
    const dsync = directorySyncSection();
    expect(dsync.queryByText("Role mappings panel")).toBeNull();
    expect(
      dsync.queryByRole("button", { name: "Manage connection" }),
    ).toBeNull();
    expect(
      dsync.getByText("Your directory provider is connected."),
    ).toBeTruthy();
  });
});

describe("domain gate before a status response", () => {
  it.each([
    ["loading", { data: undefined, isLoading: true, isError: false }],
    ["failed", { data: undefined, isLoading: false, isError: true }],
  ] as const)("does not block setup while the status is %s", (_, query) => {
    mocks.features.mockImplementation(() => ({
      data: { ssoEnabled: true, scimEnabled: true },
    }));
    mocks.onboardingQuery = query;
    show();
    expect(screen.queryByText("Verify a domain first.")).toBeNull();
    expect(screen.queryByText(/Verify a domain above/)).toBeNull();
    for (const name of ["Single Sign-On", "Directory Sync"]) {
      expect(
        section(name).getByRole<HTMLButtonElement>("button", {
          name: "Configure",
        }).disabled,
      ).toBe(false);
    }
  });
});

describe("domain verification portal", () => {
  it("opens the WorkOS portal with the domain verification intent", () => {
    show();
    fireEvent.click(screen.getByRole("button", { name: "Verify domain" }));
    expect(mocks.portal.mutate).toHaveBeenCalledOnce();
    expect(
      mocks.portal.mutate.mock.calls[0]?.[0].request
        .generateWorkOSAdminPortalLinkRequestBody.intent,
    ).toBe("domain_verification");
  });

  it("opens the WorkOS portal to set up SSO", () => {
    mocks.features.mockImplementation(() => ({ data: { ssoEnabled: true } }));
    mocks.onboarding = {
      domainVerified: true,
      ssoConfigured: false,
      verifiedDomains: ["example.com"],
    };
    show();
    fireEvent.click(
      section("Single Sign-On").getByRole("button", { name: "Configure" }),
    );
    expect(mocks.portal.mutate).toHaveBeenCalledOnce();
    expect(
      mocks.portal.mutate.mock.calls[0]?.[0].request
        .generateWorkOSAdminPortalLinkRequestBody.intent,
    ).toBe("sso");
  });

  it("opens the WorkOS portal to set up Directory Sync", () => {
    mocks.features.mockImplementation(() => ({ data: { scimEnabled: true } }));
    mocks.onboarding = {
      domainVerified: true,
      ssoConfigured: false,
      verifiedDomains: ["example.com"],
    };
    show();
    fireEvent.click(
      section("Directory Sync").getByRole("button", { name: "Configure" }),
    );
    expect(mocks.portal.mutate).toHaveBeenCalledOnce();
    expect(
      mocks.portal.mutate.mock.calls[0]?.[0].request
        .generateWorkOSAdminPortalLinkRequestBody.intent,
    ).toBe("dsync");
  });
});

describe("Slack workspaces tab", () => {
  it("opens for organization admins independently of rollout flags", () => {
    mocks.enabled = false;
    show("?tab=slack-workspaces");
    expect(screen.getByText("Slack workspace connections")).toBeTruthy();
    expect(mocks.slack).toHaveBeenCalled();
  });

  it("is hidden from non-admins", () => {
    mocks.admin = false;
    show("?tab=slack-workspaces");
    expect(screen.queryByRole("link", { name: "Slack workspaces" })).toBeNull();
    expect(mocks.slack).not.toHaveBeenCalled();
    expect(
      screen
        .getByRole("link", { name: "Single sign-on" })
        .getAttribute("aria-current"),
    ).toBe("page");
  });
});
