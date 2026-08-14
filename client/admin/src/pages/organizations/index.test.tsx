import type { AnyRouter } from "@tanstack/react-router";
import {
  useTable,
  type ColumnVisibilityState,
  type Updater,
} from "@tanstack/react-table";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
  type Mock,
} from "vitest";

import type {
  AdminOrganization,
  ListOrganizationsParams,
  ListOrganizationsResult,
} from "@/lib/gramAdminApi";
import { useState, type JSX } from "react";

import { routeTree } from "@/routeTree.gen";
import {
  organizationsSearchSchema,
  type OrganizationsSearch,
} from "@/routes/organizations.index";
import { dataTableFeatures } from "@/components/data-table";
import { renderRouteTree } from "@/test/harness";

import { ORG_COLUMNS } from "./columns";
import { TableActionBar } from "./Toolbar";

const mocks = vi.hoisted(() => ({
  listOrganizations:
    vi.fn<
      (params?: ListOrganizationsParams) => Promise<ListOrganizationsResult>
    >(),
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listOrganizationMembers: vi.fn(),
}));

// Only the endpoints this page's route tree reaches are replaced. The rest of
// the module stays real, so toSearchParams and omitUnset still decide what
// counts as an unset param. The three detail endpoints are here because a row
// click leaves this page for the detail route.
vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    listOrganizations: mocks.listOrganizations,
    getSession: mocks.getSession,
    getOrganization: mocks.getOrganization,
    listOrganizationProjects: mocks.listOrganizationProjects,
    listOrganizationMembers: mocks.listOrganizationMembers,
  };
});

// Two rows, and every optional field set on one of them. One row forecloses
// every ordering and keying fault by construction, and an unset optional field
// renders the same dash whichever field the cell reads.
const ORGS: AdminOrganization[] = [
  {
    id: "org_placeholder_one",
    name: "Placeholder One",
    slug: "placeholder-one",
    account_type: "pro",
    workos_id: "workosplaceholderone",
    whitelisted: true,
    disabled_at: "2026-03-04T00:00:00Z",
    free_trial_started_at: "2026-02-01T00:00:00Z",
    free_trial_ends_at: "2026-05-06T00:00:00Z",
    member_count: 3,
    created_at: "2026-01-02T00:00:00Z",
    updated_at: "2026-01-07T00:00:00Z",
  },
  {
    id: "org_placeholder_two",
    name: "Placeholder Two",
    slug: "placeholder-two",
    account_type: "free",
    whitelisted: false,
    member_count: 7,
    created_at: "2026-06-08T00:00:00Z",
    updated_at: "2026-06-09T00:00:00Z",
  },
];

// A page the cursor leads to. Nothing it holds appears on the first page, so a
// row that survives the page change is a reused node rather than a match.
const NEXT_PAGE_ORG: AdminOrganization = {
  id: "org_placeholder_three",
  name: "Placeholder Three",
  slug: "placeholder-three",
  account_type: "enterprise",
  whitelisted: false,
  member_count: 11,
  created_at: "2026-07-10T00:00:00Z",
  updated_at: "2026-07-11T00:00:00Z",
};

const [FIRST_ORG, SECOND_ORG] = ORGS;
if (!FIRST_ORG || !SECOND_ORG) throw new Error("ORGS needs two rows");

// The columns render a date through toLocaleDateString, so the expected text
// has to come out of the same formatter. Reading the field off the fixture is
// what the assertion is for: the format is not.
function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString();
}

function lastListParams(): ListOrganizationsParams {
  const call = mocks.listOrganizations.mock.calls.at(-1);
  return call?.[0] ?? {};
}

// Written out rather than imported from the toolbar. Reading the constant under
// test would let a shortened debounce move these assertions along with it.
const DEBOUNCE_MS = 300;

// `waitFor` polls on a timer, so it never settles while the clock is faked.
// Advance the clock instead, and let `act` flush the render and the navigation
// each tick releases.
async function withFakeTimers(
  body: (tick: (ms: number) => Promise<void>) => Promise<void>,
): Promise<void> {
  vi.useFakeTimers();
  try {
    await body(async (ms) => {
      await act(async () => {
        vi.advanceTimersByTime(ms);
      });
    });
  } finally {
    vi.useRealTimers();
  }
}

