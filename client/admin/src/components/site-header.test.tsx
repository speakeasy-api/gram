import { QueryClient } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationQuery, projectQuery } from "@/lib/adminQueries";
import { routeTree } from "@/routeTree.gen";
import { anOrganization, aProject } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  listOrganizations: vi.fn(),
  getOrganization: vi.fn(),
  getProject: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listOrganizationMembers: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getSession: mocks.getSession,
    listOrganizations: mocks.listOrganizations,
    getOrganization: mocks.getOrganization,
    getProject: mocks.getProject,
    listOrganizationProjects: mocks.listOrganizationProjects,
    listOrganizationMembers: mocks.listOrganizationMembers,
  };
});

const ORG = anOrganization();
const PROJECT = aProject();
const PROJECT_DETAIL = {
  ...PROJECT,
  organization_id: ORG.id,
  toolset_count: 0,
  deployment_count: 0,
  http_tool_count: 0,
  environment_count: 0,
  api_key_count: 0,
  assistant_count: 0,
};

beforeEach(() => {
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.listOrganizations.mockReset();
  mocks.listOrganizations.mockResolvedValue({ organizations: [ORG] });
  mocks.getOrganization.mockReset();
  // One record and no other. A mock that answers every argument alike lets a
  // crumb that reads the wrong parameter pass.
  mocks.getOrganization.mockImplementation((idOrSlug: string) =>
    idOrSlug === ORG.id || idOrSlug === ORG.slug
      ? Promise.resolve(ORG)
      : Promise.reject(new Error(`no organization ${idOrSlug}`)),
  );
  mocks.getProject.mockReset();
  // Rejects a wrong organization for the same reason the organization mock
  // rejects a wrong id: a mock that answers every scope alike lets a caller that
  // sends the wrong one pass.
  mocks.getProject.mockImplementation(
    (idOrSlug: string, organizationIdOrSlug?: string) => {
      const named = idOrSlug === PROJECT.id || idOrSlug === PROJECT.slug;
      const scoped =
        organizationIdOrSlug === undefined ||
        organizationIdOrSlug === ORG.id ||
        organizationIdOrSlug === ORG.slug;
      return named && scoped
        ? Promise.resolve(PROJECT_DETAIL)
        : Promise.reject(new Error(`no project ${idOrSlug}`));
    },
  );
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
  mocks.listOrganizationMembers.mockReset();
  mocks.listOrganizationMembers.mockResolvedValue({ members: [] });
});

afterEach(cleanup);

function bar(): HTMLElement {
  return screen.getByRole("navigation", { name: "breadcrumb" });
}

// Scoped to the bar: the sidebar's own nav items are list items too, so an
// unscoped listitem query counts them as crumbs. Inside the bar,
// BreadcrumbSeparator is role="presentation", so the list items are the crumbs
// and nothing else.
function crumbs(): string[] {
  return within(bar())
    .getAllByRole("listitem")
    .map((item) => item.textContent?.trim() ?? "");
}

// The trailing crumb is a span, so it has no href. Everything before it is an
// anchor the operator can go back through.
function hrefs(): (string | null)[] {
  return within(bar())
    .getAllByRole("link")
    .map((link) => link.getAttribute("href"));
}

function seeded(): QueryClient {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  qc.setQueryData(organizationQuery(ORG.slug).queryKey, ORG);
  // Two entries, because the two routes to this page read under two keys: the
  // record scopes the project by its organization and the standalone page
  // cannot.
  qc.setQueryData(projectQuery(PROJECT.slug).queryKey, PROJECT_DETAIL);
  qc.setQueryData(
    projectQuery(PROJECT.slug, ORG.slug).queryKey,
    PROJECT_DETAIL,
  );
  return qc;
}

function deferred<T>(): { promise: Promise<T>; settle: (value: T) => void } {
  let settle!: (value: T) => void;
  const promise = new Promise<T>((resolve) => {
    settle = resolve;
  });
  return { promise, settle };
}

