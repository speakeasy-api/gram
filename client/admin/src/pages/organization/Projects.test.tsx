import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { routeTree } from "@/routeTree.gen";
import { anOrganization, aProject } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  getProject: vi.fn(),
  listOrganizationProjects: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getSession: mocks.getSession,
    getOrganization: mocks.getOrganization,
    getProject: mocks.getProject,
    listOrganizationProjects: mocks.listOrganizationProjects,
  };
});

const ORG = anOrganization();
const PROJECT = aProject({ created_at: "2026-01-03T00:00:00Z" });

function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { timeZone: "UTC" });
}

function cellsOf(row: HTMLElement): (string | null)[] {
  return within(row)
    .getAllByRole("cell")
    .map((cell) => cell.textContent);
}

function rowFor(link: HTMLElement): HTMLTableRowElement {
  const row = link.closest("tr");
  if (!row) throw new Error("the link is not inside a row");
  return row;
}

beforeEach(() => {
  // Midnight UTC is the previous day in this zone, so a cell that reads the
  // operator's own zone fails below. CI runs in UTC, where it would not.
  vi.stubEnv("TZ", "America/Los_Angeles");
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(ORG);
  // Both endpoints answer the record they were asked about and nothing else. A
  // mock that answers every argument alike cannot tell a view that reads the
  // right parameter from one that reads whatever is next to it.
  mocks.getProject.mockReset();
  mocks.getProject.mockImplementation((idOrSlug: string) =>
    idOrSlug === PROJECT.slug || idOrSlug === PROJECT.id
      ? Promise.resolve({
          ...PROJECT,
          organization_id: ORG.id,
          toolset_count: 0,
          deployment_count: 0,
          http_tool_count: 0,
          environment_count: 0,
          api_key_count: 0,
          assistant_count: 0,
        })
      : Promise.reject(new Error(`no project ${idOrSlug}`)),
  );
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockImplementation((organizationID: string) =>
    organizationID === ORG.id
      ? Promise.resolve({ projects: [PROJECT] })
      : Promise.reject(new Error(`no organization ${organizationID}`)),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllEnvs();
});

describe("Projects", () => {
  it("renders every projects cell out of the record that produced it", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
    });

    const link = await screen.findByRole("link", { name: PROJECT.name });

    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual(["Name", "Slug", "ID", "Created"]);
    expect(cellsOf(rowFor(link))).toEqual([
      PROJECT.name,
      PROJECT.slug,
      PROJECT.id,
      shortDate(PROJECT.created_at),
    ]);
  });

  it("keeps the operator inside the record when they open a project", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
    });

    const link = await screen.findByRole("link", { name: PROJECT.name });
    // Not /projects/{slug}. That link leaves the record, and the record's own
    // project view is what the nav and the breadcrumb both point at.
    expect(link.getAttribute("href")).toBe(
      `/organizations/${ORG.slug}/projects/${PROJECT.slug}`,
    );
  });

  it("keeps a record reached by id addressed by id", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.id}/projects`,
    });

    // Reading the address off the record instead of the route sends every
    // project link to a second cache entry for one organization, and costs a
    // second read of the record to fill it.
    const link = await screen.findByRole("link", { name: PROJECT.name });
    expect(link.getAttribute("href")).toBe(
      `/organizations/${ORG.id}/projects/${PROJECT.slug}`,
    );
  });

  it("keeps that address when the row itself is clicked", async () => {
    // The row and the link are two ways to the same project, and only one of
    // them is an anchor a test can read an href off.
    const { router } = await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.id}/projects`,
    });

    const link = await screen.findByRole("link", { name: PROJECT.name });
    fireEvent.click(rowFor(link));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe(
        `/organizations/${ORG.id}/projects/${PROJECT.slug}`,
      );
    });
  });

  it("renders the project under the record it belongs to", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects/${PROJECT.slug}`,
    });

    // The record's own name stays on screen: the project view is a view of the
    // record, not a page that replaced it.
    expect(await screen.findByRole("heading", { name: ORG.name })).toBeTruthy();
    expect(
      await screen.findByRole("heading", { name: PROJECT.name }),
    ).toBeTruthy();
  });

  it("says the organization has no projects rather than showing an empty table", async () => {
    mocks.listOrganizationProjects.mockImplementation(() =>
      Promise.resolve({ projects: [] }),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
    });

    expect(
      await screen.findByText("No projects in this organization"),
    ).toBeTruthy();
  });
});
