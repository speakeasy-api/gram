import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { GramAdminError } from "@/lib/gramAdminApi";
import { routeTree } from "@/routeTree.gen";
import { anOrganization } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listOrganizationMembers: vi.fn(),
}));

// Only the endpoints this route reaches are replaced. The rest of the module
// stays real, so toSearchParams still decides what a request carries.
vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getSession: mocks.getSession,
    getOrganization: mocks.getOrganization,
    listOrganizationProjects: mocks.listOrganizationProjects,
    listOrganizationMembers: mocks.listOrganizationMembers,
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
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
  mocks.listOrganizationMembers.mockReset();
  mocks.listOrganizationMembers.mockResolvedValue({ members: [] });
});

afterEach(cleanup);

describe("RecordLayout", () => {
  it("renders the record name once the query resolves", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    expect(await screen.findByRole("heading", { name: ORG.name })).toBeTruthy();
  });

  it("renders the record name on every view, not only the index", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

    expect(await screen.findByRole("heading", { name: ORG.name })).toBeTruthy();
  });

  it("says it is loading rather than reporting an error it has not had", async () => {
    // Pending forever. A record that has not arrived is not a record that
    // failed: without the loading branch this paints "Error:" with nothing
    // after it, for as long as the request takes.
    mocks.getOrganization.mockImplementation(() => new Promise(() => {}));

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    expect(await screen.findByText("Loading...")).toBeTruthy();
    expect(screen.queryByText(/Error/)).toBeNull();
  });

  it("renders no view at all when the record fails to load", async () => {
    mocks.getOrganization.mockRejectedValue(
      new GramAdminError(404, { message: "organization not found" }, "404"),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    expect(await screen.findByText(/organization not found/)).toBeTruthy();
    // A record that failed to load has no views. Rendering the outlet anyway
    // leaves the operator reading a card of dashes for an organization that
    // may not exist.
    expect(screen.queryByText("Account type")).toBeNull();
  });
});
