import { QueryClient } from "@tanstack/react-query";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationQuery } from "@/lib/adminQueries";
import { GramAdminError } from "@/lib/gramAdminApi";
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
const OTHER_ORG = anOrganization({
  id: "org_2",
  name: "Second Org",
  slug: "second-org",
  member_count: 7,
});
const PROJECT = aProject();

const RECORDS = [ORG, OTHER_ORG];

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
  // sidebar that reads the wrong parameter pass.
  mocks.getOrganization.mockImplementation((idOrSlug: string) => {
    const found = RECORDS.find(
      (org) => org.id === idOrSlug || org.slug === idOrSlug,
    );
    return found
      ? Promise.resolve(found)
      : Promise.reject(new Error(`no organization ${idOrSlug}`));
  });
  mocks.getProject.mockReset();
  mocks.getProject.mockResolvedValue({
    ...PROJECT,
    organization_id: ORG.id,
    toolset_count: 0,
    deployment_count: 0,
    http_tool_count: 0,
    environment_count: 0,
    api_key_count: 0,
    assistant_count: 0,
  });
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
  mocks.listOrganizationMembers.mockReset();
  mocks.listOrganizationMembers.mockResolvedValue({ members: [] });
});

afterEach(cleanup);

// The breadcrumb names the same views the nav does, so every link query here
// has to say which of the two it means.
function sidebar(): HTMLElement {
  const found = document.querySelector("[data-slot='sidebar']");
  if (!(found instanceof HTMLElement)) throw new Error("no sidebar on screen");
  return found;
}

function hrefs(): (string | null)[] {
  return within(sidebar())
    .getAllByRole("link")
    .map((link) => link.getAttribute("href"));
}

// The record name is on the page as well as in the nav, so a name has to be
// looked for in one of them and not the other.
function inTheNav(text: string): boolean {
  return sidebar().textContent?.includes(text) ?? false;
}

// The count is a `SidebarMenuBadge`, a sibling of the link rather than part of
// it, so the item is what carries label and count together.
function navItem(label: string): HTMLElement {
  const item = within(sidebar())
    .getByRole("link", { name: label })
    .closest("[data-slot='sidebar-menu-item']");
  if (!(item instanceof HTMLElement)) {
    throw new Error(`no sidebar menu item around the ${label} link`);
  }
  return item;
}

function isActive(name: string): boolean {
  return (
    within(sidebar())
      .getByRole("link", { name })
      .getAttribute("data-active") === "true"
  );
}

describe("AppSidebar", () => {
  it("renders the global nav outside a record", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    expect(hrefs()).toContain("/projects");
    expect(
      screen.queryByRole("link", { name: "All organizations" }),
    ).toBeNull();
  });

  it("renders the record nav inside a record", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    expect(
      await screen.findByRole("link", { name: "All organizations" }),
    ).toBeTruthy();
    // The record nav replaces the global one. Two navs on screen at once was
    // the rejected shape.
    expect(hrefs()).not.toContain("/projects");
  });

  it("falls back to the global nav when the record fails to load", async () => {
    mocks.getOrganization.mockRejectedValue(
      new GramAdminError(404, { message: "organization not found" }, "404"),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByText(/organization not found/);
    expect(
      screen.queryByRole("link", { name: "All organizations" }),
    ).toBeNull();
    expect(hrefs()).toContain("/projects");
  });

  it("keeps the record nav when a refetch over the record fails", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    qc.setQueryData(organizationQuery(ORG.slug).queryKey, ORG);
    mocks.getOrganization.mockRejectedValue(
      new GramAdminError(500, { message: "organization read failed" }, "500"),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
      queryClient: qc,
    });

    await waitFor(() => {
      expect(
        qc.getQueryState(organizationQuery(ORG.slug).queryKey)?.status,
      ).toBe("error");
    });
    // The layout holds the record through the same failure, so a global nav
    // here would take the record's own nav away from a record still on screen.
    expect(
      screen.getByRole("link", { name: "All organizations" }),
    ).toBeTruthy();
    expect(hrefs()).not.toContain("/projects");
  });

  it("falls back to the global nav while the record is still loading", async () => {
    mocks.getOrganization.mockImplementation(() => new Promise(() => {}));

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByText("Loading...");
    // A record nav with no record in it names nothing and counts nothing.
    expect(
      screen.queryByRole("link", { name: "All organizations" }),
    ).toBeNull();
  });

  it("gives the record nav back when the operator leaves the record", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    await router.navigate({ to: "/organizations" });

    await waitFor(() => {
      expect(
        screen.queryByRole("link", { name: "All organizations" }),
      ).toBeNull();
    });
    expect(hrefs()).toContain("/projects");
  });

  it("follows the operator from one record to the next", async () => {
    // Both records already in the cache, which is what `useOpenOrganization`
    // leaves behind. Without them the second record arrives pending, the
    // sidebar drops to the global nav for a beat, and that unmount clears
    // anything the nav was holding from the first record: the carryover this
    // test is about becomes invisible.
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    for (const org of RECORDS) {
      qc.setQueryData(organizationQuery(org.slug).queryKey, org);
    }

    const { router } = await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
      queryClient: qc,
    });
    expect(inTheNav(ORG.name)).toBe(true);

    await router.navigate({
      to: "/organizations/$idOrSlug",
      params: { idOrSlug: OTHER_ORG.slug },
    });

    // The nav is not remounted between records, so anything it holds from the
    // first one stays on screen naming the wrong organization.
    await waitFor(() => {
      expect(inTheNav(OTHER_ORG.name)).toBeTruthy();
    });
    expect(navItem("Members").textContent).toContain("7");
  });

  it("keeps a record reached by id on its id", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.id}`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    // Handing the nav the record's slug instead of the address it was reached
    // by makes every item a redirect to a second cache entry for one record.
    expect(
      within(sidebar())
        .getByRole("link", { name: "Members" })
        .getAttribute("href"),
    ).toBe(`/organizations/${ORG.id}/members`);
  });

  it("lights Overview on the record's own view and nothing else", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    expect(isActive("Overview")).toBe(true);
    expect(isActive("Projects")).toBe(false);
    expect(isActive("Members")).toBe(false);
  });

  it("keeps Projects lit while one of its projects is shown", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects/${PROJECT.slug}`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    // A project is inside the Projects branch, so Overview is not the view the
    // operator is on.
    expect(isActive("Projects")).toBe(true);
    expect(isActive("Overview")).toBe(false);
  });

  it("offers one way out of a project, not two", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects/${PROJECT.slug}`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    // The nav's back row and the page's own back button were the same move in
    // the same viewport once the record gained a nav.
    expect(screen.queryByText(/Back to organization/)).toBeNull();
  });

  it("lights Members on the members view and not Overview", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    expect(isActive("Members")).toBe(true);
    expect(isActive("Overview")).toBe(false);
  });
});
