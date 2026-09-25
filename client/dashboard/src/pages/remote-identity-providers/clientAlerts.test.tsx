import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IssuerScopeOverrideAlert, LegacyCallbackAlert } from "./clientAlerts";

const rbac = vi.hoisted(() => ({ canReadOrg: true }));

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
  useRBAC: () => ({ hasAnyScope: () => rbac.canReadOrg }),
}));
vi.mock("react-router", () => ({
  Link: ({ to, children }: { to: string; children: ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

afterEach(() => {
  cleanup();
  rbac.canReadOrg = true;
});

describe("IssuerScopeOverrideAlert", () => {
  it("renders nothing when the issuer has no override", () => {
    const { container } = render(
      <>
        <IssuerScopeOverrideAlert issuerId="issuer-1" scopeOverride={null} />
        <IssuerScopeOverrideAlert issuerId="issuer-1" scopeOverride={[]} />
      </>,
    );

    expect(container.textContent).toBe("");
  });

  it("names the pinned scopes and links to the issuer's settings", () => {
    render(
      <IssuerScopeOverrideAlert
        issuerId="issuer-1"
        scopeOverride={["openid", "mcp_api"]}
      />,
    );

    expect(screen.getByText("openid mcp_api")).toBeTruthy();
    expect(screen.getByText(/has no effect/)).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "remote identity provider's settings" })
        .getAttribute("href"),
    ).toBe("/providers/issuer-1/settings");
  });

  it("names the settings without linking for callers who cannot open them", () => {
    rbac.canReadOrg = false;
    render(
      <IssuerScopeOverrideAlert issuerId="issuer-1" scopeOverride={["a"]} />,
    );

    expect(screen.queryByRole("link")).toBeNull();
    expect(
      screen.getByText(/remote identity provider's settings/),
    ).toBeTruthy();
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
