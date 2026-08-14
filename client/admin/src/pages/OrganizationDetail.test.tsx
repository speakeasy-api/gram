import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AdminOrganization,
  AdminOrganizationMember,
  AdminProject,
} from "@/lib/gramAdminApi";

import { routeTree } from "@/routeTree.gen";
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

const ORG: AdminOrganization = {
  id: "org_placeholder_one",
  name: "Placeholder One",
  slug: "placeholder-one",
  account_type: "pro",
  whitelisted: true,
  // The stale pair, dated apart from the real trial on purpose. A page back on
  // `free_trial_ends_at` then shows the wrong date rather than the right one
  // by coincidence.
  free_trial_started_at: "2026-02-01T00:00:00Z",
  free_trial_ends_at: "2026-11-12T00:00:00Z",
  trial_state: "running",
  trial_ends_at: "2026-05-06T00:00:00Z",
  member_count: 1,
  created_at: "2026-01-02T00:00:00Z",
  updated_at: "2026-01-07T00:00:00Z",
};

const PROJECT: AdminProject = {
  id: "proj_placeholder_one",
  name: "Placeholder Project",
  slug: "placeholder-project",
  created_at: "2026-01-03T00:00:00Z",
  updated_at: "2026-01-04T00:00:00Z",
};

const MEMBER: AdminOrganizationMember = {
  id: "user_placeholder_one",
  email: "member@example.test",
  display_name: "Placeholder Member",
  last_login: "2026-01-05T00:00:00Z",
  created_at: "2026-01-06T00:00:00Z",
  updated_at: "2026-01-06T00:00:00Z",
};

// Both panels render a date through toLocaleString, so the expected text has to
// come out of the same formatter. Reading the field off the fixture is what the
// assertion is for: the format is not.
function longDate(iso: string): string {
  return new Date(iso).toLocaleString();
}

// The trial is a date without a clock wherever it is read, so it does not come
// out of `longDate` with the timestamps around it.
function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString();
}

// The field rows carry no role, so a test reaches one by its label and takes
// the value beside it.
function valueBeside(label: string): HTMLElement {
  const value = screen.getByText(label).nextElementSibling;
  if (!(value instanceof HTMLElement)) {
    throw new Error(`the ${label} row has no value beside it`);
  }
  return value;
}

// A Radix tab trigger selects on mousedown, not on click. The panel only
// mounts once the organization lands, so the trigger is awaited.
async function selectTab(name: string): Promise<void> {
  const tab = await screen.findByRole("tab", { name });
  fireEvent.mouseDown(tab, { button: 0, ctrlKey: false });
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
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(ORG);
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [PROJECT] });
  mocks.listOrganizationMembers.mockReset();
  mocks.listOrganizationMembers.mockResolvedValue({ members: [MEMBER] });
});

afterEach(cleanup);

describe("organization detail", () => {
  it("reads the trial exactly the way the row does", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    const trialEndsAt = ORG.trial_ends_at;
    if (!trialEndsAt) throw new Error("the record under test needs a trial");

    const trial = await screen.findByText("Trial");
    // Written out, not built from the component. The same string is asserted
    // against the list's cell, which is the only way two pages that could
    // drift apart are held together.
    expect(valueBeside("Trial").textContent).toBe(
      `Running${shortDate(trialEndsAt)}`,
    );
    expect(
      trial.parentElement?.querySelector('[data-slot="badge"]'),
    ).toBeTruthy();

    // The defaulted pair is gone from the page, not merely unread: an operator
    // who sees "Free trial ends" reads a date no organization ever earned.
    expect(screen.queryByText("Free trial started")).toBeNull();
    expect(screen.queryByText("Free trial ends")).toBeNull();
  });

  it("reads a dash for an organization that never trialled", async () => {
    mocks.getOrganization.mockResolvedValue({
      ...ORG,
      trial_state: "none",
      trial_ends_at: undefined,
    });
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByText("Trial");
    // `free_trial_ends_at` still dates this record, which is the whole reason
    // the page was moved off it.
    expect(ORG.free_trial_ends_at).toBeTruthy();
    expect(valueBeside("Trial").textContent).toBe("-");
  });

  it("renders every projects cell out of the record that produced it", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    const link = await screen.findByRole("link", { name: PROJECT.name });
    expect(link.getAttribute("href")).toBe(`/projects/${PROJECT.slug}`);

    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual(["Name", "Slug", "ID", "Created"]);
    expect(cellsOf(rowFor(link))).toEqual([
      PROJECT.name,
      PROJECT.slug,
      PROJECT.id,
      longDate(PROJECT.created_at),
    ]);
  });

  it("renders every members cell out of the record that produced it", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });
    // Radix leaves the other tab's content out of the document until it is
    // selected.
    await selectTab("Members");

    const lastLogin = MEMBER.last_login;
    if (!lastLogin) throw new Error("the member under test needs a last login");

    const emailCell = await screen.findByRole("cell", { name: MEMBER.email });
    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual(["Email", "Name", "ID", "Last login", "Joined"]);

    const row = emailCell.closest("tr");
    if (!row) throw new Error("the email cell is not inside a row");
    expect(cellsOf(row)).toEqual([
      MEMBER.email,
      MEMBER.display_name,
      MEMBER.id,
      longDate(lastLogin),
      longDate(MEMBER.created_at),
    ]);
  });
});
