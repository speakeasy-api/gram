import { queryOptions } from "@tanstack/react-query";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminCustomDomain } from "@gram/admin-client/models/components/admincustomdomain";
import type { AdminListCustomDomainsRequest } from "@gram/admin-client/models/operations/adminlistcustomdomains";

import { routeTree } from "@/routeTree.gen";
import { renderRouteTree } from "@/test/harness";

import { routingMethodLabel } from "./customDomainModel";
import { customDomainsSearch } from "./customDomainsSearch";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  listCustomDomains: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return { ...actual, getSession: mocks.getSession };
});

vi.mock("@/lib/gramAdminClient", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminClient")>();
  return {
    ...actual,
    adminListCustomDomainsQuery: (
      request: AdminListCustomDomainsRequest = {},
    ) =>
      queryOptions({
        queryKey: ["customDomains", request],
        queryFn: () => mocks.listCustomDomains(request),
      }),
  };
});

// Invented names only: this repository is public.
function aDomain(
  overrides: Partial<AdminCustomDomain> = {},
): AdminCustomDomain {
  return {
    id: "domain_1",
    organizationId: "org_1",
    organizationName: "Test Org",
    organizationSlug: "test-org",
    domain: "mcp.example.test",
    verified: true,
    activated: true,
    dnsTargetToken: "abc123",
    dnsTarget: "abc123.targets.example.test",
    dnsRoutingMethod: "shared_cname",
    healthStatus: "healthy",
    healthCheckedAt: new Date("2026-03-02T10:15:00Z"),
    createdAt: new Date("2026-01-15T00:00:00Z"),
    ...overrides,
  };
}

beforeEach(() => {
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.listCustomDomains.mockReset();
  mocks.listCustomDomains.mockResolvedValue({ customDomains: [aDomain()] });
});

afterEach(() => {
  cleanup();
});

describe("routingMethodLabel", () => {
  it.each([
    ["per_domain_cname", "Per-domain CNAME"],
    ["shared_cname", "Shared CNAME"],
    ["a_record", "A record / flattened"],
    ["unknown", "Unknown"],
    [undefined, "Not checked"],
  ] as const)("labels %s as %s", (method, label) => {
    expect(routingMethodLabel(method).label).toBe(label);
  });
});

describe("customDomainsSearch", () => {
  it("keeps a known routing method", () => {
    expect(customDomainsSearch({ routing: "a_record" })).toEqual({
      routing: "a_record",
    });
  });

  it("drops a routing method this build does not know", () => {
    expect(customDomainsSearch({ routing: "mx_record" })).toEqual({
      routing: undefined,
    });
  });
});

describe("CustomDomains", () => {
  it("lists every domain unfiltered by default", async () => {
    await renderRouteTree(routeTree, { initialPath: "/custom-domains" });

    const cell = await screen.findByRole("cell", { name: "mcp.example.test" });
    expect(mocks.listCustomDomains).toHaveBeenCalledWith({
      dnsRoutingMethod: undefined,
    });

    const row = cell.closest("tr");
    if (!row) throw new Error("the domain cell is not inside a row");
    expect(within(row).getByText("Shared CNAME")).toBeTruthy();
    expect(within(row).getByText("Active")).toBeTruthy();
    expect(within(row).getByText("abc123.targets.example.test")).toBeTruthy();
    expect(
      within(row).getByRole("link", { name: "Test Org" }).getAttribute("href"),
    ).toBe("/organizations/test-org/custom-domain");
  });

  it("sends the routing method in the address to the server", async () => {
    mocks.listCustomDomains.mockResolvedValue({
      customDomains: [aDomain({ dnsRoutingMethod: "a_record" })],
    });

    await renderRouteTree(routeTree, {
      initialPath: "/custom-domains?routing=a_record",
    });

    await screen.findByRole("cell", { name: "mcp.example.test" });
    expect(mocks.listCustomDomains).toHaveBeenCalledWith({
      dnsRoutingMethod: "a_record",
    });
    expect(mocks.listCustomDomains).toHaveBeenCalledTimes(1);
    expect(
      screen.getByRole("combobox", { name: "Routing method" }).textContent,
    ).toContain("A record / flattened");
  });

  it("explains why a designated target is missing", async () => {
    mocks.listCustomDomains.mockResolvedValue({
      customDomains: [
        aDomain({
          dnsTarget: undefined,
          dnsTargetToken: undefined,
          dnsRoutingMethod: undefined,
          healthStatus: undefined,
          healthCheckedAt: undefined,
        }),
      ],
    });

    await renderRouteTree(routeTree, { initialPath: "/custom-domains" });

    await screen.findByText("Token not assigned yet");
    expect(screen.getAllByText("Not checked")).toHaveLength(3);
  });

  it("says when no domain uses the chosen routing method", async () => {
    mocks.listCustomDomains.mockResolvedValue({ customDomains: [] });

    await renderRouteTree(routeTree, {
      initialPath: "/custom-domains?routing=shared_cname",
    });

    await waitFor(() =>
      expect(
        screen.getByText("No custom domains use this routing method."),
      ).toBeTruthy(),
    );
  });
});
