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
//
// Both rows carry the stale `free_trial_*` pair, and neither carries a date
// there that the Trial cell should ever show. The second row is the one that
// matters: it never trialled, and the stale pair still dates it, which is the
// whole reason this column was rewritten.
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
    free_trial_ends_at: "2026-11-12T00:00:00Z",
    trial_state: "running",
    trial_ends_at: "2026-05-06T00:00:00Z",
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
    free_trial_started_at: "2026-06-08T00:00:00Z",
    free_trial_ends_at: "2026-06-22T00:00:00Z",
    trial_state: "none",
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
// what the assertion is for: the format is not. UTC, because that is the zone
// the API states these dates in and the zone the table renders them in; see
// `utils.test.ts`.
function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { timeZone: "UTC" });
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

// Taken by the header above it, not by counting cells. A column added beside
// this one otherwise leaves the assertion reading its neighbour and still
// passing or failing for reasons that have nothing to do with what it names.
function cellUnder(row: HTMLElement, header: string): HTMLElement {
  const at = screen
    .getAllByRole("columnheader")
    .map((column) => column.textContent)
    .indexOf(header);
  if (at < 0) throw new Error(`no ${header} column on the page`);

  const cell = within(row).getAllByRole("cell").at(at);
  if (!cell) throw new Error(`the row has no cell under ${header}`);
  return cell;
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

function liveRegion(): HTMLElement {
  return screen.getByRole("status");
}

// The region carries a zero-width space on every other announcement, so that a
// sentence repeated word for word still changes the text node. Stripped here:
// these assertions are about what is announced, not about the marker that
// makes an unchanged sentence announceable.
function announcement(): string {
  return (liveRegion().textContent ?? "").replaceAll("\u200b", "");
}

function isPeeked(link: HTMLElement): boolean {
  return rowFor(link).classList.contains("bg-muted");
}

// Sequential navigation order, which is what Tab and Shift+Tab walk. Document
// order over the nodes the spec puts in that sequence, so a tabindex="-1" node
// is left out: focusable, and deliberately not a stop.
const TAB_STOPS = "a[href], button, input, select, textarea, [tabindex]";

function tabStopBefore(target: HTMLElement): HTMLElement | null {
  const stops = [...document.querySelectorAll<HTMLElement>(TAB_STOPS)].filter(
    (el) => el.tabIndex >= 0 && !el.hasAttribute("disabled"),
  );
  const at = stops.indexOf(target);
  return at > 0 ? (stops[at - 1] ?? null) : null;
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

  it("offers an id as a term the search box takes", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    const input = screen.getByLabelText("Search organizations");

    // The label names the target, not the terms, and it is pinned elsewhere
    // because a screen reader announces it. An operator holding an
    // organization id has only the placeholder to tell them it will match.
    expect((input as HTMLInputElement).placeholder).toMatch(/\bid\b/i);
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

    const {
      workos_id: workosID,
      disabled_at: disabledAt,
      trial_ends_at: trialEndsAt,
    } = FIRST_ORG;
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
      "Trial",
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
      // The state leads and the end date follows it, and the date says what it
      // is: the header reads `Trial` and no longer does. The stale pair on this
      // record carries a different date, so a cell back on the old field fails
      // here on the date alone.
      `Running ends ${shortDate(trialEndsAt)}`,
      shortDate(FIRST_ORG.created_at),
    ]);
  });

  it("reads a dash in the trial cell of an organization that never trialled", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    // The row is dated by `free_trial_ends_at` all the same, which is the
    // defaulted column this cell was moved off.
    expect(SECOND_ORG.free_trial_ends_at).toBeTruthy();

    const link = await screen.findByRole("link", { name: SECOND_ORG.name });
    const trialCell = cellUnder(rowFor(link), "Trial");

    // The dash is what an operator reads; the words behind it are what a
    // screen reader is given in place of a hyphen it announces as nothing.
    expect(trialCell.textContent).toBe("-No trial");
    expect(trialCell.querySelector('[data-slot="badge"]')).toBeNull();
  });

  it("lets the operator hide the trial column like any other", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    expect(screen.getByRole("columnheader", { name: "Trial" })).toBeTruthy();

    openOn(screen.getByRole("button", { name: "Columns" }));
    const item = screen.getByRole("menuitemcheckbox", { name: "Trial" });
    // Nothing about this column justifies pinning it on screen. An operator
    // working a list of a hundred organizations gets to put away the columns
    // they are not reading, and the bar locks a column only to keep the table
    // from emptying out.
    expect(item.getAttribute("aria-disabled")).not.toBe("true");
    fireEvent.click(item);

    // The open menu hides the rest of the page from the accessibility tree, so
    // wait for a column that stays before reading the one that went.
    await waitFor(() => {
      expect(screen.getByRole("columnheader", { name: "Name" })).toBeTruthy();
    });
    expect(screen.queryByRole("columnheader", { name: "Trial" })).toBeNull();
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
    await peekOn(FIRST_ORG.name);

    expect(
      within(peekPanel()).getByRole("heading", { name: FIRST_ORG.name }),
    ).toBeTruthy();
    // The row's click handler navigates, and the control sits inside the row.
    // A control that let the click through would leave the list entirely.
    expect(router.state.location.pathname).toBe("/organizations");

    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual(["Peek", "Name", "Slug", "Type"]);
  });

  it("takes the trial column down with the rest while it is open", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    expect(screen.getByRole("columnheader", { name: "Trial" })).toBeTruthy();

    await peekOn(FIRST_ORG.name);

    // Its own test, because the set peek hides is keyed by column id in a
    // plain string record. Renaming the column leaves a stale key that is
    // neither a type error nor a failure anywhere else: the column simply
    // stops hiding, and the panel opens into a table too wide to read.
    expect(screen.queryByRole("columnheader", { name: "Trial" })).toBeNull();

    fireEvent.keyDown(peekPanel(), { key: "Escape" });

    expect(screen.getByRole("columnheader", { name: "Trial" })).toBeTruthy();
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
    expect(announcement()).toBe(`Peeking at ${SECOND_ORG.name}.`);
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

    // With the argument. `block: "center"` also counts as called, and it
    // re-centres the whole list under every arrow press.
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "nearest" });
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

  it("returns the keyboard to the control of whichever row was peeked", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const trigger = await peekOn(SECOND_ORG.name);

    fireEvent.keyDown(peekPanel(), { key: "Escape" });

    // The second row on purpose. A close that looks the control up across the
    // whole page instead of inside the peeked row finds the first row's one,
    // and every other focus test here peeks the first row, so it passes on
    // luck and sends an operator who peeked row 40 back to row 1.
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
      "Trial",
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
      "Trial",
      "Created",
    ]);
  });

  it("closes the panel again when the same control is activated twice", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    const trigger = await peekOn(FIRST_ORG.name);
    expect(peekPanel()).toBeTruthy();

    fireEvent.click(trigger);

    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
    expect(announcement()).toBe("Peek closed.");
  });

  it("moves the peek to another row when that row's control is activated", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await peekOn(FIRST_ORG.name);
    await peekOn(SECOND_ORG.name);

    // A toggle keyed on "some panel is open" rather than on this row would
    // close the panel here instead of moving it.
    expect(
      within(peekPanel()).getByRole("heading", { name: SECOND_ORG.name }),
    ).toBeTruthy();
    expect(announcement()).toBe(`Peeking at ${SECOND_ORG.name}.`);
  });

  it("reports on each control whether its own panel is open", async () => {
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

  it("returns the keyboard to the control that opened the panel", async () => {
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

  it("gives the keyboard a control it can operate", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const trigger = await peekTrigger(FIRST_ORG.name);

    // A proxy, and an honest one: happy-dom synthesises no click from Enter
    // or Space, so pressing them here proves nothing either way. The element
    // type is what a browser reads to decide whether it should.
    expect(trigger.tagName).toBe("BUTTON");
  });

  it("marks the open control apart from every other one", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    const trigger = await peekOn(FIRST_ORG.name);
    const other = await peekTrigger(SECOND_ORG.name);

    // The variant, not the classes it expands to. Peek hides the name column
    // it opened from, so the fill is the only thing left on screen that says
    // which control the panel belongs to, and the ghost hover token and the
    // peeked row token are the same grey in this theme.
    expect(trigger.getAttribute("data-variant")).toBe("default");
    expect(other.getAttribute("data-variant")).toBe("ghost");
  });

  it("describes the control to an operator who is not using a screen reader", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const trigger = await peekTrigger(FIRST_ORG.name);

    // An icon on its own says nothing to a sighted operator, and the label
    // this replaces only ever announced itself to a screen reader.
    fireEvent.focus(trigger);

    const tip = await screen.findByRole("tooltip");
    expect(tip.textContent).toBe("Peek without leaving the list");
    // Describing the control must not rename it.
    expect(
      screen.getAllByRole("button", { name: `Peek at ${FIRST_ORG.name}` }),
    ).toHaveLength(1);
  });

  it("carries the keyboard along when the arrow keys move the peek off a control", async () => {
    // Three rows, because the trap only shows on the second press: the first
    // move works and leaves the keyboard behind on the row it moved off.
    mocks.listOrganizations.mockResolvedValue({
      organizations: [FIRST_ORG, SECOND_ORG, NEXT_PAGE_ORG],
    });
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    const trigger = await peekOn(FIRST_ORG.name);
    trigger.focus();

    fireEvent.keyDown(trigger, { key: "ArrowDown" });

    // The control the peek moved to, or the next press meets the guard that
    // keeps other rows' controls out of this handler and the operator is
    // stuck on a control that answers nothing.
    const second = await peekTrigger(SECOND_ORG.name);
    expect(document.activeElement).toBe(second);

    fireEvent.keyDown(second, { key: "ArrowDown" });

    expect(
      within(peekPanel()).getByRole("heading", { name: NEXT_PAGE_ORG.name }),
    ).toBeTruthy();
    expect(document.activeElement).toBe(await peekTrigger(NEXT_PAGE_ORG.name));
  });

  it("closes on Escape after the arrow keys have moved the peek", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const trigger = await peekOn(FIRST_ORG.name);
    trigger.focus();

    fireEvent.keyDown(trigger, { key: "ArrowDown" });
    const second = await peekTrigger(SECOND_ORG.name);
    fireEvent.keyDown(second, { key: "Escape" });

    // Escape has to keep working from wherever the arrow keys left the
    // operator. A keyboard that can move but cannot close is trapped.
    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
  });

  it("leaves the keyboard in the panel when the arrow keys come from the panel", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);
    // The panel takes the focus on mount, and this is the panel's own record
    // navigation.
    expect(document.activeElement).toBe(peekPanel());

    fireEvent.keyDown(peekPanel(), { key: "ArrowDown" });

    // Following the peek out to the row control here would take the operator
    // off the record they are reading and out of the panel they opened.
    expect(
      within(peekPanel()).getByRole("heading", { name: SECOND_ORG.name }),
    ).toBeTruthy();
    expect(document.activeElement).toBe(peekPanel());
  });

  it("ignores the arrow keys on a control that is not the peeked row's", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);
    const other = await peekTrigger(SECOND_ORG.name);

    // An operator on another row's control presses this to scroll. Answering
    // it would move the panel away from where they are looking, swallow the
    // scroll, and leave them on a control still reporting itself closed.
    const event = new KeyboardEvent("keydown", {
      key: "ArrowDown",
      bubbles: true,
      cancelable: true,
    });
    other.dispatchEvent(event);

    expect(
      within(peekPanel()).getByRole("heading", { name: FIRST_ORG.name }),
    ).toBeTruthy();
    expect(event.defaultPrevented).toBe(false);
  });

  it("ignores Escape on a control that is not the peeked row's", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);
    const other = await peekTrigger(SECOND_ORG.name);
    other.focus();

    fireEvent.keyDown(other, { key: "Escape" });

    // Closing from here would pull the keyboard onto the peeked row, which is
    // a place the operator did not ask to go.
    expect(peekPanel()).toBeTruthy();
    expect(document.activeElement).toBe(other);
  });

  it("leaves a key alone once something nearer has answered it", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const first = await screen.findByRole("link", { name: FIRST_ORG.name });
    await peekOn(FIRST_ORG.name);

    // React listens at the root, so a native listener on the panel stands in
    // for anything inside it that answers the key first. Moving the peek on
    // top of that answer would act on a key already spoken for.
    const panel = peekPanel();
    panel.addEventListener("keydown", (event) => event.preventDefault());
    fireEvent.keyDown(panel, { key: "ArrowDown" });

    expect(
      within(peekPanel()).getByRole("heading", { name: FIRST_ORG.name }),
    ).toBeTruthy();
    expect(isPeeked(first)).toBe(true);
  });

  it("marks the arrow key that moved the peek as answered", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);

    const panel = peekPanel();
    const event = new KeyboardEvent("keydown", {
      key: "ArrowDown",
      bubbles: true,
      cancelable: true,
    });
    fireEvent(panel, event);

    // The mirror of every case that asserts the key was left alone. Moving the
    // peek and letting the key travel on scrolls the list out from under the
    // record it just moved to, which is the same eaten-scroll complaint from
    // the other side.
    expect(
      within(peekPanel()).getByRole("heading", { name: SECOND_ORG.name }),
    ).toBeTruthy();
    expect(event.defaultPrevented).toBe(true);
  });

  it("puts the panel in the tab order so the arrow keys stay reachable", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);

    const panel = peekPanel();
    const close = within(panel).getByRole("button", { name: "Close peek" });

    // Shift+Tab off Close has to land here. Sequential navigation skips a
    // tabindex="-1" node, so with the panel out of the order the focus it takes
    // on mount was the only focus it ever got: one Tab to Close and the record
    // navigation below was gone for the rest of the panel's life.
    expect(tabStopBefore(close)).toBe(panel);

    panel.focus();
    fireEvent.keyDown(panel, { key: "ArrowDown" });

    expect(
      within(peekPanel()).getByRole("heading", { name: SECOND_ORG.name }),
    ).toBeTruthy();
  });

  it("ignores the arrow keys pressed on the pager", async () => {
    mocks.listOrganizations.mockResolvedValue({
      organizations: ORGS,
      next_cursor: "cursor_page_two",
    });
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const first = await screen.findByRole("link", { name: FIRST_ORG.name });
    await peekOn(FIRST_ORG.name);

    const next = screen.getByRole("button", { name: "Next" });
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });
    next.focus();
    const event = new KeyboardEvent("keydown", {
      key: "ArrowDown",
      bubbles: true,
      cancelable: true,
    });
    next.dispatchEvent(event);

    expect(
      within(peekPanel()).getByRole("heading", { name: FIRST_ORG.name }),
    ).toBeTruthy();
    expect(isPeeked(first)).toBe(true);
    // The pager sits under the same handler as the panel. Answering it here
    // also eats the scroll the operator pressed the key for.
    expect(event.defaultPrevented).toBe(false);
  });

  it("ignores Escape pressed on the pager", async () => {
    mocks.listOrganizations.mockResolvedValue({
      organizations: ORGS,
      next_cursor: "cursor_page_two",
    });
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);

    const next = screen.getByRole("button", { name: "Next" });
    next.focus();
    fireEvent.keyDown(next, { key: "Escape" });

    // Closing from here would pull the keyboard off the pager and onto the
    // peeked row, which is a place the operator did not ask to go.
    expect(peekPanel()).toBeTruthy();
    expect(document.activeElement).toBe(next);
  });

  it("ignores the arrow keys pressed on a control inside the panel", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const first = await screen.findByRole("link", { name: FIRST_ORG.name });
    await peekOn(FIRST_ORG.name);

    // Inside the panel, and not the panel. The panel's own record navigation
    // belongs to the container that holds the focus; a button in the body has
    // its own keys, and the operator pressing this one wants to scroll.
    const copy = within(peekPanel()).getByRole("button", {
      name: "Copy Org id",
    });
    const event = new KeyboardEvent("keydown", {
      key: "ArrowDown",
      bubbles: true,
      cancelable: true,
    });
    copy.dispatchEvent(event);

    expect(
      within(peekPanel()).getByRole("heading", { name: FIRST_ORG.name }),
    ).toBeTruthy();
    expect(isPeeked(first)).toBe(true);
    expect(event.defaultPrevented).toBe(false);
  });

  it("closes on Escape pressed on a control inside the panel", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const trigger = await peekOn(FIRST_ORG.name);

    // Escape is scoped wider than the arrow keys, and this is the case that
    // separates them. It has no reflex of its own for the panel to steal, and
    // it dismisses whichever surface holds the focus, so the button answers it
    // rather than swallowing it and leaving the operator with a dead key.
    const copy = within(peekPanel()).getByRole("button", {
      name: "Copy Org id",
    });
    copy.focus();
    fireEvent.keyDown(copy, { key: "Escape" });

    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
    // The button unmounts with the panel, so closing from inside it has to hand
    // the keyboard somewhere or it lands on the body.
    expect(document.activeElement).toBe(trigger);
  });

  it("marks the Escape that closed the panel as answered", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);

    // Escape travels on past this handler, and a layer above it dismisses on
    // the same key. Closing quietly spends one keypress on two surfaces, which
    // is the mirror of the case above and the reason that guard can be trusted.
    const copy = within(peekPanel()).getByRole("button", {
      name: "Copy Org id",
    });
    // Dispatched through fireEvent rather than on the node, so the close it
    // causes is flushed before the panel is looked for. The event is built here
    // because the answer being asserted is on the event itself.
    const event = new KeyboardEvent("keydown", {
      key: "Escape",
      bubbles: true,
      cancelable: true,
    });
    fireEvent(copy, event);

    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
    expect(event.defaultPrevented).toBe(true);
  });

  it("leaves Escape alone inside the panel once something nearer has answered it", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);

    // A tooltip or a popover opened from inside the panel calls preventDefault
    // on the Escape that dismisses it. Closing the panel on that one takes the
    // operator two surfaces back for one keypress, and this is the only path
    // where it can happen: before Escape reached the panel body at all, the
    // panel could not close underneath a control that had already answered.
    const copy = within(peekPanel()).getByRole("button", {
      name: "Copy Org id",
    });
    copy.addEventListener("keydown", (event) => event.preventDefault());
    fireEvent.keyDown(copy, { key: "Escape" });

    expect(peekPanel()).toBeTruthy();
  });

  it("still closes on Escape pressed on the peeked row's own control", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const trigger = await peekOn(FIRST_ORG.name);
    trigger.focus();

    // The allow-list has to keep this arm, or closing from the control that
    // opened the panel stops working.
    const event = new KeyboardEvent("keydown", {
      key: "Escape",
      bubbles: true,
      cancelable: true,
    });
    fireEvent(trigger, event);

    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
    // Both arms of the allow-list answer the key the same way. Marking one and
    // not the other leaves the unmarked close travelling on to whatever else
    // dismisses on Escape.
    expect(event.defaultPrevented).toBe(true);
  });

  it("announces a move onto an organization that shares the peeked one's name", async () => {
    // Names are not unique. Two records, one name, so both announcements are
    // the same sentence word for word.
    const TWIN: AdminOrganization = {
      ...SECOND_ORG,
      id: "org_placeholder_twin",
      slug: "placeholder-twin",
      name: FIRST_ORG.name,
    };
    mocks.listOrganizations.mockResolvedValue({
      organizations: [FIRST_ORG, TWIN],
    });
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    const [first, twin] = await screen.findAllByRole("button", {
      name: `Peek at ${FIRST_ORG.name}`,
    });
    if (!first || !twin) throw new Error("both rows need a control");

    fireEvent.click(first);
    const spoken = liveRegion().textContent;

    fireEvent.click(twin);

    // The panel moved to the other record.
    expect(twin.getAttribute("aria-expanded")).toBe("true");
    expect(first.getAttribute("aria-expanded")).toBe("false");
    expect(announcement()).toBe(`Peeking at ${FIRST_ORG.name}.`);

    // A live region speaks when its text changes. Set the same string twice
    // and the DOM never moves, so the operator hears nothing at all while the
    // panel in front of them swaps records.
    expect(liveRegion().textContent).not.toBe(spoken);
  });

  it("keeps an empty status region on the page before anything is peeked", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    // A live region injected together with its text is not reliably announced,
    // so the region has to be on the page before it has anything to say.
    expect(announcement()).toBe("");
  });

  it("announces through one polite region that outlives every announcement", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const region = await screen.findByRole("status");

    // Polite, or nothing is spoken at all and every assertion in this file
    // about what the region says reads a dead feature's DOM text.
    expect(region.getAttribute("aria-live")).toBe("polite");
    // Off screen. Otherwise the announcements pile up as stray text above the
    // table, where a sighted operator reads them and nobody asked for them.
    expect(region.classList.contains("sr-only")).toBe(true);

    await peekOn(FIRST_ORG.name);

    // The same node. A region that arrives together with its text is not
    // announced, and a region keyed by its own text is a new one every time,
    // which is the same defect wearing a different hat.
    expect(liveRegion()).toBe(region);
    expect(announcement()).toBe(`Peeking at ${FIRST_ORG.name}.`);
  });

  it("opens the organization on an alt-click rather than peeking at it", async () => {
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

  it("closes the peek to show a column the peek was hiding", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);
    expect(screen.queryByRole("columnheader", { name: "Members" })).toBeNull();

    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Members" }));

    // Checking a column is an unambiguous request to see it, and the panel is
    // the only thing in the way. Granting it explains itself: the panel goes
    // and the column arrives in the same commit, where the write on its own
    // was swallowed and the checkbox snapped back with nothing to say.
    await waitFor(() => {
      expect(
        screen.getByRole("columnheader", { name: "Members" }),
      ).toBeTruthy();
    });
    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
    expect(announcement()).toBe("Peek closed to show the Members column.");
  });

  it("leaves the keyboard on the Columns control when a column closes the peek", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);
    const columns = screen.getByRole("button", { name: "Columns" });
    const table = screen.getByRole("region", { name: "Organizations table" });

    openOn(columns);

    // Every node focus passes through, not just where it ends up. Radix pulls
    // the keyboard back to this trigger as the menu closes, which happens after
    // the page has had its turn, so the end state is the same whatever the page
    // did in between and asserting only that state asserts Radix.
    const visited: Node[] = [];
    const record = (event: FocusEvent): void => {
      if (event.target instanceof Node) visited.push(event.target);
    };
    document.addEventListener("focusin", record);
    try {
      fireEvent.click(
        screen.getByRole("menuitemcheckbox", { name: "Members" }),
      );
      await waitFor(() => {
        expect(
          screen.getByRole("columnheader", { name: "Members" }),
        ).toBeTruthy();
      });
    } finally {
      document.removeEventListener("focusin", record);
    }

    // An operator's own close puts the keyboard back on the peek control, and
    // the rescue that catches a peeked record leaving the list puts it on the
    // table. Either one here drags them out of the menu they are working in,
    // even for the moment before Radix takes the keyboard back: a screen reader
    // announces wherever it landed. So the rule is the whole region, not the
    // two nodes that happen to be reachable from this handler today.
    expect(visited.filter((node) => table.contains(node))).toEqual([]);
    expect(document.activeElement).toBe(columns);
  });

  it("closes the peek to show the Trial column the peek was hiding", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);
    expect(screen.queryByRole("columnheader", { name: "Trial" })).toBeNull();

    // A second column out of the set, because one member travelling this path
    // does not show the set travels it. Peek hides five and the menu can write
    // all five.
    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Trial" }));

    await waitFor(() => {
      expect(screen.getByRole("columnheader", { name: "Trial" })).toBeTruthy();
    });
    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
    expect(announcement()).toBe("Peek closed to show the Trial column.");
  });

  it("closes the peek to hide the column the peek was forcing visible", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);
    expect(screen.getByRole("columnheader", { name: "Name" })).toBeTruthy();

    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Name" }));

    // The override runs both ways, and this is the other way. Peek is the only
    // reason Name is on screen, so unchecking it is a request peek is in the
    // way of just as much as a request to show one it hides. Left unanswered,
    // the write lands under the override and the column goes when the peek
    // closes, at a moment the operator cannot connect to this click.
    await waitFor(() => {
      expect(screen.queryByRole("columnheader", { name: "Name" })).toBeNull();
    });
    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
    // "show" here would be a false sentence, and one wording cannot serve both
    // directions.
    expect(announcement()).toBe("Peek closed to hide the Name column.");
  });

  it("keeps the peek open for a column the peek was not hiding", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    // Hidden before the peek opens. Peek leaves this column visible, and a
    // column the menu already reports as checked is not one an operator can
    // ask for.
    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Slug" }));
    await waitFor(() => {
      expect(screen.queryByRole("columnheader", { name: "Slug" })).toBeNull();
    });

    await peekOn(FIRST_ORG.name);

    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Slug" }));

    await waitFor(() => {
      expect(screen.getByRole("columnheader", { name: "Slug" })).toBeTruthy();
    });
    // Nothing was in the way of this one, so nothing has to give way for it.
    // A panel that went here would be closing on any menu click at all.
    expect(peekPanel()).toBeTruthy();
    expect(announcement()).toBe(`Peeking at ${FIRST_ORG.name}.`);
  });

  it("says nothing about the peek when a column is toggled with none open", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Members" }));

    // The same column, and no panel in the way of it. Announcing a close here
    // would report a panel that was never open, over a column that just went.
    await waitFor(() => {
      expect(
        screen.queryByRole("columnheader", { name: "Members" }),
      ).toBeNull();
    });
    expect(announcement()).toBe("");
  });

  it("says nothing about the peek when a column peek never touches is toggled with none open", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Slug" }));

    // Neither condition holds: no panel, and a column no panel would override.
    // The test above it varies only one of the two, so between them a handler
    // that announced on both being false would have nothing to fail.
    await waitFor(() => {
      expect(screen.queryByRole("columnheader", { name: "Slug" })).toBeNull();
    });
    expect(announcement()).toBe("");
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
    // happy-dom focuses elements a browser will not, so "activeElement moved"
    // is no evidence that focus could land there at all. Every focus
    // assertion in this file has to check the target is focusable in its own
    // right, and this is that check.
    expect(document.activeElement?.getAttribute("tabindex")).toBe("-1");
    // The box around the table and nothing wider. `contains(table)` is true
    // of every ancestor up to the document, so a rescue that landed on the
    // wrapper holding the pager would satisfy the line above and drop the
    // operator somewhere they can no longer see the record they lost.
    expect(document.activeElement?.contains(next)).toBe(false);
    // Named, or the operator arrives somewhere their screen reader announces
    // as nothing at all.
    expect(document.activeElement).toBe(
      screen.getByRole("region", { name: "Organizations table" }),
    );
    // Worded apart from an operator's own close, which says nothing about why.
    expect(announcement()).toBe(
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
  // Sliced, so the array carries the element type useTable asks for. Two data
  // columns for the cases about the bar's own two rules, and the peek column
  // beside one of them for the case where an unhideable column is in the count.
  const MENU_COLUMNS = ORG_COLUMNS.slice(1, 3);
  const WITH_PEEK_COLUMN = ORG_COLUMNS.slice(0, 2);

  // Destructured off the tuple rather than off the slice, which widens each
  // element back to a bare column definition.
  const [PEEK, FIRST, SECOND, THIRD] = ORG_COLUMNS;
  if (!PEEK || !FIRST || !SECOND || !THIRD) {
    throw new Error("ORG_COLUMNS needs four columns");
  }

  // An accessor column takes its id from its key unless it names one, and that
  // id is what the visibility state is keyed by.
  const FIRST_ID = String(FIRST.id ?? FIRST.accessorKey);
  const SECOND_ID = String(SECOND.id ?? SECOND.accessorKey);

  function ColumnsMenu({
    initialVisibility,
    onVisibilityChange,
    onColumnToggled,
    columns,
  }: {
    initialVisibility: ColumnVisibilityState;
    onVisibilityChange: (updater: Updater<ColumnVisibilityState>) => void;
    onColumnToggled: (columnId: string, label: string) => void;
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
    return <TableActionBar table={table} onColumnToggled={onColumnToggled} />;
  }

  type MenuSpies = {
    onVisibilityChange: Mock<(updater: Updater<ColumnVisibilityState>) => void>;
    onColumnToggled: Mock<(columnId: string, label: string) => void>;
  };

  function openColumnsMenu(
    initialVisibility: ColumnVisibilityState,
    columns: typeof MENU_COLUMNS = MENU_COLUMNS,
  ): MenuSpies {
    const spies: MenuSpies = {
      onVisibilityChange:
        vi.fn<(updater: Updater<ColumnVisibilityState>) => void>(),
      onColumnToggled: vi.fn<(columnId: string, label: string) => void>(),
    };
    render(
      <ColumnsMenu
        initialVisibility={initialVisibility}
        onVisibilityChange={spies.onVisibilityChange}
        onColumnToggled={spies.onColumnToggled}
        columns={columns}
      />,
    );
    openOn(screen.getByRole("button", { name: "Columns" }));
    return spies;
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
    const { onVisibilityChange } = openColumnsMenu({ [SECOND_ID]: false });

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
    const { onVisibilityChange } = openColumnsMenu({ [SECOND_ID]: false });

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
    const { onVisibilityChange } = openColumnsMenu({});

    fireEvent.click(itemFor(FIRST.header));
    expect(onVisibilityChange).toHaveBeenCalled();

    reopenColumnsMenu();
    expect(itemFor(FIRST.header).getAttribute("aria-checked")).toBe("false");
  });

  it("reports a toggle that landed, by column and by the name on the item", () => {
    const { onColumnToggled } = openColumnsMenu({});

    const item = itemFor(FIRST.header);
    fireEvent.click(item);

    // The label as well as the id, because the page that acts on this holds
    // only the id and cannot name the column back to the operator without it.
    // Read off the item, so the two cannot disagree about what it is called.
    expect(onColumnToggled).toHaveBeenCalledWith(FIRST_ID, item.textContent);
  });

  it("reports nothing for a toggle the lock refused", () => {
    const { onColumnToggled } = openColumnsMenu({ [SECOND_ID]: false });

    fireEvent.click(itemFor(FIRST.header));

    // Radix fires onCheckedChange whether or not the select was prevented, so
    // an unguarded report would tell the page a column changed when the lock
    // had just held it where it was.
    expect(onColumnToggled).not.toHaveBeenCalled();
  });

  it("stops the operator hiding the last column that carries data", () => {
    // Peek and Name, both visible. Peek opts out of hiding, so it is on screen
    // whatever the operator does and it is not the column that keeps the table
    // readable. Counting it would leave Name free to go, and the table behind
    // this menu would be a strip of controls above rows holding no record.
    const { onVisibilityChange } = openColumnsMenu({}, WITH_PEEK_COLUMN);

    const item = itemFor(FIRST.header);
    fireEvent.click(item);

    expect(onVisibilityChange).not.toHaveBeenCalled();
    expect(item.getAttribute("aria-disabled")).toBe("true");
    // The peek column is locked too, by its own opt-out rather than by this
    // rule, so the operator cannot reach the same state from the other side.
    expect(itemFor(PEEK.header).getAttribute("aria-disabled")).toBe("true");
  });

  it("locks a column that opts out of hiding", () => {
    // Three visible, two of them hideable, so the last-data-column rule is not
    // what holds this one and hiding the next one along is still allowed.
    const { onVisibilityChange } = openColumnsMenu({}, [
      { ...FIRST, enableHiding: false },
      SECOND,
      THIRD,
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
