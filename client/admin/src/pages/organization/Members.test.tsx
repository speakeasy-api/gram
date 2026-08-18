import { onlineManager, QueryClient } from "@tanstack/react-query";
import { cleanup, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationQuery } from "@/lib/adminQueries";
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
    ).toEqual(["Member", "Email", "Last active", "Joined"]);

    const row = emailCell.closest("tr");
    if (!row) throw new Error("the email cell is not inside a row");
    // The member cell carries the monogram and then the name it was taken
    // from. The monogram is `aria-hidden`, so the cell's accessible name is the
    // name alone, but its text is both.
    expect(cellsOf(row)).toEqual([
      `PM${MEMBER.display_name}`,
      MEMBER.email,
      shortDate(lastLogin),
      shortDate(MEMBER.created_at),
    ]);
    expect(
      screen.getByRole("cell", { name: MEMBER.display_name }),
    ).toBeTruthy();
  });

  it("falls back to the email when a member record carries no name", async () => {
    mocks.listOrganizationMembers.mockImplementation(() =>
      Promise.resolve({
        members: [aMember({ display_name: "   " })],
      }),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

    // A member with no name is still a member, and an empty first cell reads
    // as a broken row rather than as a record with a field unset.
    await screen.findByText("1 member");
    const row = screen.getAllByRole("row")[1];
    if (!row) throw new Error("the table drew no member row");
    expect(cellsOf(row)[0]).toBe(`ME${MEMBER.email}`);
  });

  it("takes a one-word name's monogram from that word", async () => {
    mocks.listOrganizationMembers.mockImplementation(() =>
      Promise.resolve({ members: [aMember({ display_name: "Ada" })] }),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

    await screen.findByRole("cell", { name: "Ada" });
    const row = screen.getAllByRole("row")[1];
    if (!row) throw new Error("the table drew no member row");
    // Two letters from the one word, not one letter and not the whole name.
    expect(cellsOf(row)[0]).toBe("ADAda");
  });

  it("takes a three-word name's monogram from its first and last word", async () => {
    mocks.listOrganizationMembers.mockImplementation(() =>
      Promise.resolve({
        members: [aMember({ display_name: "Ada Byron Lovelace" })],
      }),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

    await screen.findByRole("cell", { name: "Ada Byron Lovelace" });
    const row = screen.getAllByRole("row")[1];
    if (!row) throw new Error("the table drew no member row");
    // The surname, not the middle name. Reading the second word gives "AB",
    // which is the initials of a different person and nothing else notices.
    expect(cellsOf(row)[0]).toBe("ALAda Byron Lovelace");
  });

  it("draws the members oldest first, whatever order the endpoint sent", async () => {
    // Shuffled on the wire. `organization.members` promises no order, so a view
    // that renders what it was handed shows one organization two ways on two
    // reads.
    const middle = aMember({
      id: "user_2",
      email: "middle@example.test",
      display_name: "Middle Member",
      created_at: "2026-01-08T00:00:00Z",
    });
    const newest = aMember({
      id: "user_3",
      email: "newest@example.test",
      display_name: "Newest Member",
      created_at: "2026-02-01T00:00:00Z",
    });
    mocks.listOrganizationMembers.mockImplementation(() =>
      Promise.resolve({ members: [newest, MEMBER, middle] }),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });
    await screen.findByRole("cell", { name: MEMBER.email });

    expect(
      screen
        .getAllByRole("row")
        .slice(1)
        .map((row) => cellsOf(row)[1]),
    ).toEqual([MEMBER.email, middle.email, newest.email]);
  });

  it("counts the members above the table", async () => {
    const second = aMember({ id: "user_2", email: "second@example.test" });
    mocks.listOrganizationMembers.mockImplementation(() =>
      Promise.resolve({ members: [MEMBER, second] }),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

    expect(await screen.findByText("2 members")).toBeTruthy();
  });

  it("says one member in the singular", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

    expect(await screen.findByText("1 member")).toBeTruthy();
    expect(screen.queryByText("1 members")).toBeNull();
  });

  it("says the organization has no members rather than showing an empty table", async () => {
    mocks.listOrganizationMembers.mockImplementation(() =>
      Promise.resolve({ members: [] }),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

    expect(
      await screen.findByText("No members in this organization"),
    ).toBeTruthy();
    // No "0 members" line above it. The sentence in the table already says
    // that, and a count of nothing beside it says it twice.
    expect(screen.queryByText("0 members")).toBeNull();
  });

  it("draws no checkbox on a table that did not ask for one", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });
    await screen.findByRole("cell", { name: MEMBER.email });

    // Row selection is opt-in, and it is opted into by putting the select
    // column in a page's own column list. The shared table registers the
    // feature for every page that uses it, and a feature that drew a column of
    // its own would put one here, where nothing reads a selection.
    expect(screen.queryAllByRole("checkbox")).toEqual([]);
  });

  it("reads Never for a member who has never logged in", async () => {
    mocks.listOrganizationMembers.mockImplementation(() =>
      Promise.resolve({ members: [aMember({ last_login: undefined })] }),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

    const emailCell = await screen.findByRole("cell", { name: MEMBER.email });
    const row = emailCell.closest("tr");
    if (!row) throw new Error("the email cell is not inside a row");
    // Not the dash every other absent date draws. A member who has never
    // signed in is a fact about the account, and the dash that means "nothing
    // recorded" hides it among the fields that merely have no value.
    expect(cellsOf(row)[2]).toBe("Never");
    expect(cellsOf(row)[2]).not.toBe("-");
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
        initialPath: `/organizations/${ORG.slug}/members`,
        queryClient: qc,
      });

      expect(await screen.findByText("Loading...")).toBeTruthy();
      // An operator told an organization has no members believes a fact about
      // the customer that no read ever established.
      expect(screen.queryByText("No members in this organization")).toBeNull();
    } finally {
      onlineManager.setOnline(true);
    }
  });

  it("says the members could not be read rather than that there are none", async () => {
    mocks.listOrganizationMembers.mockRejectedValue(
      new Error("members read failed"),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

    expect(await screen.findByText("Unable to load members")).toBeTruthy();
    // The two messages sit in the same table cell, one branch apart. An
    // operator told an organization has no members after a failed read
    // believes a fact about the customer that the page never established.
    expect(screen.queryByText("No members in this organization")).toBeNull();
  });

  it("is not on the overview, so the record's views stay separate", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByText("Account type");
    expect(screen.queryByRole("cell", { name: MEMBER.email })).toBeNull();
  });
});
