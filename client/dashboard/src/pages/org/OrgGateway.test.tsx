import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GatewayNotServed } from "./OrgGateway";

const flag = vi.hoisted(() => ({ status: "disabled" as string }));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: flag.status }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    identity: { href: () => "/org/identity" },
    domains: { href: () => "/org/domains" },
    userSessionIssuers: { href: () => "/org/user-session-issuers" },
  }),
}));

afterEach(() => {
  cleanup();
  flag.status = "disabled";
});

function show(
  reason: "no_organization_issuer" | "multiple_organization_issuers",
) {
  render(
    <MemoryRouter>
      <GatewayNotServed reason={reason} />
    </MemoryRouter>,
  );
}

describe("GatewayNotServed", () => {
  it("links to creating a sign-in when there is none and the sign-ins page is on", () => {
    flag.status = "enabled";
    show("no_organization_issuer");
    expect(
      screen.getByRole("link", { name: "Create one" }).getAttribute("href"),
    ).toBe("/org/user-session-issuers?create=1");
  });

  it("points to Consolidate into another when there are several", () => {
    flag.status = "enabled";
    show("multiple_organization_issuers");
    expect(
      screen.getByRole("link", { name: "Sign-ins" }).getAttribute("href"),
    ).toBe("/org/user-session-issuers");
    expect(screen.getByText(/Consolidate into another/)).toBeDefined();
  });

  it.each(["no_organization_issuer", "multiple_organization_issuers"] as const)(
    "keeps pointing to IDP and SSO for %s while the page is off",
    (reason) => {
      show(reason);
      expect(
        screen.getByRole("link", { name: "IDP and SSO" }).getAttribute("href"),
      ).toBe("/org/identity");
    },
  );
});
