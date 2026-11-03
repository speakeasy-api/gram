import { queryOptions } from "@tanstack/react-query";
import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminListCustomDomainsRequest } from "@gram/admin-client/models/operations/adminlistcustomdomains";

import { routeTree } from "@/routeTree.gen";
import { anOrganization } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listCustomDomains: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getSession: mocks.getSession,
    getOrganization: mocks.getOrganization,
    listOrganizationProjects: mocks.listOrganizationProjects,
  };
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

const ORG = anOrganization();

beforeEach(() => {
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(ORG);
  // Not this view's query: the record nav asks for it on every view.
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
  mocks.listCustomDomains.mockReset();
  mocks.listCustomDomains.mockResolvedValue({ customDomains: [] });
});

afterEach(() => {
  cleanup();
});

describe("CustomDomain", () => {
  it("says so when the organization has no custom domain", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/custom-domain`,
    });

    await screen.findByText("This organization has no custom domain.");
    // The organization's id, not the slug in the address.
    expect(mocks.listCustomDomains).toHaveBeenCalledWith({
      organizationId: ORG.id,
    });
  });

  it("shows the organization's domain and its routing", async () => {
    mocks.listCustomDomains.mockResolvedValue({
      customDomains: [
        {
          id: "domain_1",
          organizationId: ORG.id,
          organizationName: ORG.name,
          organizationSlug: ORG.slug,
          domain: "mcp.example.test",
          verified: true,
          activated: false,
          dnsTargetToken: "abc123",
          dnsRoutingMethod: "per_domain_cname",
          healthStatus: "unhealthy",
          healthIssue: "CNAME points somewhere else",
          healthCheckedAt: new Date("2026-03-02T10:15:00Z"),
          createdAt: new Date("2026-01-15T00:00:00Z"),
        },
      ],
    });

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/custom-domain`,
    });

    await screen.findByText("mcp.example.test");
    expect(screen.getByText("Per-domain CNAME")).toBeTruthy();
    expect(screen.getByText("Verified, not activated")).toBeTruthy();
    expect(screen.getByText("Unhealthy")).toBeTruthy();
    expect(screen.getByText("CNAME points somewhere else")).toBeTruthy();
    // A token with no target means the admin server has no suffix configured.
    expect(
      screen.getByText("No target suffix configured on this admin server"),
    ).toBeTruthy();
  });
});
