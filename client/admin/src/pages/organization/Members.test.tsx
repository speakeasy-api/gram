import { cleanup, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { routeTree } from "@/routeTree.gen";
import { aMember, anOrganization } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationMembers: vi.fn(),
  listOrganizationProjects: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getSession: mocks.getSession,
    getOrganization: mocks.getOrganization,
    listOrganizationMembers: mocks.listOrganizationMembers,
    listOrganizationProjects: mocks.listOrganizationProjects,
  };
});

const ORG = anOrganization();
const MEMBER = aMember();

function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { timeZone: "UTC" });
}

function cellsOf(row: HTMLElement): (string | null)[] {
  return within(row)
    .getAllByRole("cell")
    .map((cell) => cell.textContent);
}

beforeEach(() => {
  // Every date below is midnight UTC, which is the previous day in this zone.
  // CI runs in UTC, where a cell that reads the operator's own zone renders the
  // same string as one that reads the server's and the assertions below prove
  // nothing. See utils.test.ts.
  vi.stubEnv("TZ", "America/Los_Angeles");
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(ORG);
  // The endpoint answers the record it was asked about and nothing else. A mock
  // that answers every argument alike cannot tell a view that reads the record
  // it is on from one that reads whatever is next to it.
  mocks.listOrganizationMembers.mockReset();
  mocks.listOrganizationMembers.mockImplementation((organizationID: string) =>
    organizationID === ORG.id
      ? Promise.resolve({ members: [MEMBER] })
      : Promise.reject(new Error(`no organization ${organizationID}`)),
  );
  // Not this view's query: the record nav in the sidebar asks for it on every
  // view. Unmocked it reaches the real fetch and the suite waits on a socket.
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
});

afterEach(() => {
  cleanup();
  vi.unstubAllEnvs();
});

describe("Members", () => {
  it("renders every members cell out of the record that produced it", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

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
      shortDate(lastLogin),
      shortDate(MEMBER.created_at),
    ]);
  });

  it("reads a dash for a member who has never logged in", async () => {
    mocks.listOrganizationMembers.mockImplementation(() =>
      Promise.resolve({ members: [aMember({ last_login: undefined })] }),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

    const emailCell = await screen.findByRole("cell", { name: MEMBER.email });
    const row = emailCell.closest("tr");
    if (!row) throw new Error("the email cell is not inside a row");
    expect(cellsOf(row)[3]).toBe("-");
  });

  it("is not on the overview, so the record's views stay separate", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByText("Account type");
    expect(screen.queryByRole("cell", { name: MEMBER.email })).toBeNull();
  });
});