// The router JSON-encodes and then percent-encodes an array, so decoding once
// lets an assertion name the value the way the schema declares it.
function currentSearch(router: AnyRouter): string {
  return decodeURIComponent(router.state.location.searchStr);
}

// A Radix menu and a Radix select both open on pointerdown, not on click.
function openOn(trigger: HTMLElement): void {
  fireEvent.pointerDown(trigger, {
    button: 0,
    ctrlKey: false,
    pointerType: "mouse",
  });
}

// Reached through the name link, so a test names the row the way an operator
// would rather than by position.
function rowFor(link: HTMLElement): HTMLTableRowElement {
  const row = link.closest("tr");
  if (!row) throw new Error("the name link is not inside a row");
  return row;
}

// Found by the accessible name a screen reader announces, so the test reaches
// the control the way an operator does. Waiting on it is also waiting for the
// rows, and it is present whether or not the name column is visible.
function peekTrigger(name: string): Promise<HTMLElement> {
  return screen.findByRole("button", { name: `Peek at ${name}` });
}

async function peekOn(name: string): Promise<HTMLElement> {
  const trigger = await peekTrigger(name);
  fireEvent.click(trigger);
  return trigger;
}

function peekPanel(): HTMLElement {
  return screen.getByRole("complementary", { name: "Organization peek" });
}

function isPeeked(link: HTMLElement): boolean {
  return rowFor(link).classList.contains("bg-muted");
}

function urlFor(search: Record<string, unknown>): string {
  // The router JSON-encodes a non-string value, so a bookmarked URL is built
  // the same way here rather than hand-written.
  const qs = new URLSearchParams();
  for (const [key, value] of Object.entries(search)) {
    qs.set(key, typeof value === "string" ? value : JSON.stringify(value));
  }
  return `/organizations?${qs.toString()}`;
}

beforeEach(() => {
  mocks.listOrganizations.mockReset();
  mocks.listOrganizations.mockResolvedValue({ organizations: ORGS });
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(FIRST_ORG);
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
  mocks.listOrganizationMembers.mockReset();
  mocks.listOrganizationMembers.mockResolvedValue({ members: [] });
});

afterEach(cleanup);

