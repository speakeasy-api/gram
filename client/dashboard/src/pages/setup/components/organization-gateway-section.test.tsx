import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PlatformSetupStep } from "../types";
import { OrganizationGatewayConnectorSection } from "./organization-gateway-section";

const GATEWAY_URL = "https://api.example.test/mcp/gateway/acme";

const state = vi.hoisted(() => ({
  scopes: ["org:read", "org:admin"] as string[],
  gateway: {} as {
    data?: Record<string, unknown>;
    isLoading?: boolean;
    isFetchedAfterMount?: boolean;
    error?: unknown;
  },
  queryOptions: undefined as Record<string, unknown> | undefined,
  refetch: vi.fn(),
  issuerDashboard: "disabled",
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, organizationId: string) =>
      organizationId === "org-1" && state.scopes.includes(scope),
    isLoading: false,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1", slug: "acme" }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    gateway: { href: () => "/acme/gateway" },
    identity: { href: () => "/acme/identity" },
    domains: { href: () => "/acme/domains" },
    userSessionIssuers: { href: () => "/acme/user-session-issuers" },
  }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: state.issuerDashboard }),
}));
vi.mock("@gram/client/react-query/organizationGateway.js", () => ({
  useOrganizationGateway: (
    _a: unknown,
    _b: unknown,
    options: Record<string, unknown>,
  ) => {
    state.queryOptions = options;
    return {
      isFetching: false,
      isFetchedAfterMount: true,
      refetch: state.refetch,
      ...state.gateway,
    };
  },
}));
vi.mock("./platform-setup-steps", () => ({
  PlatformSetupStepBody: ({
    step,
    onEligibilityAnswer,
  }: {
    step: PlatformSetupStep;
    onEligibilityAnswer: (eligible: boolean) => void;
  }) => (
    <div>
      <h4>{step.title}</h4>
      {step.fields?.map((field) => (
        <code key={field.label}>{field.value}</code>
      ))}
      {step.eligibility ? (
        <>
          <button onClick={() => onEligibilityAnswer(true)}>Yes</button>
          <button onClick={() => onEligibilityAnswer(false)}>No</button>
        </>
      ) : null}
    </div>
  ),
}));

afterEach(cleanup);
beforeEach(() => {
  state.scopes = ["org:read", "org:admin"];
  state.gateway = {};
  state.queryOptions = undefined;
  state.refetch.mockClear();
  state.issuerDashboard = "disabled";
});

function renderSection() {
  return render(
    <MemoryRouter>
      <OrganizationGatewayConnectorSection />
    </MemoryRouter>,
  );
}

function unavailable(reason?: string) {
  state.gateway = { data: { available: false, unavailableReason: reason } };
}

function expectNoUrlOrSteps() {
  expect(screen.queryByText(/https:\/\//)).toBeNull();
  expect(screen.queryByText("Plan check")).toBeNull();
}

describe("OrganizationGatewayConnectorSection", () => {
  it("shows the served URL with organization and personal connector guidance", () => {
    state.gateway = { data: { available: true, url: GATEWAY_URL } };

    renderSection();

    expect(screen.getByText("Plan check")).toBeTruthy();
    expect(screen.queryByText(GATEWAY_URL)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Yes" }));
    expect(
      screen.getByText("Add the connector for your organization"),
    ).toBeTruthy();
    expect(screen.getByText(GATEWAY_URL)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "No" }));
    expect(screen.getByText("Add a custom connector")).toBeTruthy();
    expect(
      screen.queryByText("Add the connector for your organization"),
    ).toBeNull();
    expect(screen.getByText(GATEWAY_URL)).toBeTruthy();
  });

  it("asks the gateway again on mount instead of trusting a cached answer", () => {
    state.gateway = { data: { available: true, url: GATEWAY_URL } };

    renderSection();

    expect(state.queryOptions).toMatchObject({
      enabled: true,
      refetchOnMount: "always",
    });
  });

  it("holds a cached URL back until this card's own read returns", () => {
    state.gateway = {
      data: { available: true, url: GATEWAY_URL },
      isFetchedAfterMount: false,
    };

    renderSection();

    expect(screen.getByText(/Loading the Organization Gateway/)).toBeTruthy();
    expectNoUrlOrSteps();
  });

  it("links admins to consolidation when the issuer dashboard is on", () => {
    state.issuerDashboard = "enabled";
    unavailable("multiple_organization_issuers");

    renderSection();

    expect(
      screen
        .getByRole("link", { name: "Speakeasy Authorization Servers" })
        .getAttribute("href"),
    ).toBe("/acme/user-session-issuers");
    expect(screen.getByRole("button", { name: "Check again" })).toBeTruthy();
    expectNoUrlOrSteps();
  });

  it("shows no URL when available is set without one", () => {
    state.gateway = { data: { available: true } };

    renderSection();

    expectNoUrlOrSteps();
    expect(screen.getByText(/isn't available/)).toBeTruthy();
  });

  it("sends a turned-off gateway to its review page rather than turning it on", () => {
    unavailable("turned_off");

    renderSection();

    expect(
      screen.getByText(/Review MCP assignments and turn it on/),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Organization Gateway" })
        .getAttribute("href"),
    ).toBe("/acme/gateway");
    expect(screen.queryByRole("switch")).toBeNull();
    expectNoUrlOrSteps();
  });

  it("tells members without an admin role who turns it on", () => {
    state.scopes = ["org:read"];
    unavailable("turned_off");

    renderSection();

    expect(
      screen.getByText(/An organization admin must review MCP assignments/),
    ).toBeTruthy();
  });

  it("sends admins to support for a missing sign-in, without a setup link", () => {
    unavailable("no_organization_issuer");

    renderSection();

    expect(screen.getByText(/doesn't have one/)).toBeTruthy();
    expect(screen.getByText(/Contact support to set one up/)).toBeTruthy();
    expect(screen.queryByText(/opening this setup card/)).toBeNull();
    expect(screen.queryByRole("link")).toBeNull();
    expectNoUrlOrSteps();
    fireEvent.click(screen.getByRole("button", { name: "Check again" }));
    expect(state.refetch).toHaveBeenCalled();
  });

  it("sends members to an admin or support for a missing sign-in", () => {
    state.scopes = ["org:read"];
    unavailable("no_organization_issuer");

    renderSection();

    expect(
      screen.getByText(/Ask an organization admin or contact support/),
    ).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
    expectNoUrlOrSteps();
  });

  it("does not offer a creation link for several sign-ins", () => {
    unavailable("multiple_organization_issuers");

    renderSection();

    expect(screen.getByText(/more than one/)).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
    expectNoUrlOrSteps();
  });

  it.each(["organization_disabled", "something_new", undefined])(
    "never reads as on or offers to turn it on for %s",
    (reason) => {
      unavailable(reason);

      renderSection();

      expect(screen.getByText(/isn't available/)).toBeTruthy();
      expect(screen.queryByText(/\bon\b/i)).toBeNull();
      expect(screen.queryByRole("link")).toBeNull();
      expectNoUrlOrSteps();
    },
  );

  it("does not read the gateway without organization read access", () => {
    state.scopes = [];
    state.gateway = { data: { available: true, url: GATEWAY_URL } };

    renderSection();

    expect(state.queryOptions?.enabled).toBe(false);
    expect(screen.getByText(/Ask an organization admin/)).toBeTruthy();
    expect(screen.queryByText(GATEWAY_URL)).toBeNull();
  });

  it("offers a retry when the gateway read fails", () => {
    state.gateway = { error: new Error("boom") };

    renderSection();

    expect(screen.getByRole("button", { name: "Check again" })).toBeTruthy();
  });
});
