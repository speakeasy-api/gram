import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GatewayNotServed } from "./organization-gateway-setting";

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    setup: { href: () => "/acme/setup" },
    identity: { href: () => "/acme/identity" },
    domains: { href: () => "/acme/domains" },
  }),
}));

afterEach(cleanup);

function renderReason(
  reason: Parameters<typeof GatewayNotServed>[0]["reason"],
) {
  return render(
    <MemoryRouter>
      <GatewayNotServed reason={reason} />
    </MemoryRouter>,
  );
}

describe("GatewayNotServed", () => {
  it("sends a missing sign-in to the setup card that creates it", () => {
    renderReason("no_organization_issuer");

    const link = screen.getByRole("link");
    expect(link.textContent).toBe("Set up Anthropic admin controls");
    expect(link.getAttribute("href")).toBe(
      "/acme/setup?task=anthropic-admin-controls",
    );
    expect(screen.queryByText(/IDP and SSO/)).toBeNull();
  });

  it("does not claim a dashboard page removes extra sign-ins", () => {
    renderReason("multiple_organization_issuers");

    expect(screen.getByText(/more than one organization-wide/)).toBeTruthy();
    expect(screen.getByText(/Contact support/)).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
  });

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