describe("organizations list", () => {
  it("takes the search term from the URL and sends it with the request", async () => {
    await renderRouteTree(routeTree, {
      initialPath: "/organizations?q=acme",
    });

    const input = screen.getByLabelText("Search organizations");
    expect((input as HTMLInputElement).value).toBe("acme");

    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });
    expect(lastListParams().q).toBe("acme");
  });

  it("holds a keystroke out of the URL until the debounce elapses", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });
    const input = screen.getByLabelText("Search organizations");

    let navigations = 0;
    router.subscribe("onResolved", () => {
      navigations += 1;
    });

    await withFakeTimers(async (tick) => {
      fireEvent.change(input, { target: { value: "acme" } });

      await tick(DEBOUNCE_MS - 1);
      expect(router.state.location.searchStr).toBe("");
      expect(navigations).toBe(0);

      await tick(1);
      expect(router.state.location.searchStr).toContain("q=acme");

      // A settled term must not re-arm the timer.
      await tick(DEBOUNCE_MS * 2);
    });

    expect(navigations).toBe(1);
  });

  it("commits a burst of typing as one history entry", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });
    const before = router.history.length;
    const input = screen.getByLabelText("Search organizations");

    await withFakeTimers(async (tick) => {
      for (const value of ["a", "ac", "acm", "acme"]) {
        fireEvent.change(input, { target: { value } });
        // Each keystroke has to restart the timer. Without a gap between them
        // the four changes coalesce through the effect cleanup alone, and the
        // debounce this test is named for is never exercised.
        await tick(DEBOUNCE_MS - 1);
        expect(router.state.location.searchStr).toBe("");
      }

      await tick(DEBOUNCE_MS);
      expect(router.state.location.searchStr).toContain("q=acme");
    });

    expect(router.history.length).toBe(before);
  });

  it("keeps a space the operator typed at the end of the term", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });
    const input = screen.getByLabelText("Search organizations");

    fireEvent.change(input, { target: { value: "acme " } });

    await waitFor(
      () => {
        expect(router.state.location.searchStr).toContain("q=acme");
      },
      { timeout: 2000 },
    );
    expect((input as HTMLInputElement).value).toBe("acme ");
  });

  it("follows the search term back when the operator goes back", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations?q=acme",
    });
    const input = screen.getByLabelText("Search organizations");

    await router.navigate({ to: "/organizations", search: { q: "widget" } });
    expect((input as HTMLInputElement).value).toBe("widget");

    router.history.back();

    await waitFor(() => {
      expect((input as HTMLInputElement).value).toBe("acme");
    });
  });

  it("hides a column the Columns control unchecks", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    expect(screen.getByRole("columnheader", { name: "Slug" })).toBeTruthy();

    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Slug" }));

    // The open menu hides the rest of the page from the accessibility tree, so
    // wait for a column that stays before reading the one that went.
    await waitFor(() => {
      expect(screen.getByRole("columnheader", { name: "Name" })).toBeTruthy();
    });
    expect(screen.queryByRole("columnheader", { name: "Slug" })).toBeNull();

    // Every row has to drop the same cell the header dropped, or every cell
    // after it slides one column to the left.
    expect(screen.getAllByRole("cell").length).toBe(
      screen.getAllByRole("columnheader").length * ORGS.length,
    );

    // Reopening reads the menu against the page's own state. Without this the
    // bar could take a constant and its guard would never fire.
    openOn(screen.getByRole("button", { name: "Columns" }));
    expect(
      screen
        .getByRole("menuitemcheckbox", { name: "Slug" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(
      screen
        .getByRole("menuitemcheckbox", { name: "Name" })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("shows the filters a reloaded URL carries", async () => {
    await renderRouteTree(routeTree, {
      initialPath: urlFor({ type: "pro", disabled: true }),
    });

    expect(screen.getByLabelText("Account type").textContent).toContain("pro");
    expect(screen.getByRole("button", { name: "Hide disabled" })).toBeTruthy();

    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });
    expect(lastListParams().account_type).toBe("pro");
    expect(lastListParams().include_disabled).toBe(true);
  });

  it("writes the filter controls back to the URL", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    openOn(screen.getByLabelText("Account type"));
    fireEvent.click(await screen.findByRole("option", { name: "enterprise" }));

    await waitFor(() => {
      expect(currentSearch(router)).toContain("type=enterprise");
    });

    fireEvent.click(screen.getByRole("button", { name: "Show disabled" }));

    await waitFor(() => {
      expect(currentSearch(router)).toContain("disabled=true");
    });
    expect(currentSearch(router)).toContain("type=enterprise");
  });

  it("clears the account type filter back to every type", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: urlFor({ type: "pro" }),
    });

    await waitFor(() => {
      expect(lastListParams().account_type).toBe("pro");
    });

    openOn(screen.getByLabelText("Account type"));
    fireEvent.click(await screen.findByRole("option", { name: "All types" }));

    // "All types" is the only route back to an unfiltered list.
    await waitFor(() => {
      expect(lastListParams().account_type).toBeUndefined();
    });
    expect(currentSearch(router)).not.toContain("type=");
  });

  it("sends a pasted term without the whitespace around it", async () => {
    await renderRouteTree(routeTree, {
      initialPath: urlFor({ q: "acme " }),
    });

    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });
    // Same request, and so the same cache entry, as `?q=acme`.
    expect(lastListParams().q).toBe("acme");
  });

  it("drops the cursor when a filter changes", async () => {
    mocks.listOrganizations.mockResolvedValue({
      organizations: ORGS,
      next_cursor: "cursor_page_two",
    });
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    const next = await screen.findByRole("button", { name: "Next" });
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });
    fireEvent.click(next);
    await waitFor(() => {
      expect(lastListParams().cursor).toBe("cursor_page_two");
    });

    fireEvent.click(screen.getByRole("button", { name: "Show disabled" }));

    await waitFor(() => {
      expect(lastListParams().include_disabled).toBe(true);
    });
    // The cursor was minted by the previous filter set and points into a
    // different result set.
    expect(lastListParams().cursor).toBeUndefined();
  });

  it("renders every cell of a row out of the record that produced it", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    const { workos_id: workosID, disabled_at: disabledAt } = FIRST_ORG;
    const trialEndsAt = FIRST_ORG.free_trial_ends_at;
    if (!workosID || !disabledAt || !trialEndsAt) {
      throw new Error("the row under test needs its optional fields set");
    }

    const link = await screen.findByRole("link", { name: FIRST_ORG.name });
    // The Name cell puts the name in the text and the slug in the href, so a
    // cell that reads the wrong field cannot satisfy both.
    expect(link.getAttribute("href")).toBe(`/organizations/${FIRST_ORG.slug}`);

    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual([
      "Peek",
      "Name",
      "Slug",
      "Type",
      "Members",
      "WorkOS",
      "Disabled",
      "Trial ends",
      "Created",
    ]);
    expect(
      within(rowFor(link))
        .getAllByRole("cell")
        .map((cell) => cell.textContent),
    ).toEqual([
      // The peek control carries an icon and its name is on the button.
      "",
      FIRST_ORG.name,
      FIRST_ORG.slug,
      FIRST_ORG.account_type,
      String(FIRST_ORG.member_count),
      // The truncation length is written out rather than imported. Reading the
      // column's own constant would move this expectation along with it.
      `${workosID.substring(0, 12)}...`,
      shortDate(disabledAt),
      shortDate(trialEndsAt),
      shortDate(FIRST_ORG.created_at),
    ]);
  });

  it("opens the organization when the operator clicks the row body", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    const link = await screen.findByRole("link", { name: FIRST_ORG.name });
    // Past the peek control and the name link, so the click lands on the row
    // body rather than on something that handles it first.
    const [, , slugCell] = within(rowFor(link)).getAllByRole("cell");
    if (!slugCell) throw new Error("the row needs a third cell");

    fireEvent.click(slugCell);

    // The slug, not the id: the handler takes the record, and a handler handed
    // the row wrapper instead would reach the id and seed the detail page's
    // cache with a table object.
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(
        `/organizations/${FIRST_ORG.slug}`,
      );
    });
  });

  it("leaves the current tab in place when the operator command-clicks a name", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    const link = await screen.findByRole("link", { name: FIRST_ORG.name });
    fireEvent.click(link, { button: 0, metaKey: true });

    // The browser opens the link in a background tab and the row handler has to
    // stay out of it. Without the guard the list the operator meant to keep
    // navigates away underneath the new tab.
    await act(async () => {
      await new Promise<void>((resolve) => {
        setTimeout(resolve, 0);
      });
    });
    expect(router.state.location.pathname).toBe("/organizations");
  });

  it("drops focus rather than moving it to another organization on the next page", async () => {
    mocks.listOrganizations.mockImplementation((params) =>
      Promise.resolve(
        params?.cursor
          ? { organizations: [NEXT_PAGE_ORG] }
          : { organizations: ORGS, next_cursor: "cursor_page_two" },
      ),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    const link = await screen.findByRole("link", { name: FIRST_ORG.name });
    link.focus();
    expect(document.activeElement).toBe(link);

    const next = await screen.findByRole("button", { name: "Next" });
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });
    fireEvent.click(next);
    await screen.findByRole("link", { name: NEXT_PAGE_ORG.name });

    // Keyed by index, React reuses this node for the next page's record and
    // relabels it under the operator's focus. No focus event fires, so the next
    // Enter opens an organization they never chose.
    expect(link.isConnected).toBe(false);
    expect(document.activeElement).toBe(document.body);
  });
});

