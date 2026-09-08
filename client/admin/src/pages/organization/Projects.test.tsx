import { onlineManager, QueryClient } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationQuery } from "@/lib/adminQueries";
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
const PROJECT = aProject({
  created_at: "2026-01-03T00:00:00Z",
  // Not 0 and not 1: a cell that read the wrong field, or counted the rows,
  // would land on one of those.
  mcp_server_count: 7,
});

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
    ).toEqual(["Name", "Slug", "MCP Servers", "Created"]);
    expect(cellsOf(rowFor(link))).toEqual([
      PROJECT.name,
      PROJECT.slug,
      String(PROJECT.mcp_server_count),
      shortDate(PROJECT.created_at),
    ]);
  });

  it("counts a project with no MCP servers rather than leaving the cell blank", async () => {
    mocks.listOrganizationProjects.mockImplementation(() =>
      Promise.resolve({ projects: [aProject({ mcp_server_count: 0 })] }),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
    });

    const link = await screen.findByRole("link", { name: PROJECT.name });
    // A blank cell reads as a project nobody has measured. None is an answer
    // about a project that has been, and it is the answer an operator looking
    // for unused projects is scanning for.
    expect(cellsOf(rowFor(link))[2]).toBe("0");
  });

  it("draws the projects oldest first, whatever order the endpoint sent", async () => {
    // Shuffled on the wire. `organization.projects` promises no order, so a
    // view that renders what it was handed shows one organization two ways on
    // two reads.
    const middle = aProject({
      id: "proj_2",
      name: "Middle Project",
      slug: "middle-project",
      created_at: "2026-01-05T00:00:00Z",
    });
    const newest = aProject({
      id: "proj_3",
      name: "Newest Project",
      slug: "newest-project",
      created_at: "2026-02-01T00:00:00Z",
    });
    mocks.listOrganizationProjects.mockImplementation(() =>
      Promise.resolve({ projects: [newest, PROJECT, middle] }),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
    });
    await screen.findByRole("link", { name: PROJECT.name });

    expect(
      screen
        .getAllByRole("row")
        .slice(1)
        .map((row) => cellsOf(row)[0]),
    ).toEqual([PROJECT.name, middle.name, newest.name]);
  });

  it("counts the projects above the table, and says nothing while there is nothing to count", async () => {
    const second = aProject({
      id: "proj_2",
      name: "Second Project",
      slug: "second-project",
    });
    mocks.listOrganizationProjects.mockImplementation(() =>
      Promise.resolve({ projects: [PROJECT, second] }),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
    });

    expect(await screen.findByText("2 projects")).toBeTruthy();
  });

  it("says one project in the singular", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
    });

    expect(await screen.findByText("1 project")).toBeTruthy();
    expect(screen.queryByText("1 projects")).toBeNull();
  });

  it("draws no checkbox on a table that did not ask for one", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
    });
    await screen.findByRole("link", { name: PROJECT.name });

    // Row selection is opt-in, and it is opted into by putting the select
    // column in a page's own column list. The shared table registers the
    // feature for every page that uses it, and a feature that drew a column of
    // its own would put one here, where nothing reads a selection.
    expect(screen.queryAllByRole("checkbox")).toEqual([]);
  });

  it("keeps the operator inside the record when they open a project", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
    });

    const link = await screen.findByRole("link", { name: PROJECT.name });
    // Not /projects/{id}. That link leaves the record, and the record's own
    // project view is what the nav and the breadcrumb both point at. The
    // project segment is the id, because project.get resolves a slug across
    // every organization.
    expect(link.getAttribute("href")).toBe(
      `/organizations/${ORG.slug}/projects/${PROJECT.id}`,
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
      `/organizations/${ORG.id}/projects/${PROJECT.id}`,
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
        `/organizations/${ORG.id}/projects/${PROJECT.id}`,
      );
    });
  });

  it("re-addresses its links when the operator moves to another record", async () => {
    const other = anOrganization({
      id: "org_2",
      name: "Second Org",
      slug: "second-org",
    });
    const otherProject = aProject({
      id: "proj_2",
      name: "Second Project",
      slug: "second-project",
    });
    mocks.getOrganization.mockImplementation((idOrSlug: string) =>
      Promise.resolve(idOrSlug === other.slug ? other : ORG),
    );
    mocks.listOrganizationProjects.mockImplementation(
      (organizationID: string) =>
        Promise.resolve({
          projects: organizationID === other.id ? [otherProject] : [PROJECT],
        }),
    );
    // Both records in the cache, as `useOpenOrganization` leaves them. Without
    // that the second arrives pending, the layout drops its outlet for a beat,
    // and this table is rebuilt from scratch rather than carried over.
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    for (const org of [ORG, other]) {
      qc.setQueryData(organizationQuery(org.slug).queryKey, org);
    }

    const { router } = await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
      queryClient: qc,
    });
    await screen.findByRole("link", { name: PROJECT.name });

    // The same view, kept mounted, showing another record: what the back
    // button does between two records' project lists. Columns built once
    // address the record the operator has left.
    await router.navigate({
      to: "/organizations/$idOrSlug/projects",
      params: { idOrSlug: other.slug },
    });

    const link = await screen.findByRole("link", { name: otherProject.name });
    expect(link.getAttribute("href")).toBe(
      `/organizations/${other.slug}/projects/${otherProject.id}`,
    );
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
    // No "0 projects" line above it. The sentence in the table already says
    // that, and a count of nothing beside it says it twice.
    expect(screen.queryByText("0 projects")).toBeNull();
  });

  it("says it is loading rather than that there are none while the read is paused", async () => {
    // Offline, so the query is pending and not fetching. React Query calls that
    // neither loading nor errored, and a table that branches on `isLoading`
    // reaches the sentence meant for an answered read with no rows. The record
    // is seeded because its own read pauses too.
    onlineManager.setOnline(false);
    try {
      const qc = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      qc.setQueryData(organizationQuery(ORG.slug).queryKey, ORG);

      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${ORG.slug}/projects`,
        queryClient: qc,
      });

      expect(await screen.findByText("Loading...")).toBeTruthy();
      // An operator told an organization has no projects believes a fact about
      // the customer that no read ever established.
      expect(screen.queryByText("No projects in this organization")).toBeNull();
    } finally {
      onlineManager.setOnline(true);
    }
  });

  it("says the projects could not be read rather than that there are none", async () => {
    mocks.listOrganizationProjects.mockRejectedValue(
      new Error("projects read failed"),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
    });

    expect(await screen.findByText("Unable to load projects")).toBeTruthy();
    // One branch apart in the same cell, and the wrong one states a fact about
    // the customer that a failed read never established.
    expect(screen.queryByText("No projects in this organization")).toBeNull();
  });
});
