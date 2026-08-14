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
