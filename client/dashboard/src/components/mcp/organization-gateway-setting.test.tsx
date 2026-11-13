import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GatewayNotServed } from "./organization-gateway-setting";

const state = vi.hoisted(() => ({
  admin: true,
  issuerDashboard: "enabled",
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "project" }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1", slug: "acme" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, organizationId: string) =>
      organizationId === "org-1" && (scope !== "org:admin" || state.admin),
  }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: state.issuerDashboard }),
}));

afterEach(cleanup);
beforeEach(() => {
  state.admin = true;
  state.issuerDashboard = "enabled";
});

function renderReason(
  reason: Parameters<typeof GatewayNotServed>[0]["reason"],
) {
  return render(
    <MemoryRouter>
      <GatewayNotServed reason={reason} />
    </MemoryRouter>,
  );
}

const issuersLink = () =>
  screen.queryByRole("link", { name: "Speakeasy Authorization Servers" });

describe("GatewayNotServed", () => {
  it("sends admins to make a project's server organization-wide", () => {
    renderReason("no_organization_issuer");

    expect(
      screen.getByText(/Make a project's authorization server/),
    ).toBeTruthy();
    expect(issuersLink()?.getAttribute("href")).toBe(
      "/acme/user-session-issuers",
    );
    expect(screen.queryByText(/IDP and SSO/)).toBeNull();
  });

  it("sends admins to consolidate extra organization-wide servers", () => {
    renderReason("multiple_organization_issuers");

    expect(screen.getByText(/Consolidate into another/)).toBeTruthy();
    expect(issuersLink()?.getAttribute("href")).toBe(
      "/acme/user-session-issuers",
    );
  });

  it.each(["no_organization_issuer", "multiple_organization_issuers"] as const)(
    "falls back to support for %s when the issuer dashboard is off",
    (reason) => {
      state.issuerDashboard = "disabled";

      renderReason(reason);

      expect(screen.getByText(/Contact support/)).toBeTruthy();
      expect(screen.queryByRole("link")).toBeNull();
    },
  );

  it.each(["no_organization_issuer", "multiple_organization_issuers"] as const)(
    "sends members to an admin for %s without a link",
    (reason) => {
      state.admin = false;

      renderReason(reason);

      expect(
        screen.getByText(/Ask an organization admin or contact support/),
      ).toBeTruthy();
      expect(screen.queryByRole("link")).toBeNull();
    },
  );

  it("still links an allowlisted custom domain to Network Access", () => {
    renderReason("custom_domain_only");

    expect(screen.getByRole("link", { name: "Network Access" })).toBeTruthy();
  });

  it.each(["turned_off", "organization_disabled", undefined] as const)(
    "says nothing for %s",
    (reason) => {
      const { container } = renderReason(reason);
      expect(container.textContent).toBe("");
    },
  );
});
