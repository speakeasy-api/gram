import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IssuerScopeOverrideAlert, LegacyCallbackAlert } from "./clientAlerts";

const rbac = vi.hoisted(() => ({
  canReadOrg: true,
  requested: [] as string[][],
  isPlatformAdmin: true,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    remoteIdentityProviders: {
      issuerDetail: {
        settings: { href: (id: string) => `/providers/${id}/settings` },
      },
    },
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAnyScope: (scopes: string[]) => {
      rbac.requested.push(scopes);
      return rbac.canReadOrg;
    },
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => rbac.isPlatformAdmin,
}));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://app.example.com",
}));
vi.mock(
  "../mcp/x/tabs/settings/sections/authentication/issuerFormUtils",
  () => ({
    remoteLoginCallbackURL: () =>
      "https://app.example.com/mcp/remote_login_callback",
  }),
);
vi.mock("react-router", () => ({
  Link: ({ to, children }: { to: string; children: ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

afterEach(() => {
  cleanup();
  rbac.canReadOrg = true;
  rbac.requested = [];
  rbac.isPlatformAdmin = true;
});

const orgIssuer = (scopeOverride: string[] | null) => ({
  id: "issuer-1",
  organizationId: "org-1",
  scopeOverride,
});

describe("IssuerScopeOverrideAlert", () => {
  it("renders nothing when the issuer has no override", () => {
    const { container } = render(
      <>
        <IssuerScopeOverrideAlert issuer={undefined} />
        <IssuerScopeOverrideAlert issuer={orgIssuer(null)} />
        <IssuerScopeOverrideAlert issuer={orgIssuer([])} />
      </>,
    );

    expect(container.textContent).toBe("");
    expect(rbac.requested).toEqual([]);
  });

  it("names the pinned scopes and links to the issuer's settings", () => {
    render(
      <IssuerScopeOverrideAlert issuer={orgIssuer(["openid", "mcp_api"])} />,
    );

    expect(screen.getByText("openid mcp_api")).toBeTruthy();
    expect(screen.getByText(/has no effect/)).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "remote identity provider's settings" })
        .getAttribute("href"),
    ).toBe("/providers/issuer-1/settings");
    expect(rbac.requested).toContainEqual(["org:read", "org:admin"]);
  });

  it("names the settings without linking for callers who cannot open them", () => {
    rbac.canReadOrg = false;
    render(<IssuerScopeOverrideAlert issuer={orgIssuer(["a"])} />);

    expect(screen.queryByRole("link")).toBeNull();
    expect(
      screen.getByText(/remote identity provider's settings/),
    ).toBeTruthy();
    expect(rbac.requested).toContainEqual(["org:read", "org:admin"]);
  });

  it("drops the edit instruction for a platform provider", () => {
    render(
      <IssuerScopeOverrideAlert
        issuer={{ id: "issuer-1", scopeOverride: ["openid"] }}
      />,
    );

    expect(screen.getByText("openid")).toBeTruthy();
    expect(screen.getByText(/has no effect/)).toBeTruthy();
    expect(
      screen.getByText(/can't be changed from this organization/),
    ).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.queryByText(/settings/)).toBeNull();
    expect(screen.queryByText(/Edit the scope override/)).toBeNull();
  });
});

describe("LegacyCallbackAlert", () => {
  it("renders nothing for a current client or a non-admin", () => {
    const { rerender, container } = render(
      <LegacyCallbackAlert
        legacyCallbackUrl={false}
        onMigrate={vi.fn<() => void>()}
      />,
    );
    expect(container.textContent).toBe("");

    rbac.isPlatformAdmin = false;
    rerender(
      <LegacyCallbackAlert legacyCallbackUrl onMigrate={vi.fn<() => void>()} />,
    );
    expect(container.textContent).toBe("");
  });

  it("hides Migrate from callers who cannot save", () => {
    render(
      <LegacyCallbackAlert
        legacyCallbackUrl
        onMigrate={vi.fn<() => void>()}
        canMigrate={false}
      />,
    );

    expect(screen.getByText(/runs in compatibility mode/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Migrate" })).toBeNull();
  });

  it("names both callback URLs and migrates only after confirmation", () => {
    const onMigrate = vi.fn<() => void>();
    render(<LegacyCallbackAlert legacyCallbackUrl onMigrate={onMigrate} />);

    expect(
      screen.getByText("https://app.example.com/oauth/callback"),
    ).toBeTruthy();
    expect(
      screen.getByText("https://app.example.com/mcp/remote_login_callback"),
    ).toBeTruthy();
    expect(screen.getByText(/runs in compatibility mode/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Migrate" }));
    expect(onMigrate).not.toHaveBeenCalled();

    const confirm = screen
      .getAllByRole("button", { name: "Migrate" })
      .find((button) => button.closest("[role='dialog']"));
    expect(confirm).toBeTruthy();
    expect(
      confirm!
        .closest("[role='dialog']")!
        .textContent?.includes(
          "https://app.example.com/mcp/remote_login_callback",
        ),
    ).toBe(true);
    fireEvent.click(confirm!);
    expect(onMigrate).toHaveBeenCalledTimes(1);
  });
});
