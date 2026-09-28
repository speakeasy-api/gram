import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IssuerScopeOverrideAlert, LegacyCallbackAlert } from "./clientAlerts";

const rbac = vi.hoisted(() => ({
  canReadOrg: true,
  requested: [] as string[][],
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
vi.mock("react-router", () => ({
  Link: ({ to, children }: { to: string; children: ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

afterEach(() => {
  cleanup();
  rbac.canReadOrg = true;
  rbac.requested = [];
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
  it("warns only for legacy callback clients", () => {
    const { rerender, container } = render(
      <LegacyCallbackAlert legacyCallbackUrl={false} />,
    );
    expect(container.textContent).toBe("");

    rerender(<LegacyCallbackAlert legacyCallbackUrl />);
    expect(screen.getByText(/uses the legacy callback URLs/)).toBeTruthy();
  });
});
