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

const RECORD_NAV = [
  "All organizations",
  "Overview",
  "Projects",
  "Features",
  "Members",
];

// Anchored rather than exact: Features carries the rest of its accessible name
// to say where it goes, and every other item is its label and nothing else.
function navLink(label: string): HTMLElement {
  return within(sidebar()).getByRole("link", {
    name: new RegExp(`^${label}`),
  });
}

// The whole destination, spelled out. `__GRAM_APP_URL__` is the origin
// vitest.config.ts substitutes. A test that merely looks for "features" in the
// href passes on a link to the operator's own organization, which is the
// mistake this row exists to avoid.
function featuresLink(slug: string): string {
  return `https://app.gram.test/rpc/auth.login?redirect=%2F${slug}%2Fplatform-admin%2Ffeatures`;
}

// The items in the order they are read, named by the label each one opens with.
function navOrder(): string[] {
  return within(sidebar())
    .getAllByRole("link")
    .flatMap((link) => {
      const label = RECORD_NAV.find((l) => link.textContent?.startsWith(l));
      return label ? [label] : [];
    });
}

// Every item, both answers: the highlight a sighted operator reads and the
// state a screen reader is told. Asserting only the item expected to be current
// passes while three others also claim to be the page.
function navState(): Record<string, { active: boolean; current: boolean }> {
  return Object.fromEntries(
    RECORD_NAV.map((label) => {
      const link = navLink(label);
      return [
        label,
        {
          active: link.getAttribute("data-active") === "true",
          current: link.getAttribute("aria-current") === "page",
        },
      ];
    }),
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

  it("asks for no record at all while the operator is off one", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: ORG.name });

    // A query left enabled here asks for the organization named by an empty
    // string on every page the operator visits. Nothing on screen says so: a
    // rejected read and a disabled one both draw the global nav.
    expect(mocks.getOrganization).not.toHaveBeenCalled();
  });

  it("keeps the global nav's section lit while the operator is inside it", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/projects/${PROJECT.slug}`,
    });
    await screen.findByRole("link", { name: "Organizations" });

    // A project outside any record is still the Projects section. Matching the
    // address exactly unlights the nav the moment anything in it is opened.
    expect(isActive("Projects")).toBe(true);
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

  it("names one current page on the record's own view", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    expect(navState()).toEqual({
      "All organizations": { active: false, current: false },
      Overview: { active: true, current: true },
      Projects: { active: false, current: false },
      Features: { active: false, current: false },
      Members: { active: false, current: false },
    });
  });

  it("names one current page on the members view", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    // The record's address begins with both `/organizations` and the record's
    // own index, so those two items are the ones that claim a page they are not.
    expect(navState()).toEqual({
      "All organizations": { active: false, current: false },
      Overview: { active: false, current: false },
      Projects: { active: false, current: false },
      Features: { active: false, current: false },
      Members: { active: true, current: true },
    });
  });

  it("names one current page on the projects view", async () => {
    mocks.listOrganizationProjects.mockResolvedValue({
      projects: [PROJECT, aProject({ id: "proj_2", slug: "second-project" })],
    });

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    expect(navState()).toEqual({
      "All organizations": { active: false, current: false },
      Overview: { active: false, current: false },
      Projects: { active: true, current: true },
      Features: { active: false, current: false },
      Members: { active: false, current: false },
    });
  });

  it("names one current page on the projects view of a record with one project", async () => {
    mocks.listOrganizationProjects.mockResolvedValue({ projects: [PROJECT] });

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    // The item points past this address, at the one project, so the link's own
    // account of the current page is inverted here: the highlighted item is the
    // only one it calls an ordinary link.
    await waitFor(() => {
      expect(
        within(sidebar())
          .getByRole("link", { name: "Projects" })
          .getAttribute("href"),
      ).toBe(`/organizations/${ORG.slug}/projects/${PROJECT.id}`);
    });
    expect(navState()).toEqual({
      "All organizations": { active: false, current: false },
      Overview: { active: false, current: false },
      Projects: { active: true, current: true },
      Features: { active: false, current: false },
      Members: { active: false, current: false },
    });
  });

  it("names one current page while one of the record's projects is shown", async () => {
    mocks.listOrganizationProjects.mockResolvedValue({
      projects: [PROJECT, aProject({ id: "proj_2", slug: "second-project" })],
    });

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/projects/${PROJECT.slug}`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    // A project is under the Projects branch, which the item is current for.
    // Exactness alone would drop the mark here.
    expect(navState()).toEqual({
      "All organizations": { active: false, current: false },
      Overview: { active: false, current: false },
      Projects: { active: true, current: true },
      Features: { active: false, current: false },
      Members: { active: false, current: false },
    });
  });

  for (const [view, path] of [
    ["the record's own view", ""],
    ["the projects view", "/projects"],
    ["the members view", "/members"],
  ]) {
    it(`sends Features to the record's feature switches in Gram from ${view}`, async () => {
      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${ORG.slug}${path}`,
      });
      await screen.findByRole("link", { name: "All organizations" });

      expect(navLink("Features").getAttribute("href")).toBe(
        featuresLink(ORG.slug),
      );
    });
  }

  it("keeps the record's items in the order the design draws them", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    // Every other assertion here reads one item on its own and passes with the
    // rows in any order at all, so position is only pinned here. Features sits
    // between Projects and Members.
    expect(navOrder()).toEqual(RECORD_NAV);
  });

  it("builds Features from the slug even when the record was opened by id", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.id}`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    // Deliberately the one item that does not keep the address the operator
    // arrived on. Every other item would send the record to a second cache
    // entry; this one is read back by the server as the organization, and an
    // id there lands the operator nowhere.
    expect(navLink("Members").getAttribute("href")).toBe(
      `/organizations/${ORG.id}/members`,
    );
    expect(navLink("Features").getAttribute("href")).toBe(
      featuresLink(ORG.slug),
    );
  });

  it("says Features leaves the app, and names no referrer", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    const link = navLink("Features");
    // The operator is working through a record and this is the one item that
    // leaves the admin app. Taking the tab loses the record.
    expect(link.getAttribute("target")).toBe("_blank");
    // The admin address names the organization being looked at.
    expect(link.getAttribute("rel")).toContain("noreferrer");
    // A sighted operator reads the glyph; this is the other half of the same
    // warning, and without it the row is "Features" and nothing more.
    expect(link.textContent).toContain("opens in the Gram dashboard");
  });

  it("renders no Features row for a record with no slug", async () => {
    mocks.getOrganization.mockResolvedValue(anOrganization({ slug: "" }));

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.id}`,
    });
    await screen.findByRole("link", { name: "All organizations" });

    // By words as well as by role. An anchor rendered with no href is the shape
    // this mistake takes, and such an anchor has no link role at all: an
    // absence asserted by role alone passes with the dead control on screen.
    expect(within(sidebar()).queryByText(/^Features/)).toBeNull();
    expect(
      within(sidebar()).queryByRole("link", { name: /^Features/ }),
    ).toBeNull();
  });
});
