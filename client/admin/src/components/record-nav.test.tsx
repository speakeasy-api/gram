import { QueryClient } from "@tanstack/react-query";
import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { RecordNav } from "@/components/record-nav";
import { SidebarProvider } from "@/components/ui/sidebar";
import { organizationProjectsQuery } from "@/lib/adminQueries";
import type {
  AdminOrganization,
  AdminProject,
  TrialState,
} from "@/lib/gramAdminApi";
import { anOrganization, aProject } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  listOrganizationProjects: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    listOrganizationProjects: mocks.listOrganizationProjects,
  };
});

const ORG = anOrganization();

// The two states that mean the record is not trialling.
const NOT_TRIALLING: (TrialState | undefined)[] = ["none", undefined];

beforeEach(() => {
  mocks.listOrganizationProjects.mockReset();
});

afterEach(cleanup);

// `projects: undefined` leaves the projects query pending forever, which is the
// state three of these tests are about. A resolved empty array is a different
// state and must not stand in for it.
//
// The resolved cases seed the cache as well as the mock, because that is what
// `useOpenOrganization` does: the record is in hand before the nav mounts.
async function mount(projects?: AdminProject[], org: AdminOrganization = ORG) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  // Answers for one organization only. A mock that answers every argument
  // alike lets a nav that queries the slug, or another record's id, pass.
  mocks.listOrganizationProjects.mockImplementation(
    (organizationID: string) => {
      if (organizationID !== org.id) {
        return Promise.reject(
          new Error(`unexpected organization ${organizationID}`),
        );
      }
      return projects
        ? Promise.resolve({ projects })
        : new Promise<never>(() => {});
    },
  );

  if (projects) {
    qc.setQueryData(organizationProjectsQuery(org.id).queryKey, { projects });
  }

  return renderWithApp(
    <SidebarProvider>
      <RecordNav idOrSlug={org.slug || org.id} org={org} />
    </SidebarProvider>,
    { queryClient: qc },
  );
}

// The count is a `SidebarMenuBadge`, a sibling of the link rather than part of
// it, so the item is what carries label and count together.
function navItem(label: string): HTMLElement {
  const link = screen.getByRole("link", { name: label });
  const item = link.closest("[data-slot='sidebar-menu-item']");
  if (!(item instanceof HTMLElement)) {
    throw new Error(`no sidebar menu item around the ${label} link`);
  }
  return item;
}

describe("RecordNav", () => {
  it("shows the member count from the record, with no query", async () => {
    await mount();

    // The projects query is still pending, so nothing this item shows can have
    // come from a request.
    expect(navItem("Members").textContent).toContain("3");
  });

  it("shows no project count while the projects query is pending", async () => {
    await mount();

    // Not a zero and not a skeleton. Nothing. A pending count that renders "0"
    // tells the operator the organization has no projects, which is a claim the
    // query has not answered yet.
    expect(navItem("Projects").textContent).not.toMatch(/\d/);
  });

  it("points Projects at the one project, with no count, when it has exactly one", async () => {
    await mount([aProject()]);

    const item = navItem("Projects");
    // The id, not the slug: project.get resolves a slug across every
    // organization, and every organization has a project slugged "default".
    expect(
      screen.getByRole("link", { name: "Projects" }).getAttribute("href"),
    ).toBe("/organizations/test-org/projects/proj_1");
    expect(item.textContent).not.toMatch(/\d/);
    // The label does not become the project's name. Relabelling would make the
    // nav shift under the operator between records.
    expect(item.textContent).toContain("Projects");
  });

  it("shows the project count once the projects query resolves", async () => {
    await mount([
      aProject(),
      aProject({ id: "proj_2", slug: "second-project" }),
    ]);

    expect(navItem("Projects").textContent).toContain("2");
  });

  it("points Projects at the list when the organization has no projects", async () => {
    await mount([]);

    const item = navItem("Projects");
    expect(
      screen.getByRole("link", { name: "Projects" }).getAttribute("href"),
    ).toBe("/organizations/test-org/projects");
    expect(item.textContent).not.toMatch(/\d/);
  });

  it("points Projects at the list when the organization has two projects", async () => {
    await mount([
      aProject(),
      aProject({ id: "proj_2", slug: "second-project" }),
    ]);

    expect(
      screen.getByRole("link", { name: "Projects" }).getAttribute("href"),
    ).toBe("/organizations/test-org/projects");
  });

  it("points Projects at the list while the projects query is still pending", async () => {
    await mount();

    expect(
      screen.getByRole("link", { name: "Projects" }).getAttribute("href"),
    ).toBe("/organizations/test-org/projects");
  });

  it("keeps the record's own address in every item, not the record's slug", async () => {
    // Reached by id. The links have to stay on the address the operator is on,
    // or every nav press moves the record to another cache entry and refetches
    // it.
    await mount([], anOrganization({ slug: "" }));

    expect(
      screen.getByRole("link", { name: "Overview" }).getAttribute("href"),
    ).toBe("/organizations/org_1");
    expect(
      screen.getByRole("link", { name: "Members" }).getAttribute("href"),
    ).toBe("/organizations/org_1/members");
  });

  it("names the account type and the trial state under the record", async () => {
    await mount([], anOrganization({ trial_state: "running" }));

    expect(screen.getByText("free · Running")).toBeTruthy();
  });

  // `undefined` beside `"none"`, because `subtitle` defaults an absent state to
  // `none` and that is the state most records are in.
  it.each(NOT_TRIALLING)(
    "names the account type alone for a trial state of %s",
    async (trial_state) => {
      await mount([], anOrganization({ trial_state }));

      // `TRIAL_LABELS.none` is the words "No trial", so the guard for a state
      // this build has never heard of never catches this one. The header draws
      // no mark for it and the list cell draws a dash.
      expect(screen.getByText("free")).toBeTruthy();
      expect(screen.queryByText(/No trial/)).toBeNull();
    },
  );

  it("says nothing about a trial state this build has no words for", async () => {
    await mount([], anOrganization({ trial_state: "resurrected" as never }));

    // Reading an unknown state as "No trial" is the laundering `Trial` exists
    // to stop, put back in the sidebar.
    expect(screen.getByText("free")).toBeTruthy();
    expect(screen.queryByText(/No trial/)).toBeNull();
  });

  it("says nothing for a trial state named after a property of Object", async () => {
    await mount([], anOrganization({ trial_state: "toString" as never }));

    // The words are looked up in a plain record, so an inherited property
    // answers for a state nobody wrote down. This is the case `Object.hasOwn`
    // is there for, and the only one it is needed for: without it the subtitle
    // reads out the source of `Object.prototype.toString`.
    expect(screen.getByText("free")).toBeTruthy();
  });

  it("leads back to the list of organizations", async () => {
    await mount([]);

    expect(
      screen
        .getByRole("link", { name: "All organizations" })
        .getAttribute("href"),
    ).toBe("/organizations");
  });
});