describe("SiteHeader", () => {
  it("reads Organizations on the list", async () => {
    await renderRouteTree(routeTree, {
      initialPath: "/organizations",
      queryClient: seeded(),
    });

    expect(crumbs()).toEqual(["Organizations"]);
  });

  it("reads Organizations / Test Org / Overview on the record index", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
      queryClient: seeded(),
    });

    expect(crumbs()).toEqual(["Organizations", ORG.name, "Overview"]);
  });

  it("reads Organizations / Test Org / Members on the members view", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
      queryClient: seeded(),
    });

    expect(crumbs()).toEqual(["Organizations", ORG.name, "Members"]);
  });

  it("reads Organizations / Test Org / Projects on the projects list", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
      queryClient: seeded(),
    });

    expect(crumbs()).toEqual(["Organizations", ORG.name, "Projects"]);
  });

  it("ends in the project name, not Projects, on the single-project view", async () => {
    // The claim the spec makes explicitly, and the one a pathname-slicing
    // implementation gets wrong: the last path segment is the slug, and the
    // crumb has to be the project's name.
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects/${PROJECT.slug}`,
      queryClient: seeded(),
    });

    expect(crumbs()).toEqual(["Organizations", ORG.name, PROJECT.name]);
  });

  it("fills the project crumb from the view's own read, nothing seeded", async () => {
    // The one claim seeding cannot make. The bar watches the cache and never
    // fetches, so its key must be the key the view writes under, and both tests
    // above seed whatever key the crumb asks for. Here only the view reads, and
    // it reads scoped by the organization the URL names.
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    qc.setQueryData(organizationQuery(ORG.slug).queryKey, ORG);

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects/${PROJECT.slug}`,
      queryClient: qc,
    });

    await waitFor(() =>
      expect(crumbs()).toEqual(["Organizations", ORG.name, PROJECT.name]),
    );
    expect(mocks.getProject).toHaveBeenCalledWith(PROJECT.slug, ORG.slug);
  });

  it("names the standalone project page after its project", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/projects/${PROJECT.slug}`,
      queryClient: seeded(),
    });

    expect(crumbs()).toEqual(["Projects", PROJECT.name]);
  });

  // Every address the bar can stand on, with the crumb that ends its trail.
  // `/members` is the one address where position and "is this link's target the
  // current address" agree, so a bar tested only there hides the record index,
  // where the record crumb's own target is where the operator is standing.
  it.each([
    ["the organizations list", "/organizations", "Organizations"],
    ["a record's own index", `/organizations/${ORG.slug}`, "Overview"],
    ["the members view", `/organizations/${ORG.slug}/members`, "Members"],
    ["the projects list", `/organizations/${ORG.slug}/projects`, "Projects"],
    [
      "one project inside a record",
      `/organizations/${ORG.slug}/projects/${PROJECT.slug}`,
      PROJECT.name,
    ],
    ["the standalone project page", `/projects/${PROJECT.slug}`, PROJECT.name],
  ])(
    "marks only the last crumb as the current page on %s",
    async (_address, initialPath, last) => {
      // Not a role assertion: the vendored BreadcrumbPage carries role="link"
      // aria-disabled="true" aria-current="page", so getAllByRole("link")
      // returns the trailing crumb too. `ui/` may not be edited to change that,
      // so aria-current is the only honest discriminator.
      await renderRouteTree(routeTree, {
        initialPath,
        queryClient: seeded(),
      });

      const current = within(bar())
        .getAllByRole("listitem")
        .filter((item) => item.querySelector("[aria-current='page']") !== null);

      expect(current.map((item) => item.textContent?.trim())).toEqual([last]);
    },
  );

  it("leaves the router's active flag off a crumb nobody is standing on", async () => {
    // The other half of the router's guess, and the half the crumb cannot take
    // back after the fact. Every crumb's target is an ancestor of the address,
    // so without `exact` the router calls all three active and anything keyed
    // off `data-status` lights the whole trail.
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
      queryClient: seeded(),
    });

    const flags = within(bar())
      .getAllByRole("listitem")
      .map(
        (item) => item.querySelector("a")?.getAttribute("data-status") ?? null,
      );

    expect(flags).toEqual([null, null, null]);
  });

  it("links every crumb but the last back to its own view", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
      queryClient: seeded(),
    });

    expect(hrefs()).toEqual([
      "/organizations",
      `/organizations/${ORG.slug}`,
      null,
    ]);
  });

  it("sets a separator between each pair of crumbs", async () => {
    // The separator is aria-hidden and role="presentation", so a bar that lost
    // every one of them reads the same to every other assertion here and runs
    // three names together on screen.
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
      queryClient: seeded(),
    });

    const list = within(bar()).getByRole("list");
    const slots = Array.from(list.children).map((child) =>
      child.getAttribute("data-slot"),
    );

    expect(slots).toEqual([
      "breadcrumb-item",
      "breadcrumb-separator",
      "breadcrumb-item",
      "breadcrumb-separator",
      "breadcrumb-item",
    ]);
  });

  it("keeps a record reached by id addressed by id", async () => {
    // Linking the record crumb to the slug instead of the address it was
    // reached by sends one record to a second cache entry.
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.id}/members`,
      queryClient: (() => {
        const qc = seeded();
        qc.setQueryData(organizationQuery(ORG.id).queryKey, ORG);
        return qc;
      })(),
    });

    expect(hrefs()).toEqual([
      "/organizations",
      `/organizations/${ORG.id}`,
      null,
    ]);
  });

  it("drops a crumb whose record has not loaded yet", async () => {
    // The record never arrives, so its crumb resolves to nothing and is
    // dropped: the bar reads Organizations / Members rather than showing a
    // placeholder where the name will go.
    mocks.getOrganization.mockImplementation(() => new Promise(() => {}));

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

    expect(crumbs()).toEqual(["Organizations", "Members"]);
  });

  it("names the record once its fetch lands", async () => {
    // The bar renders before the record does on any address opened cold, so a
    // crumb read straight from the cache would stay missing until the next
    // navigation.
    const record = deferred<typeof ORG>();
    mocks.getOrganization.mockImplementation(() => record.promise);

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });
    expect(crumbs()).toEqual(["Organizations", "Members"]);

    record.settle(ORG);

    await waitFor(() => {
      expect(crumbs()).toEqual(["Organizations", ORG.name, "Members"]);
    });
  });

  it("follows the operator from one view to the next", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
      queryClient: seeded(),
    });
    expect(crumbs()).toEqual(["Organizations", ORG.name, "Overview"]);

    await router.navigate({
      to: "/organizations/$idOrSlug/members",
      params: { idOrSlug: ORG.slug },
    });

    await waitFor(() => {
      expect(crumbs()).toEqual(["Organizations", ORG.name, "Members"]);
    });
  });

  it("carries the operator up the trail when a crumb is clicked", async () => {
    // A real click, not `router.navigate`: the crumb is a hand-assembled anchor
    // rather than a `Link`, so the spread props are the only thing keeping the
    // click on the router, and navigating around the anchor cannot see that.
    const { router } = await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
      queryClient: seeded(),
    });
    expect(router.state.location.pathname).toBe(
      `/organizations/${ORG.slug}/members`,
    );

    fireEvent.click(within(bar()).getByRole("link", { name: ORG.name }));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe(`/organizations/${ORG.slug}`);
    });
  });
});