describe("organizations list peek", () => {
  it("docks a panel beside the list rather than leaving the page", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });
    const trigger = await peekOn(FIRST_ORG.name);

    expect(
      within(peekPanel()).getByRole("heading", { name: FIRST_ORG.name }),
    ).toBeTruthy();
    // The row's click handler navigates, and the control sits inside the row.
    // A control that let the click through would leave the list entirely.
    expect(router.state.location.pathname).toBe("/organizations");

    // happy-dom synthesises no click from Enter or Space, so the element type
    // is what proves the keyboard can operate this control at all.
    expect(trigger.tagName).toBe("BUTTON");

    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual(["Peek", "Name", "Slug", "Type"]);
  });

  it("highlights the row it is peeking at and no other", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const link = await screen.findByRole("link", { name: FIRST_ORG.name });

    await peekOn(FIRST_ORG.name);

    expect(isPeeked(link)).toBe(true);
    expect(isPeeked(screen.getByRole("link", { name: SECOND_ORG.name }))).toBe(
      false,
    );
  });

  it("walks peek down the list and stops at the last row", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const first = await screen.findByRole("link", { name: FIRST_ORG.name });
    await peekOn(FIRST_ORG.name);

    fireEvent.keyDown(peekPanel(), { key: "ArrowDown" });

    expect(
      within(peekPanel()).getByRole("heading", { name: SECOND_ORG.name }),
    ).toBeTruthy();
    // Nothing else speaks when the panel already holds the focus and the
    // record under it changes.
    expect(screen.getByRole("status").textContent).toBe(
      `Peeking at ${SECOND_ORG.name}.`,
    );
    expect(isPeeked(screen.getByRole("link", { name: SECOND_ORG.name }))).toBe(
      true,
    );
    expect(isPeeked(first)).toBe(false);

    fireEvent.keyDown(peekPanel(), { key: "ArrowDown" });
    expect(
      within(peekPanel()).getByRole("heading", { name: SECOND_ORG.name }),
    ).toBeTruthy();
  });

  it("walks peek back up the list and stops at the first row", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(SECOND_ORG.name);

    fireEvent.keyDown(peekPanel(), { key: "ArrowUp" });
    expect(
      within(peekPanel()).getByRole("heading", { name: FIRST_ORG.name }),
    ).toBeTruthy();

    fireEvent.keyDown(peekPanel(), { key: "ArrowUp" });
    expect(
      within(peekPanel()).getByRole("heading", { name: FIRST_ORG.name }),
    ).toBeTruthy();
  });

  it("scrolls the row peek moves to into view", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);

    const second = rowFor(screen.getByRole("link", { name: SECOND_ORG.name }));
    const scrollIntoView = vi.spyOn(second, "scrollIntoView");

    fireEvent.keyDown(peekPanel(), { key: "ArrowDown" });

    expect(scrollIntoView).toHaveBeenCalled();
  });

  it("closes on Escape and puts the keyboard back on the control", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const trigger = await peekOn(FIRST_ORG.name);

    fireEvent.keyDown(peekPanel(), { key: "Escape" });

    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("gives the operator's own column visibility back when peek closes", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Slug" }));
    await waitFor(() => {
      expect(screen.queryByRole("columnheader", { name: "Slug" })).toBeNull();
    });

    await peekOn(FIRST_ORG.name);
    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual(["Peek", "Name", "Type"]);

    fireEvent.keyDown(peekPanel(), { key: "Escape" });

    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual([
      "Peek",
      "Name",
      "Type",
      "Members",
      "WorkOS",
      "Disabled",
      "Trial ends",
      "Created",
    ]);
  });

  it("shows the name column while it is open, even when the operator hid it", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Name" }));
    await waitFor(() => {
      expect(screen.getByRole("columnheader", { name: "Slug" })).toBeTruthy();
    });
    expect(screen.queryByRole("columnheader", { name: "Name" })).toBeNull();

    // The name link is gone with the column, and the control is not: it names
    // the organization itself, so it is still reachable by that name.
    await peekOn(FIRST_ORG.name);

    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual(["Peek", "Name", "Slug", "Type"]);
    expect(screen.getByRole("link", { name: FIRST_ORG.name })).toBeTruthy();

    fireEvent.keyDown(peekPanel(), { key: "Escape" });

    expect(screen.queryByRole("columnheader", { name: "Name" })).toBeNull();
  });

  it("leaves the arrow keys to the Columns menu while it is open", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const first = await screen.findByRole("link", { name: FIRST_ORG.name });
    const second = screen.getByRole("link", { name: SECOND_ORG.name });
    await peekOn(FIRST_ORG.name);

    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.keyDown(screen.getByRole("menuitemcheckbox", { name: "Name" }), {
      key: "ArrowDown",
    });

    expect(isPeeked(first)).toBe(true);
    expect(isPeeked(second)).toBe(false);
  });

  it("leaves Escape to the Columns menu while it is open", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const first = await screen.findByRole("link", { name: FIRST_ORG.name });
    await peekOn(FIRST_ORG.name);

    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.keyDown(screen.getByRole("menuitemcheckbox", { name: "Name" }), {
      key: "Escape",
    });

    expect(isPeeked(first)).toBe(true);
  });

  it("drops peek when the operator pages away from the record", async () => {
    mocks.listOrganizations.mockImplementation((params) =>
      Promise.resolve(
        params?.cursor
          ? { organizations: [NEXT_PAGE_ORG] }
          : { organizations: ORGS, next_cursor: "cursor_page_two" },
      ),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await peekOn(FIRST_ORG.name);
    expect(peekPanel()).toBeTruthy();

    const next = await screen.findByRole("button", { name: "Next" });
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });
    fireEvent.click(next);
    await screen.findByRole("link", { name: NEXT_PAGE_ORG.name });

    await waitFor(() => {
      expect(
        screen.queryByRole("complementary", { name: "Organization peek" }),
      ).toBeNull();
    });
    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual([
      "Peek",
      "Name",
      "Slug",
      "Type",
      "Members",
      "WorkOS",
      "Disabled",
      "Trial ends",
      "Created",
    ]);
  });

  it("activating the control twice closes the panel again", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    const trigger = await peekOn(FIRST_ORG.name);
    expect(peekPanel()).toBeTruthy();

    fireEvent.click(trigger);

    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
    expect(screen.getByRole("status").textContent).toBe("Peek closed.");
  });

  it("activating another row's control moves the peek to that row", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await peekOn(FIRST_ORG.name);
    await peekOn(SECOND_ORG.name);

    // A toggle keyed on "some panel is open" rather than on this row would
    // close the panel here instead of moving it.
    expect(
      within(peekPanel()).getByRole("heading", { name: SECOND_ORG.name }),
    ).toBeTruthy();
    expect(screen.getByRole("status").textContent).toBe(
      `Peeking at ${SECOND_ORG.name}.`,
    );
  });

  it("the control reports whether its own panel is open", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    const trigger = await peekOn(FIRST_ORG.name);
    const other = await peekTrigger(SECOND_ORG.name);

    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(other.getAttribute("aria-expanded")).toBe("false");

    // Set only while the panel exists: aria-controls pointing at an absent id
    // is invalid, so the row that is not peeked carries none.
    expect(trigger.getAttribute("aria-controls")).toBe(peekPanel().id);
    expect(other.hasAttribute("aria-controls")).toBe(false);
  });

  it("closing puts the keyboard back on the control that opened it", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    const trigger = await peekOn(FIRST_ORG.name);
    // The panel takes the focus on mount, which is what puts Escape and the
    // arrow keys within reach of the handler above the table.
    expect(document.activeElement).toBe(peekPanel());

    fireEvent.click(
      within(peekPanel()).getByRole("button", { name: "Close peek" }),
    );

    expect(document.activeElement).toBe(trigger);
  });

  it("keeps an empty status region on the page before anything is peeked", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    // A live region injected together with its text is not reliably announced,
    // so the region has to be on the page before it has anything to say.
    expect(screen.getByRole("status").textContent).toBe("");
  });

  it("alt-clicking a row opens the organization rather than peeking at it", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    const link = await screen.findByRole("link", { name: FIRST_ORG.name });
    const [, , slugCell] = within(rowFor(link)).getAllByRole("cell");
    if (!slugCell) throw new Error("the row needs a third cell");

    // The gesture is gone: browsers read Alt-click on an anchor as "save
    // link", and the row's own handler carries no branch for it any more.
    fireEvent.click(slugCell, { altKey: true });

    await waitFor(() => {
      expect(router.state.location.pathname).toBe(
        `/organizations/${FIRST_ORG.slug}`,
      );
    });
    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
  });

  it("will not let the Columns menu hide the control", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    openOn(screen.getByRole("button", { name: "Columns" }));
    const item = screen.getByRole("menuitemcheckbox", { name: "Peek" });
    fireEvent.click(item);

    expect(item.getAttribute("aria-disabled")).toBe("true");
    expect(item.getAttribute("aria-checked")).toBe("true");

    // A locked item leaves the menu open, and an open Radix menu hides the
    // rest of the page from the accessibility tree.
    fireEvent.keyDown(item, { key: "Escape" });

    await waitFor(() => {
      expect(screen.getByRole("columnheader", { name: "Peek" })).toBeTruthy();
    });
    expect(await peekTrigger(FIRST_ORG.name)).toBeTruthy();
  });

  it("puts the keyboard on the table, not on the body, when the peeked record leaves the list", async () => {
    mocks.listOrganizations.mockImplementation((params) =>
      Promise.resolve(
        params?.cursor
          ? { organizations: [NEXT_PAGE_ORG] }
          : { organizations: ORGS, next_cursor: "cursor_page_two" },
      ),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await peekOn(FIRST_ORG.name);
    // The panel holds the focus, and it is about to unmount under it.
    expect(document.activeElement).toBe(peekPanel());

    const next = await screen.findByRole("button", { name: "Next" });
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });
    fireEvent.click(next);
    await screen.findByRole("link", { name: NEXT_PAGE_ORG.name });

    // The scroll box around the table, reached through the table it holds
    // rather than by class: it is the nearest thing to where the operator was.
    await waitFor(() => {
      expect(document.activeElement).not.toBe(document.body);
    });
    expect(document.activeElement?.contains(screen.getByRole("table"))).toBe(
      true,
    );
    // Worded apart from an operator's own close, which says nothing about why.
    expect(screen.getByRole("status").textContent).toBe(
      `Peek closed. ${FIRST_ORG.name} is no longer in the list.`,
    );
  });

  it("leaves the keyboard on the pager when the operator pages the peeked record away", async () => {
    mocks.listOrganizations.mockImplementation((params) =>
      Promise.resolve(
        params?.cursor
          ? { organizations: [NEXT_PAGE_ORG], next_cursor: "cursor_page_three" }
          : { organizations: ORGS, next_cursor: "cursor_page_two" },
      ),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await peekOn(FIRST_ORG.name);

    const next = await screen.findByRole("button", { name: "Next" });
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });
    // The operator drove this from the pager, so their focus is already on a
    // live control. An unguarded rescue would take it off them.
    next.focus();
    fireEvent.click(next);
    await screen.findByRole("link", { name: NEXT_PAGE_ORG.name });

    expect(document.activeElement).toBe(next);
  });
});

// The bar takes a table, so the last-column case is reachable here by handing
// it a two column table rather than by clicking seven items shut through a
// menu that closes each time.
describe("TableActionBar", () => {
  // Sliced, so the array carries the element type useTable asks for. Past the
  // peek column, which opts out of hiding: the cases below are about the two
  // rules the bar itself applies.
  const MENU_COLUMNS = ORG_COLUMNS.slice(1, 3);

  // Destructured off the tuple rather than off the slice, which widens each
  // element back to a bare column definition.
  const [, FIRST, SECOND] = ORG_COLUMNS;
  if (!FIRST || !SECOND) throw new Error("ORG_COLUMNS needs three columns");

  // An accessor column takes its id from its key unless it names one, and that
  // id is what the visibility state is keyed by.
  const SECOND_ID = String(SECOND.id ?? SECOND.accessorKey);

  function ColumnsMenu({
    initialVisibility,
    onVisibilityChange,
    columns,
  }: {
    initialVisibility: ColumnVisibilityState;
    onVisibilityChange: (updater: Updater<ColumnVisibilityState>) => void;
    columns: typeof MENU_COLUMNS;
  }): JSX.Element {
    const [columnVisibility, setColumnVisibility] = useState(initialVisibility);
    const table = useTable({
      features: dataTableFeatures,
      columns,
      data: ORGS,
      getRowId: (org) => org.id,
      state: { columnVisibility },
      // The spy sits in front of the state, so a blocked toggle is a call that
      // never happened rather than a state that happened to settle back.
      onColumnVisibilityChange: (updater) => {
        onVisibilityChange(updater);
        setColumnVisibility(updater);
      },
    });
    return <TableActionBar table={table} />;
  }

  function openColumnsMenu(
    initialVisibility: ColumnVisibilityState,
    columns: typeof MENU_COLUMNS = MENU_COLUMNS,
  ): Mock<(updater: Updater<ColumnVisibilityState>) => void> {
    const onVisibilityChange =
      vi.fn<(updater: Updater<ColumnVisibilityState>) => void>();
    render(
      <ColumnsMenu
        initialVisibility={initialVisibility}
        onVisibilityChange={onVisibilityChange}
        columns={columns}
      />,
    );
    openOn(screen.getByRole("button", { name: "Columns" }));
    return onVisibilityChange;
  }

  // A toggle that lands closes the menu. Reopening also reads the item back
  // against the state that settled, not against the click that asked for it.
  function reopenColumnsMenu(): void {
    openOn(screen.getByRole("button", { name: "Columns" }));
  }

  // Found by the name an operator reads, so the query goes through the same
  // accessible name a screen reader would announce. A header is a node in
  // general, and only a string carries a name this query can match.
  function itemFor(header: unknown): HTMLElement {
    if (typeof header !== "string") {
      throw new Error("the column under test needs a text header");
    }
    return screen.getByRole("menuitemcheckbox", { name: header });
  }

  it("stops the operator hiding the last visible column", () => {
    const onVisibilityChange = openColumnsMenu({ [SECOND_ID]: false });

    const item = itemFor(FIRST.header);
    fireEvent.click(item);
    expect(onVisibilityChange).not.toHaveBeenCalled();

    // Marked rather than disabled. Radix drops a disabled item out of the
    // menu's roving focus, and a screen reader then never reaches it.
    expect(item.getAttribute("aria-disabled")).toBe("true");
    expect(item.hasAttribute("data-disabled")).toBe(false);

    // The visual half of the same guard. Radix dims a `disabled` item for us
    // and this one is not disabled, so a sighted operator otherwise reads a
    // locked item as live and gets no answer when clicking it does nothing.
    // A token, not a substring: the stock item already carries
    // `data-[disabled]:opacity-50`.
    expect(item.classList.contains("opacity-50")).toBe(true);
    expect(itemFor(SECOND.header).classList.contains("opacity-50")).toBe(false);

    // The other half of the guard. Radix dismisses the menu on select unless
    // the default is prevented, and a menu that closes on a locked item reads
    // as if the click had worked.
    expect(itemFor(FIRST.header)).toBe(item);
  });

  it("still unhides a column while only one is visible", () => {
    const onVisibilityChange = openColumnsMenu({ [SECOND_ID]: false });

    const item = itemFor(SECOND.header);
    expect(item.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(item);
    expect(onVisibilityChange).toHaveBeenCalled();

    // The lock holds the last visible column, not the whole menu. Locking the
    // menu would trap the operator in the state the lock exists to prevent.
    reopenColumnsMenu();
    expect(itemFor(SECOND.header).getAttribute("aria-checked")).toBe("true");
  });

  it("hides a column while a second one is still visible", () => {
    const onVisibilityChange = openColumnsMenu({});

    fireEvent.click(itemFor(FIRST.header));
    expect(onVisibilityChange).toHaveBeenCalled();

    reopenColumnsMenu();
    expect(itemFor(FIRST.header).getAttribute("aria-checked")).toBe("false");
  });

  it("locks a column that opts out of hiding", () => {
    // Both are visible, so the last-column rule is not what holds this one.
    const onVisibilityChange = openColumnsMenu({}, [
      { ...FIRST, enableHiding: false },
      SECOND,
    ]);

    const item = itemFor(FIRST.header);
    fireEvent.click(item);
    expect(onVisibilityChange).not.toHaveBeenCalled();
    expect(item.getAttribute("aria-disabled")).toBe("true");

    // The opt-out is per column, so the menu still works around it.
    fireEvent.click(itemFor(SECOND.header));
    expect(onVisibilityChange).toHaveBeenCalled();
  });
});

// A param the schema drops is absent from the parsed search, so every expected
// value below is the whole object the route sees.
describe("organizationsSearchSchema", () => {
  const cases: [string, Record<string, unknown>, OrganizationsSearch][] = [
    ["reads a hand-written type", { type: "free" }, { type: "free" }],
    ["drops a type the API does not accept", { type: "startup" }, {}],
    [
      "drops a list, which the request cannot honour",
      { type: ["pro", "enterprise"] },
      {},
    ],
    ["reads an all-digit term the router coerced", { q: 123 }, { q: "123" }],
    ["reads a boolean term the router coerced", { q: true }, { q: "true" }],
    ["reads a null term the router coerced", { q: null }, { q: "null" }],
    ["drops a term that is a list, not a word", { q: ["acme"] }, {}],
    ["drops a term that is only whitespace", { q: "   " }, {}],
    ["trims the term a pasted link carries", { q: "  acme  " }, { q: "acme" }],
    ["reads a direction in the union", { dir: "desc" }, { dir: "desc" }],
    ["drops a direction outside the union", { dir: "sideways" }, {}],
    ["keeps the disabled flag", { disabled: true }, { disabled: true }],
    ["drops the disabled flag when it is off", { disabled: false }, {}],
    ["drops page 1, which is the default", { page: 1 }, {}],
    ["drops a page below 1", { page: 0 }, {}],
    ["drops a page between two whole ones", { page: 2.5 }, {}],
    ["keeps a page past the first", { page: 2 }, { page: 2 }],
  ];

  it.each(cases)("%s", (_name, search, expected) => {
    expect(organizationsSearchSchema(search)).toEqual(expected);
  });
});
