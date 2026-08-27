import type { AnyRouter } from "@tanstack/react-router";
import {
  useTable,
  type ColumnVisibilityState,
  type Updater,
} from "@tanstack/react-table";
import {
  act,
  cleanup,
  createEvent,
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

import {
  GramAdminError,
  TRIAL_STATES,
  type AdminOrganization,
  type AdminOrganizationStats,
  type BulkUpdateAccountTypeRequest,
  type BulkUpdateAccountTypeResult,
  type ListOrganizationsParams,
  type ListOrganizationsResult,
} from "@/lib/gramAdminApi";
import { ACCOUNT_TYPE_OPTIONS } from "@/lib/accountTypes";
import { TRIAL_LABELS } from "@/lib/trialLabels";
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
  getOrganizationStats: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listOrganizationMembers: vi.fn(),
  disableOrganization:
    vi.fn<(body: { id: string }) => Promise<AdminOrganization>>(),
  enableOrganization:
    vi.fn<(body: { id: string }) => Promise<AdminOrganization>>(),
  extendTrial:
    vi.fn<(body: { id: string; days: number }) => Promise<AdminOrganization>>(),
  rearmTrial:
    vi.fn<(body: { id: string; days: number }) => Promise<AdminOrganization>>(),
  startTrial:
    vi.fn<(body: { id: string; days: number }) => Promise<AdminOrganization>>(),
  bulkUpdateAccountType:
    vi.fn<
      (
        body: BulkUpdateAccountTypeRequest,
      ) => Promise<BulkUpdateAccountTypeResult>
    >(),
  createOrganization:
    vi.fn<(body: { name: string }) => Promise<AdminOrganization>>(),
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
    getOrganizationStats: mocks.getOrganizationStats,
    getOrganization: mocks.getOrganization,
    listOrganizationProjects: mocks.listOrganizationProjects,
    listOrganizationMembers: mocks.listOrganizationMembers,
    disableOrganization: mocks.disableOrganization,
    enableOrganization: mocks.enableOrganization,
    extendTrial: mocks.extendTrial,
    rearmTrial: mocks.rearmTrial,
    startTrial: mocks.startTrial,
    bulkUpdateAccountType: mocks.bulkUpdateAccountType,
    createOrganization: mocks.createOrganization,
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
    trial_state: "none",
    member_count: 7,
    created_at: "2026-06-08T00:00:00Z",
    updated_at: "2026-06-09T00:00:00Z",
  },
  // Live, and mid-trial. Extend trial is offered on this row and on no other:
  // the first row's trial is running too, but the organization is disabled and
  // a disabled organization is not offered more of a trial nobody can use.
  {
    id: "org_placeholder_four",
    name: "Placeholder Four",
    slug: "placeholder-four",
    account_type: "enterprise",
    whitelisted: false,
    trial_state: "ending_soon",
    trial_ends_at: "2026-05-06T00:00:00Z",
    member_count: 5,
    created_at: "2026-04-02T00:00:00Z",
    updated_at: "2026-04-07T00:00:00Z",
  },
];

const STATS: AdminOrganizationStats = {
  total: 12,
  created_last_7_days: 3,
  customers: 4,
  customers_created_last_7_days: 1,
  trials_ending_soon: 2,
  disabled: 1,
  disabled_last_7_days: 1,
};

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

const [FIRST_ORG, SECOND_ORG, TRIALLING_ORG] = ORGS;
if (!FIRST_ORG || !SECOND_ORG || !TRIALLING_ORG) {
  throw new Error("ORGS needs three rows");
}

// The first row is disabled and its trial is running; the second is live and
// never trialled. Between them every action has a row that offers it and a row
// that does not.
if (!FIRST_ORG.disabled_at || SECOND_ORG.disabled_at) {
  throw new Error("the fixture needs one disabled row and one live row");
}

// What each write answers with, dated apart from anything the fixture already
// carries so a row that failed to repaint cannot read as one that did.
const DISABLED_AT = "2026-08-01T00:00:00Z";
const EXTENDED_TRIAL_END = "2026-05-20T00:00:00Z";
const STARTED_TRIAL_END = "2026-08-28T00:00:00Z";

function orgByID(id: string): AdminOrganization {
  const org = ORGS.find((row) => row.id === id);
  if (!org) throw new Error(`no fixture organization with id ${id}`);
  return org;
}

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

// One turn of the macrotask queue, flushed through act. An assertion that
// something did *not* navigate has to give the navigation a chance to happen
// first, or it passes against a router that simply had not got there yet.
async function settle(): Promise<void> {
  await act(async () => {
    await new Promise<void>((resolve) => {
      setTimeout(resolve, 0);
    });
  });
}

// The router JSON-encodes and then percent-encodes an array, so decoding once
// lets an assertion name the value the way the schema declares it.
function currentSearch(router: AnyRouter): string {
  return decodeURIComponent(router.state.location.searchStr);
}

// The three triggers above the table. Found by the state a screen reader is
// told, because the visible label is the group's name and a bare count.
function filterTrigger(group: string): HTMLElement {
  return screen.getByRole("button", { name: new RegExp(`^${group} filter:`) });
}

// Focused before the click, because a click does not focus a button in every
// browser and the sheet hands the keyboard back to the trigger that opened it.
async function openFilters(group: string): Promise<HTMLElement> {
  const trigger = filterTrigger(group);
  trigger.focus();
  fireEvent.click(trigger);
  await screen.findByRole("dialog");
  return trigger;
}

// A picker inside the sheet. Its accessible name is the group's heading
// followed by its own text, which is what the group is filtering on.
function picker(group: string): HTMLElement {
  return screen.getByRole("combobox", { name: new RegExp(`^${group}`) });
}

// Radix opens a popover on click, unlike the menu and the select below. A
// choice does not close it, because the group takes more than one, so a second
// call must not toggle the open list shut.
async function chooseFilter(group: string, option: string): Promise<void> {
  const trigger = picker(group);
  if (trigger.getAttribute("aria-expanded") !== "true") {
    fireEvent.click(trigger);
  }
  fireEvent.click(await screen.findByRole("option", { name: option }));
}

function applyFilters(): void {
  fireEvent.click(screen.getByRole("button", { name: "Apply" }));
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
// The marker the region alternates. Written out rather than imported, so a
// change to the page's own constant has to be made here as well.
const ZERO_WIDTH_SPACE = "\u200b";

function announcement(): string {
  return (liveRegion().textContent ?? "").replaceAll(ZERO_WIDTH_SPACE, "");
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
  mocks.getOrganizationStats.mockReset();
  mocks.getOrganizationStats.mockResolvedValue(STATS);
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(FIRST_ORG);
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
  mocks.listOrganizationMembers.mockReset();
  mocks.listOrganizationMembers.mockResolvedValue({ members: [] });
  // Each write answers with the record in its new state, which is what the
  // list repaints from. Answered off the id in the request, so a test can act
  // on either row and still be given that row back.
  mocks.disableOrganization.mockReset();
  mocks.disableOrganization.mockImplementation(({ id }) =>
    Promise.resolve({ ...orgByID(id), disabled_at: DISABLED_AT }),
  );
  mocks.enableOrganization.mockReset();
  mocks.enableOrganization.mockImplementation(({ id }) =>
    Promise.resolve({ ...orgByID(id), disabled_at: undefined }),
  );
  mocks.extendTrial.mockReset();
  mocks.extendTrial.mockImplementation(({ id }) =>
    Promise.resolve({ ...orgByID(id), trial_ends_at: EXTENDED_TRIAL_END }),
  );
  // No default answer: the one describe that re-arms owns a record none of the
  // rows above it carry, so it supplies its own.
  mocks.rearmTrial.mockReset();
  mocks.startTrial.mockReset();
  mocks.startTrial.mockImplementation(({ id }) =>
    Promise.resolve({
      ...orgByID(id),
      account_type: "enterprise",
      whitelisted: true,
      trial_state: "running",
      trial_ends_at: STARTED_TRIAL_END,
    }),
  );
  // Everything the request asked for, and nothing missing. The reversal guards
  // nothing on its own, because only the length of this array is ever read;
  // "names the organizations the server could not find" is the test that holds
  // the reporting, by answering a different set from the one it was sent.
  mocks.bulkUpdateAccountType.mockReset();
  mocks.bulkUpdateAccountType.mockImplementation(({ ids }) =>
    Promise.resolve({ updated_ids: [...ids].reverse(), missing_ids: [] }),
  );
  mocks.createOrganization.mockReset();
  mocks.createOrganization.mockImplementation(({ name }) =>
    Promise.resolve({ ...CREATED_ORG, name }),
  );
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
      initialPath: urlFor({
        type: ["pro"],
        trial: ["running", "expired"],
        disabled: ["active", "disabled"],
      }),
    });

    // The one chosen value is named; more than one is counted, because the
    // trigger has a row of controls to share.
    expect(filterTrigger("Type").getAttribute("aria-label")).toContain("pro");
    expect(filterTrigger("Trial").getAttribute("aria-label")).toContain(
      "2 selected",
    );

    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });
    expect(lastListParams()).toMatchObject({
      account_types: ["pro"],
      trial_states: ["running", "expired"],
      disabled_states: ["active", "disabled"],
    });

    await openFilters("Type");
    expect(picker("Type").textContent).toContain("pro");
  });

  it("sends every value of a group the operator picked", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    await openFilters("Type");
    // Picked in the other order, so the assertion below is about the order the
    // schema settles on rather than the order they were clicked. Two operators
    // choosing the same filter must produce one request and one cache entry.
    await chooseFilter("Type", "enterprise");
    await chooseFilter("Type", "free");
    applyFilters();

    await waitFor(() => {
      expect(lastListParams().account_types).toEqual(["free", "enterprise"]);
    });
    expect(currentSearch(router)).toContain('type=["free","enterprise"]');
  });

  it("filters by payg, which the type picker offers", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    await openFilters("Type");
    await chooseFilter("Type", "payg");
    applyFilters();

    await waitFor(() => {
      expect(lastListParams().account_types).toEqual(["payg"]);
    });
    expect(currentSearch(router)).toContain('type=["payg"]');
  });

  it("holds the table still until the operator applies", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });
    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });
    const before = mocks.listOrganizations.mock.calls.length;

    await openFilters("Trial");
    await chooseFilter("Trial", "Running");
    await chooseFilter("Trial", "Expired");

    // Three requests and two lists nobody asked for, if the edit went straight
    // to the URL.
    expect(mocks.listOrganizations.mock.calls.length).toBe(before);
    expect(currentSearch(router)).toBe("");

    applyFilters();

    await waitFor(() => {
      expect(lastListParams().trial_states).toEqual(["running", "expired"]);
    });
  });

  it("discards the edit when the operator presses Escape", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });
    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });

    const trigger = await openFilters("Type");
    await chooseFilter("Type", "enterprise");

    // The open picker is a dismiss layer of its own and takes the first
    // Escape. The sheet takes the second.
    fireEvent.keyDown(document.body, { key: "Escape" });
    fireEvent.keyDown(document.body, { key: "Escape" });

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(currentSearch(router)).toBe("");
    expect(lastListParams().account_types).toBeUndefined();
    // Closing must not strand the keyboard on a subtree that has unmounted.
    expect(document.activeElement).toBe(trigger);

    // Discarded, not merely unapplied: the edit is gone when the sheet is
    // opened again.
    await openFilters("Type");
    expect(picker("Type").textContent).toContain("All types");
  });

  it("holds the list open so a second value can be picked off it", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await openFilters("Type");
    // The trigger is clicked once and only once. Every other test here goes
    // through `chooseFilter`, which reopens a closed list, so a picker that
    // shut itself on each choice would pass all of them: the operator would be
    // reopening the list for every value, and nothing would say so.
    fireEvent.click(picker("Type"));
    fireEvent.click(await screen.findByRole("option", { name: "enterprise" }));

    expect(picker("Type").getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(screen.getByRole("option", { name: "free" }));
    expect(picker("Type").getAttribute("aria-expanded")).toBe("true");

    applyFilters();
    await waitFor(() => {
      expect(lastListParams().account_types).toEqual(["free", "enterprise"]);
    });
  });

  it("gives the keyboard back to the trigger after applying", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });

    await openFilters("Trial");
    await chooseFilter("Trial", "Expired");
    applyFilters();

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    // Escape is not the only way out of the sheet. Applying unmounts the same
    // subtree, and must not leave the keyboard on it either.
    expect(document.activeElement).toBe(filterTrigger("Trial"));
    await waitFor(() => {
      expect(lastListParams().trial_states).toEqual(["expired"]);
    });
  });

  it("clears every filter and leaves the search term alone", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: urlFor({
        q: "acme",
        type: ["pro"],
        disabled: ["disabled"],
      }),
    });
    await waitFor(() => {
      expect(lastListParams().account_types).toEqual(["pro"]);
    });

    await openFilters("Status");
    fireEvent.click(screen.getByRole("button", { name: "Clear all" }));

    await waitFor(() => {
      expect(lastListParams().account_types).toBeUndefined();
    });
    expect(lastListParams().disabled_states).toBeUndefined();
    // The term is not a filter this sheet holds. An operator who cleared the
    // filters has not asked to type it again.
    expect(lastListParams().q).toBe("acme");
    expect(currentSearch(router)).toContain("q=acme");
    expect(
      (screen.getByLabelText("Search organizations") as HTMLInputElement).value,
    ).toBe("acme");
  });

  it("counts a full set of types rather than calling it all of them", async () => {
    await renderRouteTree(routeTree, {
      initialPath: urlFor({ type: [...ACCOUNT_TYPE_OPTIONS] }),
    });

    await waitFor(() => {
      expect(lastListParams().account_types).toEqual([...ACCOUNT_TYPE_OPTIONS]);
    });
    // An organization can carry a type the picker does not offer, so every
    // option at once is still a narrowing and must not read as "All types".
    expect(filterTrigger("Type").getAttribute("aria-label")).toBe(
      `Type filter: ${ACCOUNT_TYPE_OPTIONS.length} selected`,
    );
  });

  it("keeps an account type the picker does not offer", async () => {
    // ACCOUNT_TYPE_OPTIONS is the list the picker offers, not the list the
    // column can hold. Dropping a value from outside it would widen the view a
    // link carries while the control read "all types".
    await renderRouteTree(routeTree, {
      initialPath: urlFor({ type: ["startup"] }),
    });

    await waitFor(() => {
      expect(lastListParams().account_types).toEqual(["startup"]);
    });
    expect(filterTrigger("Type").getAttribute("aria-label")).toContain(
      "startup",
    );

    await openFilters("Type");
    fireEvent.click(picker("Type"));
    const option = await screen.findByRole("option", { name: "startup" });
    expect(option.getAttribute("aria-checked")).toBe("true");
  });

  // These two assert the request rather than the parsed search, because the
  // empty string has to be gone by the time anything is sent, and asserting the
  // schema's output would pass while it survived one step further on.
  //
  // The hazard is specific to the type group. `trialStates` and
  // `disabledStates` drop whatever they do not recognise, so an empty string
  // reaching them is harmless. `accountTypes` deliberately keeps an
  // unrecognised value, so an empty one would be sent as an account type, and
  // the server matches no organization against it: a blank table under a
  // control still reading "All types".
  it("sends no filter for a group a link left empty", async () => {
    // `?type=` is not the empty list the schema already drops. It arrives as an
    // empty string, and `text()` normalising to undefined is the only thing
    // between it and the request.
    await renderRouteTree(routeTree, {
      initialPath: "/organizations?type=&trial=&disabled=",
    });

    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });
    const params = lastListParams();
    expect(params.account_types).toBeUndefined();
    expect(params.trial_states).toBeUndefined();
    expect(params.disabled_states).toBeUndefined();
    expect(filterTrigger("Type").getAttribute("aria-label")).toContain(
      "All types",
    );
  });

  it("sends no filter for a group a link filled with whitespace", async () => {
    await renderRouteTree(routeTree, {
      initialPath: "/organizations?type=%20&trial=%20&disabled=%20",
    });

    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });
    const params = lastListParams();
    expect(params.account_types).toBeUndefined();
    expect(params.trial_states).toBeUndefined();
    expect(params.disabled_states).toBeUndefined();
  });

  it("opens on the group the operator asked for", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await openFilters("Trial");

    // The sheet holds three pickers. Landing on the first would make the
    // operator walk to the one they pressed.
    await waitFor(() => {
      expect(document.activeElement).toBe(picker("Trial"));
    });
  });

  it("offers the trial states under the words the rows carry", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await openFilters("Trial");
    fireEvent.click(picker("Trial"));

    // TRIAL_LABELS is the map the Trial cell renders its badge from, so a
    // filter that spelled a state its own way would fail here.
    const options = await screen.findAllByRole("option");
    expect(options.map((option) => option.textContent)).toEqual(
      TRIAL_STATES.map((state) => TRIAL_LABELS[state]),
    );
  });

  it("names every trial state at once rather than counting them", async () => {
    await renderRouteTree(routeTree, {
      initialPath: urlFor({ trial: [...TRIAL_STATES] }),
    });

    await waitFor(() => {
      expect(lastListParams().trial_states).toEqual([...TRIAL_STATES]);
    });
    // Every organization holds exactly one of these, so all of them is the
    // whole platform. "6 selected" reads as a narrowing that is not there.
    expect(filterTrigger("Trial").getAttribute("aria-label")).toBe(
      "Trial filter: All trial states",
    );
  });

  it("keeps the sort when a filter is applied", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: urlFor({ sort: "name", dir: "asc" }),
    });

    await openFilters("Status");
    await chooseFilter("Status", "Disabled");
    applyFilters();

    await waitFor(() => {
      expect(lastListParams().disabled_states).toEqual(["disabled"]);
    });
    const url = currentSearch(router);
    expect(url).toContain("sort=name");
    expect(url).toContain("dir=asc");
  });

  it("returns to the first page when the sheet applies the set already on", async () => {
    mocks.listOrganizations.mockResolvedValue({
      organizations: ORGS,
      next_cursor: "cursor_page_two",
    });
    await renderRouteTree(routeTree, {
      initialPath: urlFor({ disabled: ["disabled"] }),
    });

    const next = await screen.findByRole("button", { name: "Next" });
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });
    fireEvent.click(next);
    await waitFor(() => {
      expect(lastListParams().cursor).toBe("cursor_page_two");
    });

    // Nothing in the URL moves, so the pager cannot notice on its own. Page
    // three of a filter set is not the first page an operator asked for.
    await openFilters("Status");
    applyFilters();

    await waitFor(() => {
      expect(lastListParams().cursor).toBeUndefined();
    });
  });

  it("keeps an unrecognised type on offer after the operator unchecks it", async () => {
    await renderRouteTree(routeTree, {
      initialPath: urlFor({ type: ["startup"] }),
    });
    await waitFor(() => {
      expect(lastListParams().account_types).toEqual(["startup"]);
    });

    await openFilters("Type");
    // Unchecked, the value has nothing left to derive the option from. Taking
    // the options off the draft would drop it out of the list here, and the
    // operator could not change their mind without editing the URL by hand.
    await chooseFilter("Type", "startup");
    expect(
      (await screen.findByRole("option", { name: "startup" })).getAttribute(
        "aria-checked",
      ),
    ).toBe("false");

    await chooseFilter("Type", "startup");
    applyFilters();
    await waitFor(() => {
      expect(lastListParams().account_types).toEqual(["startup"]);
    });
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

    await openFilters("Status");
    await chooseFilter("Status", "Disabled");
    applyFilters();

    await waitFor(() => {
      expect(lastListParams().disabled_states).toEqual(["disabled"]);
    });
    // The cursor was minted by the previous filter set and points into a
    // different result set.
    expect(lastListParams().cursor).toBeUndefined();
  });

  it("drops the cursor when the search box changes the term", async () => {
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

    // The box writes to the URL itself, so nothing tells the pager. Only the
    // signature the render compares can catch this one.
    fireEvent.change(screen.getByLabelText("Search organizations"), {
      target: { value: "acme" },
    });

    await waitFor(
      () => {
        expect(lastListParams().q).toBe("acme");
      },
      { timeout: 2000 },
    );
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
      // The select column's header is a checkbox, so it carries no text.
      "",
      "Name",
      "Slug",
      "Type",
      "Members",
      "WorkOS",
      "Disabled",
      "Trial",
      "Created",
      "Actions",
    ]);
    expect(
      within(rowFor(link))
        .getAllByRole("cell")
        .map((cell) => cell.textContent),
    ).toEqual([
      // The select checkbox is named on the control, not in the cell.
      "",
      FIRST_ORG.name,
      FIRST_ORG.slug,
      FIRST_ORG.account_type,
      String(FIRST_ORG.member_count),
      // The truncation length is written out rather than imported. Reading the
      // column's own constant would move this expectation along with it.
      `${workosID.substring(0, 12)}...`,
      shortDate(disabledAt),
      // The factual state leads and its end date follows it.
      `Running ends ${shortDate(trialEndsAt)}`,
      shortDate(FIRST_ORG.created_at),
      // Both controls carry an icon and their names are on the buttons.
      "",
    ]);
  });

  it("reads a dash in the trial cell of an organization that never trialled", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

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
    // Past the two controls and the name link, so the click lands on the row
    // body rather than on something that handles it first. Named by its header
    // rather than counted, because a control column added beside the others
    // would otherwise slide this onto a cell that answers the click itself.
    const slugCell = cellUnder(rowFor(link), "Slug");

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
    await settle();
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
    ).toEqual(["", "Name", "Slug", "Type", "Actions"]);
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

    // On to the last row, and then nowhere: paging replaces the row nodes the
    // anchor depends on, so the walk stops rather than following the pager.
    fireEvent.keyDown(peekPanel(), { key: "ArrowDown" });
    expect(
      within(peekPanel()).getByRole("heading", { name: TRIALLING_ORG.name }),
    ).toBeTruthy();

    fireEvent.keyDown(peekPanel(), { key: "ArrowDown" });
    expect(
      within(peekPanel()).getByRole("heading", { name: TRIALLING_ORG.name }),
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
    ).toEqual(["", "Name", "Type", "Actions"]);

    fireEvent.keyDown(peekPanel(), { key: "Escape" });

    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual([
      "",
      "Name",
      "Type",
      "Members",
      "WorkOS",
      "Disabled",
      "Trial",
      "Created",
      "Actions",
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
    ).toEqual(["", "Name", "Slug", "Type", "Actions"]);
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
      // The select column's header is a checkbox, so it carries no text.
      "",
      "Name",
      "Slug",
      "Type",
      "Members",
      "WorkOS",
      "Disabled",
      "Trial",
      "Created",
      "Actions",
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

  it("peeks on an alt-click of the row rather than opening the organization", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    const link = await screen.findByRole("link", { name: FIRST_ORG.name });
    const slugCell = cellUnder(rowFor(link), "Slug");

    fireEvent.click(slugCell, { altKey: true });

    expect(
      within(peekPanel()).getByRole("heading", { name: FIRST_ORG.name }),
    ).toBeTruthy();
    // The half that is not changing, and the half a careless fix breaks: the
    // gesture peeks instead of navigating, not as well as navigating.
    await settle();
    expect(router.state.location.pathname).toBe("/organizations");
  });

  it("announces an alt-click peek the same way the control does", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const region = await screen.findByRole("status");
    const link = await screen.findByRole("link", { name: FIRST_ORG.name });

    fireEvent.click(cellUnder(rowFor(link), "Slug"), { altKey: true });

    // The same node and the same sentence. A second path into peek that
    // announced differently, or into a region of its own, would reach a
    // screen reader as a different feature.
    expect(liveRegion()).toBe(region);
    expect(announcement()).toBe(`Peeking at ${FIRST_ORG.name}.`);
  });

  it("closes the peek when the same row is alt-clicked again", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const link = await screen.findByRole("link", { name: FIRST_ORG.name });
    const slugCell = cellUnder(rowFor(link), "Slug");

    fireEvent.click(slugCell, { altKey: true });
    expect(peekPanel()).toBeTruthy();

    // The gesture toggles, the same way the control it stands in for does.
    fireEvent.click(cellUnder(rowFor(link), "Slug"), { altKey: true });

    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
  });

  it("opens the organization on a plain click of the same cell", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    const link = await screen.findByRole("link", { name: FIRST_ORG.name });
    fireEvent.click(cellUnder(rowFor(link), "Slug"));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe(
        `/organizations/${FIRST_ORG.slug}`,
      );
    });
    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
  });

  // Ctrl, Meta and Shift are the browser's open-in-tab and open-in-window
  // gestures on the link this row carries. A handler that peeked on any
  // modifier rather than on Alt alone would eat all three.
  for (const modifier of ["ctrlKey", "metaKey", "shiftKey"] as const) {
    it(`neither peeks nor navigates on a ${modifier} click of the row`, async () => {
      const { router } = await renderRouteTree(routeTree, {
        initialPath: "/organizations",
      });
      const link = await screen.findByRole("link", { name: FIRST_ORG.name });

      fireEvent.click(cellUnder(rowFor(link), "Slug"), { [modifier]: true });

      expect(
        screen.queryByRole("complementary", { name: "Organization peek" }),
      ).toBeNull();
      // Staying put is the whole gesture. A modified click that navigated in
      // this tab would take the list out from under the tab the operator
      // meant to open, and the peek assertion above passes either way.
      await settle();
      expect(router.state.location.pathname).toBe("/organizations");
    });
  }

  it("cancels the name link's download and peeks when it is alt-clicked", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });
    const link = await screen.findByRole("link", { name: FIRST_ORG.name });

    // Dispatched by hand, because the assertion is on the event rather than on
    // the DOM: a browser reads Alt on an anchor as "save link", so the row has
    // to cancel the anchor's own default or the gesture downloads the page.
    const event = createEvent.click(link, { altKey: true });
    fireEvent(link, event);

    expect(event.defaultPrevented).toBe(true);
    expect(
      within(peekPanel()).getByRole("heading", { name: FIRST_ORG.name }),
    ).toBeTruthy();
    await settle();
    expect(router.state.location.pathname).toBe("/organizations");
  });

  // Alt plus a second modifier is a gesture nobody aimed at this row, but the
  // anchor's default is still "save link". Cancelling has to survive the
  // narrower peek test, or the operator downloads an HTML file.
  for (const modifier of ["ctrlKey", "metaKey", "shiftKey"] as const) {
    it(`cancels the download without peeking on an alt+${modifier} click of the name`, async () => {
      const { router } = await renderRouteTree(routeTree, {
        initialPath: "/organizations",
      });
      const link = await screen.findByRole("link", { name: FIRST_ORG.name });

      const event = createEvent.click(link, { altKey: true, [modifier]: true });
      fireEvent(link, event);

      expect(event.defaultPrevented).toBe(true);
      expect(
        screen.queryByRole("complementary", { name: "Organization peek" }),
      ).toBeNull();
      await settle();
      expect(router.state.location.pathname).toBe("/organizations");
    });
  }

  it("opens the peek once when the control itself is alt-clicked", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const trigger = await peekTrigger(FIRST_ORG.name);

    // The control sits in the row the gesture is also bound to, so the click
    // reaches both. Only the control may answer it.
    fireEvent.click(trigger, { altKey: true });

    expect(
      within(peekPanel()).getByRole("heading", { name: FIRST_ORG.name }),
    ).toBeTruthy();
    // Raw, marker included, because the count is the only trace a second
    // answer leaves: both handlers read the same peeked id and both open the
    // same record, so the panel looks right and the region has spoken twice.
    // The zero-width space alternates to make a repeated sentence announceable
    // at all, and a gesture answered twice quietly spends that guarantee.
    expect(liveRegion().textContent).toBe(
      `Peeking at ${FIRST_ORG.name}.` + ZERO_WIDTH_SPACE,
    );
  });

  it("will not let the Columns menu hide the control", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    openOn(screen.getByRole("button", { name: "Columns" }));
    const item = screen.getByRole("menuitemcheckbox", { name: "Actions" });
    fireEvent.click(item);

    expect(item.getAttribute("aria-disabled")).toBe("true");
    expect(item.getAttribute("aria-checked")).toBe("true");

    // A locked item leaves the menu open, and an open Radix menu hides the
    // rest of the page from the accessibility tree.
    fireEvent.keyDown(item, { key: "Escape" });

    await waitFor(() => {
      expect(
        screen.getByRole("columnheader", { name: "Actions" }),
      ).toBeTruthy();
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

describe("organizations list write actions", () => {
  // The second row: live, so it is the one that offers Disable, and it is not
  // the row the detail mocks answer for.
  const LIVE = SECOND_ORG;

  async function openRowMenu(name: string): Promise<HTMLElement> {
    const trigger = await screen.findByRole("button", {
      name: `Actions for ${name}`,
    });
    openOn(trigger);
    return trigger;
  }

  async function confirmDisable(): Promise<void> {
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    await screen.findByRole("dialog");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Disable" }));
    });
  }

  it("offers Start trial on a live organization that never trialled", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await openRowMenu(LIVE.name);

    expect(screen.getByRole("menuitem", { name: "Start trial" })).toBeTruthy();
    expect(screen.queryByRole("menuitem", { name: "Extend trial" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Re-arm trial" })).toBeNull();
  });

  it("repaints the row out of the answer rather than asking for the list again", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const link = await screen.findByRole("link", { name: LIVE.name });
    expect(cellUnder(rowFor(link), "Disabled").textContent).toBe("-");

    await openRowMenu(LIVE.name);
    await confirmDisable();

    await waitFor(() => {
      expect(cellUnder(rowFor(link), "Disabled").textContent).toBe(
        shortDate(DISABLED_AT),
      );
    });
    // One request, the one that drew the page. The answer already carries the
    // record in its new state, so a refetch behind it is a second round trip
    // for a row that is already right, and the list flickers through the
    // loading state on the way.
    expect(mocks.listOrganizations).toHaveBeenCalledTimes(1);
    // The row that was written, and no other. A cache update keyed on the
    // wrong thing repaints every row with one record.
    const other = screen.getByRole("link", { name: FIRST_ORG.name });
    expect(cellUnder(rowFor(other), "Disabled").textContent).toBe(
      shortDate(FIRST_ORG.disabled_at ?? ""),
    );
  });

  // Unlike the row, which repaints from the answer the write already returned.
  it("asks for the platform totals again after a write", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: LIVE.name });
    const before = mocks.getOrganizationStats.mock.calls.length;
    expect(before).toBeGreaterThan(0);

    await openRowMenu(LIVE.name);
    await confirmDisable();

    await waitFor(() => {
      expect(mocks.getOrganizationStats.mock.calls.length).toBeGreaterThan(
        before,
      );
    });
  });

  // The write drops the read in flight before it goes out. A write that then
  // fails puts nothing in its place, and a first read holds no figures to fall
  // back on, so the strip would keep the dashes it started with.
  it("asks for the platform totals again when the write is refused", async () => {
    mocks.disableOrganization.mockRejectedValue(
      new GramAdminError(
        409,
        { name: "conflict", message: "organization is already disabled" },
        "gram admin 409 Conflict",
      ),
    );
    // Held open, so the cancel catches it before it has answered once.
    mocks.getOrganizationStats.mockReturnValueOnce(new Promise(() => {}));
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: LIVE.name });
    expect(mocks.getOrganizationStats.mock.calls.length).toBe(1);

    await openRowMenu(LIVE.name);
    await confirmDisable();

    await waitFor(() => {
      expect(mocks.getOrganizationStats.mock.calls.length).toBe(2);
    });
  });

  it("stays on the list while the operator works the menu it opened from a row", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    await openRowMenu(LIVE.name);
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    const dialog = await screen.findByRole("dialog");

    // The row opens the organization when it is clicked, and both of these are
    // drawn in a portal at the end of the document: nowhere near the row in the
    // DOM, and directly under it in the React tree the click travels up. The
    // operator reading a confirmation has not asked to leave the list, and
    // leaving would take the confirmation with it.
    fireEvent.click(within(dialog).getByText(`Disable ${LIVE.name}?`));
    const backdrop = document.querySelector('[data-slot="dialog-overlay"]');
    if (!backdrop) throw new Error("the dialog has no backdrop");
    fireEvent.click(backdrop);

    expect(router.state.location.pathname).toBe("/organizations");
    expect(mocks.disableOrganization).not.toHaveBeenCalled();
  });

  it("offers the opposite action after each write, twice running", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await openRowMenu(LIVE.name);
    await confirmDisable();

    // The row now carries a disabled record, so the menu has to have swapped
    // the two entries. Twice, because the state the second write leaves is the
    // state the first one started from: a menu that reads its answer from the
    // record the page loaded with, rather than from the row it is drawn on,
    // passes this once and fails on the way back.
    await openRowMenu(LIVE.name);
    expect(screen.queryByRole("menuitem", { name: "Disable" })).toBeNull();
    await act(async () => {
      fireEvent.click(screen.getByRole("menuitem", { name: "Re-enable" }));
    });

    await openRowMenu(LIVE.name);
    expect(screen.queryByRole("menuitem", { name: "Re-enable" })).toBeNull();
    await confirmDisable();

    expect(mocks.disableOrganization).toHaveBeenCalledTimes(2);
    expect(mocks.enableOrganization).toHaveBeenCalledTimes(1);
  });

  it("leaves the row as it was when the server refuses the write", async () => {
    mocks.disableOrganization.mockRejectedValue(
      new GramAdminError(
        409,
        { name: "conflict", message: "organization is already disabled" },
        "gram admin 409 Conflict",
      ),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: LIVE.name });

    await openRowMenu(LIVE.name);
    await confirmDisable();

    // The dialog is still up, holding the reason, and the operator is the one
    // who decides when to leave it.
    expect(screen.getByRole("dialog")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    // The row is drawn from the answer, and there was no answer. A row that
    // repaints off the request instead shows the operator a state the server
    // never reached, and the list is the only place they would notice.
    const link = screen.getByRole("link", { name: LIVE.name });
    expect(cellUnder(rowFor(link), "Disabled").textContent).toBe("-");
    expect(announcement()).toBe(
      `Could not disable ${LIVE.name}: organization is already disabled`,
    );
  });

  it("reports the write through the one region the list already speaks from", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await openRowMenu(LIVE.name);
    await confirmDisable();

    // The same polite region the peek announces through, rather than a second
    // one shipped with the row menu. A row menu is not a surface an operator
    // can read a result off: the dialog closes and the cell that changed is
    // eight columns away.
    await waitFor(() => {
      expect(announcement()).toBe(`${LIVE.name} is disabled.`);
    });
  });

  it("shows a re-enable that failed, and not only to a screen reader", async () => {
    mocks.enableOrganization.mockRejectedValue(
      new GramAdminError(
        409,
        { name: "conflict", message: "organization is not disabled" },
        "gram admin 409 Conflict",
      ),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await openRowMenu(FIRST_ORG.name);
    await act(async () => {
      fireEvent.click(screen.getByRole("menuitem", { name: "Re-enable" }));
    });

    // Re-enable is the one action with no dialog, so without this the whole
    // account of the failure on the page is a sentence inside sr-only: the row
    // does not change, and the banner above it covers the list query alone. A
    // sighted operator presses Re-enable, sees nothing happen, and is told
    // nothing about why.
    const failure = `Could not re-enable ${FIRST_ORG.name}: organization is not disabled`;
    const banner = await screen.findByRole("alert");
    expect(banner.textContent).toContain(failure);
    expect(banner.className).not.toContain("sr-only");
    expect(announcement()).toBe(failure);

    // Dismissible, because it is the operator's page and this is not a modal.
    fireEvent.click(
      screen.getByRole("button", { name: "Dismiss the failure" }),
    );
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("takes the failure down when the next write succeeds", async () => {
    mocks.enableOrganization.mockRejectedValueOnce(
      new GramAdminError(404, null, "gram admin 404 Not Found"),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await openRowMenu(FIRST_ORG.name);
    await act(async () => {
      fireEvent.click(screen.getByRole("menuitem", { name: "Re-enable" }));
    });
    expect(await screen.findByRole("alert")).toBeTruthy();

    await openRowMenu(FIRST_ORG.name);
    await act(async () => {
      fireEvent.click(screen.getByRole("menuitem", { name: "Re-enable" }));
    });

    // The operator has just been told the current state of this record, so a
    // banner about the attempt before it is describing a page that has moved
    // on.
    await waitFor(() => {
      expect(screen.queryByRole("alert")).toBeNull();
    });
  });

  it("keeps the live region reachable while a dialog is open", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await openRowMenu(LIVE.name);
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    await screen.findByRole("dialog");

    // An open Radix modal hides the rest of the page with aria-hidden, and the
    // one exemption that package makes is for elements carrying aria-live by
    // name. Everything else goes, which is what this asserts against: the
    // table is gone from the tree and the region is not.
    expect(liveRegion()).toBeTruthy();
    expect(screen.queryByRole("link", { name: LIVE.name })).toBeNull();
  });

  it("speaks the same refusal twice when the operator presses through it", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await openRowMenu(TRIALLING_ORG.name);
    fireEvent.click(screen.getByRole("menuitem", { name: "Extend trial" }));
    await screen.findByRole("dialog");

    // The one refusal the calendar's own bounds cannot prevent: pressing the
    // selected day again clears the selection.
    fireEvent.click(screen.getByLabelText("Ends on"));
    await screen.findByRole("grid");
    const selected = document.querySelector("td[data-selected='true'] button");
    if (!(selected instanceof HTMLButtonElement)) {
      throw new Error("the calendar opened with no day selected");
    }
    fireEvent.click(selected);
    await waitFor(() => {
      expect(screen.queryByRole("grid")).toBeNull();
    });

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Extend" }));
    });
    const first = liveRegion().textContent;
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Extend" }));
    });

    // Word for word the same sentence, and the region still changed: the
    // zero-width space alternates, so the second refusal reaches the
    // accessibility tree as a change rather than as silence. Nothing on screen
    // moves on that press, which is the whole reason this path announces.
    expect(announcement()).toContain("Pick a date between");
    expect(liveRegion().textContent).not.toBe(first);
    expect(mocks.extendTrial).not.toHaveBeenCalled();
  });

  it("will not let the Columns menu take the row menu away", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    openOn(screen.getByRole("button", { name: "Columns" }));
    const item = screen.getByRole("menuitemcheckbox", { name: "Actions" });
    fireEvent.click(item);

    // Hiding this column puts disable, re-enable and extend out of reach for
    // every row at once, and the operator has no route back except the column
    // they just hid.
    expect(item.getAttribute("aria-disabled")).toBe("true");
    expect(item.getAttribute("aria-checked")).toBe("true");

    fireEvent.keyDown(item, { key: "Escape" });

    await waitFor(() => {
      expect(
        screen.getByRole("columnheader", { name: "Actions" }),
      ).toBeTruthy();
    });
    expect(
      await screen.findByRole("button", { name: `Actions for ${LIVE.name}` }),
    ).toBeTruthy();
  });

  it("keeps the actions column while the peek is open", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);

    // Peek takes five columns down to make room for the panel. This one stays:
    // the panel covers one record, and the reason to reach for another row's
    // menu does not go away because a panel is open beside it.
    expect(screen.getByRole("columnheader", { name: "Actions" })).toBeTruthy();
    expect(
      screen.getByRole("button", { name: `Actions for ${LIVE.name}` }),
    ).toBeTruthy();
  });

  it("extends the trial from the panel and repaints the panel with the answer", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(TRIALLING_ORG.name);

    fireEvent.click(
      within(peekPanel()).getByRole("button", {
        name: `Extend trial for ${TRIALLING_ORG.name}`,
      }),
    );
    await screen.findByRole("dialog");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Extend" }));
    });

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    // The default day count, sent without the operator typing anything.
    expect(mocks.extendTrial).toHaveBeenCalledWith({
      id: TRIALLING_ORG.id,
      days: 14,
    });
    // The panel is drawn from the row it is peeking at, so it repaints from the
    // same cache write the row does. Reading the old date here is the operator
    // being shown the trial they just extended, unextended.
    expect(peekPanel().textContent).toContain(shortDate(EXTENDED_TRIAL_END));
    expect(announcement()).toBe(
      `${TRIALLING_ORG.name} trial extended by 14 days.`,
    );
  });

  it("re-enables from the panel without a dialog in the way", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);
    const panel = within(peekPanel());

    await act(async () => {
      fireEvent.click(
        panel.getByRole("button", { name: `Re-enable ${FIRST_ORG.name}` }),
      );
    });

    expect(mocks.enableOrganization).toHaveBeenCalledWith({
      id: FIRST_ORG.id,
    });
    // Named for the record it acts on, and it is the record that changed
    // rather than the panel: the panel's own name is a constant, and it swaps
    // records under itself on the arrow keys.
    expect(
      within(peekPanel()).getByRole("button", {
        name: `Disable ${FIRST_ORG.name}`,
      }),
    ).toBeTruthy();
    // The panel stays. Nothing about the record left the list, and closing it
    // would take the operator off the row they are working.
    expect(peekPanel()).toBeTruthy();
  });

  it("gives the keyboard back to the panel control the dialog opened from", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(TRIALLING_ORG.name);
    const extend = within(peekPanel()).getByRole("button", {
      name: `Extend trial for ${TRIALLING_ORG.name}`,
    });

    fireEvent.click(extend);
    await screen.findByRole("dialog");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Extend" }));
    });

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    // The same node, through the write: the record changed underneath it and
    // React kept the element. Radix would have focused a DialogTrigger that
    // does not exist here and left the keyboard on the document body, at the
    // top of the page, with the panel still open beside it.
    await waitFor(() => {
      expect(document.activeElement).toBe(extend);
    });
  });

  it("leaves the arrow keys to the day count typed inside the peek", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const peeked = await screen.findByRole("link", {
      name: TRIALLING_ORG.name,
    });
    await peekOn(TRIALLING_ORG.name);

    fireEvent.click(
      within(peekPanel()).getByRole("button", {
        name: `Extend trial for ${TRIALLING_ORG.name}`,
      }),
    );
    await screen.findByRole("dialog");

    // The control the arrow keys mean something to: it opens a calendar that
    // walks itself day by day. Dispatched from the trigger rather than from the
    // grid, so nothing but the list's own handler can claim the key.
    const endsOn = screen.getByLabelText("Ends on");

    // The dialog is drawn in a portal and rendered inside the panel's subtree,
    // so its keys reach the list's handler through React even though the node
    // is outside the panel. An operator walking the calendar a week down is not
    // asking the list to walk the peek down the page underneath them.
    const event = new KeyboardEvent("keydown", {
      key: "ArrowDown",
      bubbles: true,
      cancelable: true,
    });
    endsOn.dispatchEvent(event);

    // Read off the row rather than through a role query: an open modal takes
    // the table out of the accessibility tree.
    expect(isPeeked(peeked)).toBe(true);
    expect(event.defaultPrevented).toBe(false);
  });

  it("closes the dialog on Escape and leaves the peek open behind it", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(TRIALLING_ORG.name);

    fireEvent.click(
      within(peekPanel()).getByRole("button", {
        name: `Extend trial for ${TRIALLING_ORG.name}`,
      }),
    );
    await screen.findByRole("dialog");

    fireEvent.keyDown(screen.getByLabelText("Ends on"), { key: "Escape" });

    // One surface per press. Escape inside the panel body closes the peek, and
    // this key is inside the panel's React subtree, so two separate things
    // keep the panel: the dialog answers the key first and marks it, and the
    // panel reads containment off the DOM, where the portal is nowhere near
    // it. Losing either one alone is survivable; losing both closes the panel
    // out from under a dialog the operator was reading.
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(peekPanel()).toBeTruthy();
    expect(mocks.extendTrial).not.toHaveBeenCalled();
  });
});

describe("organizations list actions column", () => {
  function headers(): string[] {
    return screen
      .getAllByRole("columnheader")
      .map((header) => header.textContent ?? "");
  }

  function actionsCell(name: string): HTMLElement {
    return cellUnder(rowFor(screen.getByRole("link", { name })), "Actions");
  }

  function menuTrigger(name: string): HTMLElement {
    return screen.getByRole("button", { name: `Actions for ${name}` });
  }

  it("puts the column last and leaves it there when peek opens", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    expect(headers().at(-1)).toBe("Actions");

    await peekOn(FIRST_ORG.name);

    // Peek takes five columns down. The controls are the ones the operator is
    // reaching for at that moment, so they have to be where they were.
    expect(headers().at(-1)).toBe("Actions");
    expect(headers()).not.toContain("Created");
  });

  it("marks the actions column sticky at the right edge, above its neighbours and no wider than its contents", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    // Both halves of the column, because a pin on the header alone leaves the
    // cells sliding under a header that stayed put. happy-dom lays nothing
    // out, so these read the classes that carry the pin; the measurement that
    // proves the column holds its x is in the browser.
    for (const element of [
      screen.getByRole("columnheader", { name: "Actions" }),
      actionsCell(FIRST_ORG.name),
    ]) {
      expect(element.classList.contains("sticky")).toBe(true);
      expect(element.classList.contains("right-0")).toBe(true);
      // Above the cells that scroll under it, below the sticky header row.
      expect(element.classList.contains("z-1")).toBe(true);
      // The table is `w-full`, so a column that did not shrink to its contents
      // would take a share of the freed width and read as an empty gutter.
      expect(element.classList.contains("w-px")).toBe(true);
    }
  });

  it("gives the pinned cells opaque colours rather than a transparent one", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const link = await screen.findByRole("link", { name: FIRST_ORG.name });

    // The header's own colour, not the header row's: the pinned cell paints
    // above its neighbours, so it needs one of its own to cover their text.
    expect(
      screen
        .getByRole("columnheader", { name: "Actions" })
        .classList.contains("bg-muted"),
    ).toBe(true);

    // The body cell takes the row's colour instead of a flat one, so it does
    // not read as a stripe over the peeked row.
    expect(actionsCell(FIRST_ORG.name).classList.contains("bg-inherit")).toBe(
      true,
    );
    // Which is only opaque if the row it inherits from carries a colour.
    expect(rowFor(link).classList.contains("bg-background")).toBe(true);
  });

  it("keeps the row's hover and expanded colours free of an alpha", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const link = await screen.findByRole("link", { name: FIRST_ORG.name });
    const classes = [...rowFor(link).classList];

    // The base row tints these two states at half alpha. Inherited, that alpha
    // is painted twice and the scrolled row shows through the pinned cell, so
    // each has to survive as its opaque form and neither may be left behind.
    for (const state of ["hover", "has-aria-expanded"]) {
      expect(classes).toContain(`${state}:bg-muted`);
      expect(classes).not.toContain(`${state}:bg-muted/50`);
    }
  });

  it("leaves the peeked row one colour for the pinned cell to inherit", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const link = await screen.findByRole("link", { name: FIRST_ORG.name });
    await peekOn(FIRST_ORG.name);

    // One colour on the row, not two. Both classes present would leave the
    // stylesheet's order to decide which the pinned cell inherits.
    expect(rowFor(link).classList.contains("bg-muted")).toBe(true);
    expect(rowFor(link).classList.contains("bg-background")).toBe(false);
  });

  it("holds both controls in the one cell, the peek trigger first", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    // Peek is the read action and the more frequent one; the menu holds the
    // writes. Read by accessible name, which is what a screen reader
    // announces and what an operator hears in this order.
    expect(
      within(actionsCell(FIRST_ORG.name))
        .getAllByRole("button")
        .map((control) => control.getAttribute("aria-label")),
    ).toEqual([`Peek at ${FIRST_ORG.name}`, `Actions for ${FIRST_ORG.name}`]);
  });

  it("keeps both controls on the keyboard, peek before the menu", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    // Adjacent stops in the sequential order Tab walks, which is also the
    // assertion that neither one dropped out of it.
    expect(tabStopBefore(menuTrigger(FIRST_ORG.name))).toBe(
      await peekTrigger(FIRST_ORG.name),
    );
  });

  it("keeps both controls reachable while the peek is open", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(FIRST_ORG.name);

    // The panel covers one record. The reason to reach another row's controls
    // does not go away because it is open beside them.
    expect(tabStopBefore(menuTrigger(SECOND_ORG.name))).toBe(
      await peekTrigger(SECOND_ORG.name),
    );
    expect(
      within(actionsCell(SECOND_ORG.name)).getAllByRole("button"),
    ).toHaveLength(2);
  });
});

// The bar takes a table, so the last-column case is reachable here by handing
// it a two column table rather than by clicking seven items shut through a
// menu that closes each time.
describe("organizations list bulk account type", () => {
  // Every id the operator can act on comes from a ticked row, so the tests
  // reach the rows the way an operator does: through the checkbox named for
  // the organization it is on. There is no field anywhere here that takes an
  // id, and there must not be one: the write matches an id case-sensitively
  // and the search matches case-insensitively, so a typed id can name a row
  // that is on screen and still come back missing.
  function selectAll(): HTMLElement {
    return screen.getByRole("checkbox", {
      name: "Select every organization on this page",
    });
  }

  function rowCheckbox(name: string): HTMLElement {
    return screen.getByRole("checkbox", { name: `Select ${name}` });
  }

  async function tick(name: string): Promise<void> {
    fireEvent.click(
      await screen.findByRole("checkbox", {
        name: `Select ${name}`,
      }),
    );
  }

  function bulkTrigger(): HTMLElement {
    return screen.getByRole("button", { name: "Set account type" });
  }

  // The two halves of the action, kept apart on purpose: picking a type opens
  // the confirmation and writes nothing, and confirming is what writes.
  function pick(type: string): void {
    openOn(bulkTrigger());
    fireEvent.click(screen.getByRole("menuitem", { name: type }));
  }

  async function confirm(type: string): Promise<void> {
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: `Set to ${type}` }));
    });
  }

  function dialogTitle(): string {
    return (
      within(screen.getByRole("dialog")).getByRole("heading").textContent ?? ""
    );
  }

  function lastBulkRequest(): BulkUpdateAccountTypeRequest {
    const call = mocks.bulkUpdateAccountType.mock.calls.at(-1);
    if (!call?.[0]) throw new Error("nothing was sent to the bulk endpoint");
    return call[0];
  }

  it("says nothing is selected until a row is ticked", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    expect(screen.getByText("Nothing selected")).toBeTruthy();
    // The action appears with the selection. Offering it against nothing would
    // be offering a request the server refuses: the payload takes one id at
    // least.
    expect(
      screen.queryByRole("button", { name: "Set account type" }),
    ).toBeNull();
  });

  it("counts the rows the operator ticked", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    expect(screen.getByText("1 selected")).toBeTruthy();

    await tick(SECOND_ORG.name);
    expect(screen.getByText("2 selected")).toBeTruthy();
    expect(screen.queryByText("Nothing selected")).toBeNull();
  });

  // The strip swaps its contents in place. happy-dom performs no layout, so
  // nothing here can prove the strip keeps its height; what it can prove is
  // that the control the height is set by is in both states. The geometry is
  // checked in a browser.
  it("keeps the Columns control in the strip in both states", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });
    const bar = screen.getByText("Nothing selected").parentElement;
    if (!bar) throw new Error("the strip has no element");
    expect(within(bar).getByRole("button", { name: "Columns" })).toBeTruthy();

    await tick(FIRST_ORG.name);

    const selectedBar = screen.getByText("1 selected").parentElement;
    expect(selectedBar).toBe(bar);
    expect(within(bar).getByRole("button", { name: "Columns" })).toBeTruthy();
  });

  it("ticks every row on the page from the header checkbox", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    fireEvent.click(selectAll());

    expect(screen.getByText(`${ORGS.length} selected`)).toBeTruthy();
    for (const org of ORGS) {
      expect(rowCheckbox(org.name).getAttribute("aria-checked")).toBe("true");
    }
  });

  it("unticks every row on the page from the header checkbox", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    fireEvent.click(selectAll());
    fireEvent.click(selectAll());

    expect(screen.getByText("Nothing selected")).toBeTruthy();
    expect(rowCheckbox(FIRST_ORG.name).getAttribute("aria-checked")).toBe(
      "false",
    );
  });

  it("reports the header checkbox as mixed while only some rows are ticked", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);

    // Not "false". The next press clears rather than extends, and an unchecked
    // box states the opposite to whoever cannot see the count beside it.
    expect(selectAll().getAttribute("aria-checked")).toBe("mixed");
  });

  it("leaves the list where it is when a checkbox is ticked", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    // The checkbox sits inside a row that opens the organization when it is
    // clicked. A row handler that answered this one would take the operator
    // off the list at the moment they started building a selection.
    await tick(FIRST_ORG.name);

    expect(router.state.location.pathname).toBe("/organizations");
    expect(screen.getByText("1 selected")).toBeTruthy();
  });

  it("puts the row's checkbox ahead of everything else in it", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    const link = await screen.findByRole("link", { name: FIRST_ORG.name });

    // The select column is leftmost and the actions column is pinned to the
    // right edge, so the keyboard has to walk the row the way the eye does:
    // checkbox, then the record, then what can be done to it. happy-dom lays
    // nothing out, so this is the order and not the placement.
    expect(tabStopBefore(link)).toBe(rowCheckbox(FIRST_ORG.name));
    expect(tabStopBefore(await peekTrigger(FIRST_ORG.name))).not.toBe(
      rowCheckbox(FIRST_ORG.name),
    );
  });

  it("pins the select column to the left edge, in both the header and the rows", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    // The list is wider than the window at every width an operator uses, so an
    // unpinned checkbox scrolls off the left while the actions column holds the
    // right. A table whose purpose is picking rows cannot hide the control that
    // picks them. happy-dom lays nothing out, so these read the classes that
    // carry the pin; the measurement is in the browser.
    const head = screen
      .getByRole("checkbox", {
        name: "Select every organization on this page",
      })
      .closest("th");
    const cell = rowCheckbox(FIRST_ORG.name).closest("td");

    for (const pinned of [head, cell]) {
      expect(pinned?.classList.contains("sticky")).toBe(true);
      expect(pinned?.classList.contains("left-0")).toBe(true);
      expect(pinned?.classList.contains("z-1")).toBe(true);
      // The table is `w-full`, so a column that did not shrink to the checkbox
      // would take a share of the freed width and read as an empty gutter.
      expect(pinned?.classList.contains("w-px")).toBe(true);
    }

    // Each element's own colour, and not the other's. The header's grey painted
    // down the column would cover the rows sliding under it and stop the hover
    // and peek highlight dead at the checkbox.
    expect(head?.classList.contains("bg-muted")).toBe(true);
    expect(cell?.classList.contains("bg-inherit")).toBe(true);
    expect(cell?.classList.contains("bg-muted")).toBe(false);
  });

  it("leaves the row's own controls alone when a checkbox is ticked", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    await tick(FIRST_ORG.name);

    // The actions column carries peek and the row menu in one pinned cell. A
    // checkbox press that reached either would open a panel or a menu over the
    // list the operator is building a selection in.
    expect(
      screen.queryByRole("complementary", { name: "Organization peek" }),
    ).toBeNull();
    expect(screen.queryByRole("menu")).toBeNull();
    expect(screen.getByText("1 selected")).toBeTruthy();
  });

  it("clears the selection when the operator pages", async () => {
    mocks.listOrganizations.mockImplementation((params) =>
      Promise.resolve(
        params?.cursor
          ? { organizations: [NEXT_PAGE_ORG] }
          : { organizations: ORGS, next_cursor: "cursor_page_two" },
      ),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    const next = await screen.findByRole("button", { name: "Next" });
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });
    fireEvent.click(next);
    await screen.findByRole("link", { name: NEXT_PAGE_ORG.name });

    // A selection that survives a page is a selection the operator cannot see.
    // The count would still read 1 and the row it names would be off screen.
    expect(screen.getByText("Nothing selected")).toBeTruthy();
  });

  it("clears the selection when a platform total opens the rows behind it", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    fireEvent.click(screen.getByRole("button", { name: /^Disabled/ }));

    // A stat cell replaces the filters, so the rows under the selection are a
    // different set from the one the operator ticked. It reaches the selection
    // through the search in the URL, the same way the filter sheet does.
    await waitFor(() => {
      expect(screen.getByText("Nothing selected")).toBeTruthy();
    });
    expect(
      screen.queryByRole("button", { name: "Set account type" }),
    ).toBeNull();

    // The strip is a sibling above the bar the bulk control lives in, so a
    // selection swaps that bar's contents and leaves the totals alone. Document
    // order, not layout: happy-dom lays nothing out.
    const strip = screen.getByRole("group", { name: "Platform totals" });
    await tick(FIRST_ORG.name);
    expect(screen.getByRole("group", { name: "Platform totals" })).toBe(strip);
    expect(
      strip.compareDocumentPosition(bulkTrigger()) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("does not bring the selection back when the operator pages back", async () => {
    mocks.listOrganizations.mockImplementation((params) =>
      Promise.resolve(
        params?.cursor
          ? { organizations: [NEXT_PAGE_ORG] }
          : { organizations: ORGS, next_cursor: "cursor_page_two" },
      ),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    const next = await screen.findByRole("button", { name: "Next" });
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });
    fireEvent.click(next);
    await screen.findByRole("link", { name: NEXT_PAGE_ORG.name });

    fireEvent.click(screen.getByRole("button", { name: "Previous" }));
    await screen.findByRole("link", { name: FIRST_ORG.name });

    // The page the selection was made on is back, so a selection that was only
    // hidden by the page change rather than dropped shows up again here. The
    // pager is component state and stays out of the URL, which is why watching
    // the URL alone is not enough.
    expect(screen.getByText("Nothing selected")).toBeTruthy();
    expect(rowCheckbox(FIRST_ORG.name).getAttribute("aria-checked")).toBe(
      "false",
    );
  });

  it("clears the selection when a filter is applied", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);

    await openFilters("Status");
    await chooseFilter("Status", "Disabled");
    applyFilters();

    await waitFor(() => {
      expect(lastListParams().disabled_states).toEqual(["disabled"]);
    });
    expect(screen.getByText("Nothing selected")).toBeTruthy();
  });

  it("clears the selection when the sort changes", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    await tick(FIRST_ORG.name);

    // The sort is in the URL and not in the list request, so a page that
    // watched only the request would keep a selection across a reorder.
    await act(async () => {
      await router.navigate({
        to: "/organizations",
        search: { sort: "name", dir: "asc" },
      });
    });

    expect(screen.getByText("Nothing selected")).toBeTruthy();
  });

  it("clears the selection when the search term changes", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    await tick(FIRST_ORG.name);

    await act(async () => {
      await router.navigate({ to: "/organizations", search: { q: "acme" } });
    });

    expect(screen.getByText("Nothing selected")).toBeTruthy();
  });

  it("unticks one row and keeps the rest", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    fireEvent.click(selectAll());
    await tick(SECOND_ORG.name);

    // The second press on a ticked row clears that row, so a checkbox that
    // only ever ticked would leave the operator no way back short of clearing
    // the whole selection.
    expect(screen.getByText(`${ORGS.length - 1} selected`)).toBeTruthy();
    expect(rowCheckbox(SECOND_ORG.name).getAttribute("aria-checked")).toBe(
      "false",
    );
  });

  it("drops the selection when the operator clears it by hand", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    fireEvent.click(screen.getByRole("button", { name: "Clear selection" }));

    expect(screen.getByText("Nothing selected")).toBeTruthy();
    expect(mocks.bulkUpdateAccountType).not.toHaveBeenCalled();
  });

  it("writes nothing until the operator confirms", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    await tick(SECOND_ORG.name);
    pick("enterprise");

    // The whole point of the confirmation. A control that wrote on the pick
    // would have changed both records by now, and the dialog below would be a
    // decoration over a write that had already happened.
    await screen.findByRole("dialog");
    expect(mocks.bulkUpdateAccountType).not.toHaveBeenCalled();
  });

  it("names the count and the target type in the confirmation", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    await tick(SECOND_ORG.name);
    pick("enterprise");
    await screen.findByRole("dialog");

    // The count is the thing an operator gets wrong, and the type is the thing
    // they picked. Both are read back before anything is written.
    expect(dialogTitle()).toBe("Set 2 organizations to enterprise?");
  });

  it("counts one organization in the singular", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    pick("free");
    await screen.findByRole("dialog");

    expect(dialogTitle()).toBe("Set 1 organization to free?");
  });

  it("sends the ticked ids and the picked type, and nothing else", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    await tick(TRIALLING_ORG.name);
    pick("pro");
    await screen.findByRole("dialog");
    await confirm("pro");

    // Two of the three rows on the page, so a request built from the rows
    // rather than from the selection fails here. The ids are compared as a set
    // for the same reason the answer is read as one.
    const sent = lastBulkRequest();
    expect([...sent.ids].sort()).toEqual(
      [FIRST_ORG.id, TRIALLING_ORG.id].sort(),
    );
    expect(sent.account_type).toBe("pro");
  });

  it("writes nothing when the operator cancels", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    pick("enterprise");
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(mocks.bulkUpdateAccountType).not.toHaveBeenCalled();
    // The selection is what the operator built. Cancelling the write is not
    // asking to build it again.
    expect(screen.getByText("1 selected")).toBeTruthy();
  });

  it("clears the selection once the write lands", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    await tick(SECOND_ORG.name);
    pick("enterprise");
    await screen.findByRole("dialog");
    await confirm("enterprise");

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    // A selection that outlives the write it was made for is a second write
    // one press away, against rows that already carry the type.
    expect(screen.getByText("Nothing selected")).toBeTruthy();
    expect(announcement()).toBe("2 organizations set to enterprise.");
  });

  it("asks for the list again once the write lands", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });
    const before = mocks.listOrganizations.mock.calls.length;

    await tick(FIRST_ORG.name);
    pick("enterprise");
    await screen.findByRole("dialog");
    await confirm("enterprise");

    // The answer carries ids, not records, so there is nothing to repaint the
    // rows from. Without the invalidation the table keeps showing the type the
    // operator just changed.
    await waitFor(() => {
      expect(mocks.listOrganizations.mock.calls.length).toBeGreaterThan(before);
    });
  });

  it("names the organizations the server could not find", async () => {
    mocks.bulkUpdateAccountType.mockImplementation(({ ids }) =>
      Promise.resolve({
        updated_ids: ids.filter((id) => id !== SECOND_ORG.id),
        missing_ids: [SECOND_ORG.id],
      }),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    await tick(SECOND_ORG.name);
    pick("enterprise");
    await screen.findByRole("dialog");
    await confirm("enterprise");

    // Shown as well as spoken. A bulk write that quietly did less than it said
    // is worse than one that failed, and the count comes off the answer rather
    // than off what was asked for.
    const banner = await screen.findByRole("alert");
    expect(banner.textContent).toContain("1 organization set to enterprise.");
    // The verb has to agree at one as well as at many, and the noun is already
    // counted, so the sentence cannot branch on the count twice.
    expect(banner.textContent).toContain(
      `1 organization matched nothing and stayed unchanged: ${SECOND_ORG.name}.`,
    );
    expect(announcement()).toBe(banner.textContent?.replace("Dismiss", ""));
  });

  it("names every organization the server could not find, not just the first", async () => {
    mocks.bulkUpdateAccountType.mockImplementation(() =>
      Promise.resolve({
        updated_ids: [],
        missing_ids: [SECOND_ORG.id, FIRST_ORG.id],
      }),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    await tick(SECOND_ORG.name);
    pick("enterprise");
    await screen.findByRole("dialog");
    await confirm("enterprise");

    // The same sentence at two, so the wording cannot be fixed at one count by
    // breaking it at the other. The order is the answer's, not the selection's.
    const banner = await screen.findByRole("alert");
    expect(banner.textContent).toContain("0 organizations set to enterprise.");
    expect(banner.textContent).toContain(
      `2 organizations matched nothing and stayed unchanged: ${SECOND_ORG.name}, ${FIRST_ORG.name}.`,
    );
  });

  it("reports nothing missing when every id landed", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    pick("free");
    await screen.findByRole("dialog");
    await confirm("free");

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    // No banner at all: nothing went missing, and a banner that always shows
    // teaches the operator to ignore the one that matters.
    expect(screen.queryByRole("alert")).toBeNull();
    expect(announcement()).toBe("1 organization set to free.");
  });

  it("keeps the dialog open holding the reason when the server refuses", async () => {
    mocks.bulkUpdateAccountType.mockRejectedValue(
      new GramAdminError(
        400,
        { name: "invalid", message: "account_type is not allowed" },
        "gram admin 400 Bad Request",
      ),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    pick("enterprise");
    await screen.findByRole("dialog");
    await confirm("enterprise");

    // As an alert, not merely as text: the modal takes the page's live region
    // out of the accessibility tree, so this role is the only thing that speaks
    // the refusal to an operator who cannot see it.
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByRole("alert").textContent).toBe(
      "account_type is not allowed",
    );
    // Nothing was written, so the selection the operator would retry with is
    // still there.
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(screen.getByText("1 selected")).toBeTruthy();
  });

  it("does not carry a refused write's reason into the next one", async () => {
    mocks.bulkUpdateAccountType.mockRejectedValue(
      new GramAdminError(
        400,
        { name: "invalid", message: "account_type is not allowed" },
        "gram admin 400 Bad Request",
      ),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    pick("enterprise");
    await screen.findByRole("dialog");
    await confirm("enterprise");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    pick("free");

    // The reason belongs to the write that was refused. A dialog that opened
    // holding it would be telling the operator their next write had already
    // failed.
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByRole("alert")).toBeNull();
  });

  it("gives the keyboard back to the control the dialog opened from", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    pick("enterprise");
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(document.activeElement).toBe(bulkTrigger());
  });

  it("puts the keyboard on the list when the write takes that control away", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    pick("enterprise");
    await screen.findByRole("dialog");
    await confirm("enterprise");
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    // A landed write clears the selection, which takes the trigger the dialog
    // opened from off the page. Dropped on the body instead, the next Tab
    // restarts at the top of the document, nowhere near the rows just written.
    expect(
      screen.queryByRole("button", { name: "Set account type" }),
    ).toBeNull();
    expect(document.activeElement).toBe(
      screen.getByRole("region", { name: "Organizations table" }),
    );
  });

  it("shuts the dialog's own controls while the write is in flight", async () => {
    let land = (): void => {};
    mocks.bulkUpdateAccountType.mockImplementation(
      () =>
        new Promise((resolve) => {
          land = () =>
            resolve({ updated_ids: [FIRST_ORG.id], missing_ids: [] });
        }),
    );
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    pick("enterprise");
    await screen.findByRole("dialog");
    // Held open on purpose: every other test resolves the write inside the same
    // act, so nothing else ever observes this state.
    await confirm("enterprise");

    const dialog = screen.getByRole("dialog");
    const setting = await within(dialog).findByRole("button", {
      name: "Setting...",
    });
    expect(setting.hasAttribute("disabled")).toBe(true);
    // Cancel too: a write already sent cannot be called back by closing the
    // dialog it was sent from.
    expect(
      within(dialog)
        .getByRole("button", { name: "Cancel" })
        .hasAttribute("disabled"),
    ).toBe(true);
    // The close control goes rather than sitting there live beside a greyed
    // Cancel, doing nothing when it is pressed.
    expect(within(dialog).queryByRole("button", { name: "Close" })).toBeNull();

    // A press and an Escape a macrotask later, which is as fast as an operator
    // can be. Neither may reach the endpoint or take the dialog down.
    await act(async () => {
      await Promise.resolve();
    });
    fireEvent.click(setting);
    fireEvent.keyDown(dialog, { key: "Escape" });
    expect(mocks.bulkUpdateAccountType.mock.calls).toHaveLength(1);
    expect(screen.getByRole("dialog")).toBe(dialog);

    await act(async () => {
      land();
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("lets the operator pick a different type after cancelling", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    pick("enterprise");
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    pick("free");
    await screen.findByRole("dialog");

    // The dialog reads the type off the pick that opened it, not off the first
    // one the operator ever made.
    expect(dialogTitle()).toBe("Set 1 organization to free?");
    await confirm("free");
    expect(lastBulkRequest().account_type).toBe("free");
  });

  it("offers every account type the server takes and no other", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    await tick(FIRST_ORG.name);
    openOn(bulkTrigger());

    // ACCOUNT_TYPE_OPTIONS mirrors constants.AccountTypes, which is the enum
    // the payload declares. An option outside it is a request the generated
    // decoder refuses before the handler ever sees it.
    expect(
      screen.getAllByRole("menuitem").map((item) => item.textContent),
    ).toEqual([...ACCOUNT_TYPE_OPTIONS]);
  });
});

// A record neither row on the page carries, and free tier with no trial, which
// is what the create endpoint makes.
const CREATED_ORG: AdminOrganization = {
  id: "org_placeholder_three",
  name: "Placeholder Three",
  slug: "placeholder-three",
  account_type: "free",
  whitelisted: false,
  member_count: 0,
  created_at: "2026-01-02T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

// The control's own behaviour is covered in CreateOrganization.test.tsx. These
// two are about the page: that it draws the control at all, and that what the
// control says reaches the one live region, which is the whole reason the
// announcement is routed through the page rather than spoken locally.
describe("organizations list create organization", () => {
  it("offers the create control in the toolbar", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    const control = screen.getByRole("button", { name: "Create organization" });
    // In the search box's own row, not the strip inside the table: this is the
    // page's action rather than the table's. The row is the search box's
    // grandparent, and walking up from the box rather than down from the page
    // keeps the assertion off every other container on it.
    const row = screen
      .getByLabelText("Search organizations")
      .closest("div")?.parentElement;
    expect(row?.contains(control)).toBe(true);
  });

  it("announces a created organization through the page's live region", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await screen.findByRole("link", { name: FIRST_ORG.name });

    fireEvent.click(
      screen.getByRole("button", { name: "Create organization" }),
    );
    fireEvent.change(await screen.findByLabelText("Organization name"), {
      target: { value: CREATED_ORG.name },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Create" }));
    });

    // The region itself, not a spy on the reporter. A control that announced
    // into its own region, or into nothing, would pass every test in the
    // component's own file and fail this one.
    await waitFor(() => {
      expect(announcement()).toContain(`Created ${CREATED_ORG.name}.`);
    });
  });
});

// The write cancels every organization read in flight before it goes out, and
// this control is pressable while the page's first list read is still open: the
// toolbar is drawn whether or not the rows have arrived. A cancelled read
// reverts and nothing restarts it, so a refused create can leave the table on
// its loading state with no request outstanding.
describe("organizations list create organization: a refusal", () => {
  it("leaves the list fetching rather than cancelled", async () => {
    let releaseList!: (result: ListOrganizationsResult) => void;
    mocks.listOrganizations.mockReturnValueOnce(
      new Promise<ListOrganizationsResult>((resolve) => {
        releaseList = resolve;
      }),
    );

    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    expect(screen.getByText("Loading...")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Create organization" }),
    );
    fireEvent.change(await screen.findByLabelText("Organization name"), {
      target: { value: CREATED_ORG.name },
    });
    mocks.createOrganization.mockRejectedValueOnce(
      new GramAdminError(422, { message: "no" }, "Unprocessable Entity"),
    );
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Create" }));
    });
    await screen.findByRole("alert");

    // The read the write cancelled is dead: React Query drops its answer, so
    // the rows can only arrive from a request made after the failure.
    releaseList({ organizations: ORGS });
    // An open Radix modal hides the rest of the page from the accessibility
    // tree, so the rows are unreachable by role until the operator is out of
    // the dialog. Closing it is their next move anyway.
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await screen.findByRole("link", { name: FIRST_ORG.name });
  });
});

describe("TableActionBar", () => {
  // Destructured off the tuple rather than off a slice, which widens each
  // element back to a bare column definition.
  // Position, not an id lookup, for the select column: leftmost is the claim.
  const [SELECT, FIRST, SECOND, THIRD] = ORG_COLUMNS;
  // Found by id rather than by position, so a column added after it does not
  // quietly become the one these cases are about.
  const ACTIONS = ORG_COLUMNS.find((definition) => definition.id === "actions");
  if (!SELECT || !FIRST || !SECOND || !THIRD || !ACTIONS) {
    throw new Error(
      "ORG_COLUMNS needs a select column, three data ones and an actions one",
    );
  }

  // Sliced, so the array carries the element type useTable asks for. Two data
  // columns for the cases about the bar's own two rules, and the control
  // column beside one of them for the cases where a column that cannot be
  // hidden is in the count.
  const MENU_COLUMNS = ORG_COLUMNS.slice(1, 3);
  const WITH_ACTIONS_COLUMN: typeof MENU_COLUMNS = [ACTIONS, FIRST];
  // A second opt-out column, so the rule still has to count hideable columns
  // rather than visible ones when more than one of them cannot be hidden.
  const WITH_TWO_LOCKED_COLUMNS: typeof MENU_COLUMNS = [
    ACTIONS,
    { ...SECOND, enableHiding: false },
    FIRST,
  ];

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
    // Actions and Name, both visible. Actions opts out of hiding, so it is on
    // screen whatever the operator does and it is not the column that keeps
    // the table readable. Counting it would leave Name free to go, and the
    // table behind this menu would be a strip of controls above rows holding
    // no record.
    const { onVisibilityChange } = openColumnsMenu({}, WITH_ACTIONS_COLUMN);

    const item = itemFor(FIRST.header);
    fireEvent.click(item);

    expect(onVisibilityChange).not.toHaveBeenCalled();
    expect(item.getAttribute("aria-disabled")).toBe("true");
    // The actions column is locked too, by its own opt-out rather than by this
    // rule, so the operator cannot reach the same state from the other side.
    expect(itemFor(ACTIONS.header).getAttribute("aria-disabled")).toBe("true");
  });

  it("stops the operator hiding the last data column beside two locked ones", () => {
    // Every extra column that opts out of hiding is another way to make the
    // count wrong: a guard counting every visible column instead of every
    // hideable one now needs three columns on screen before it lets go of one.
    const { onVisibilityChange } = openColumnsMenu({}, WITH_TWO_LOCKED_COLUMNS);

    const item = itemFor(FIRST.header);
    fireEvent.click(item);

    expect(onVisibilityChange).not.toHaveBeenCalled();
    expect(item.getAttribute("aria-disabled")).toBe("true");
    expect(itemFor(ACTIONS.header).getAttribute("aria-disabled")).toBe("true");
    expect(itemFor(SECOND.header).getAttribute("aria-disabled")).toBe("true");
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

  it("names the select column in the menu and locks it", () => {
    const { onVisibilityChange } = openColumnsMenu({}, [SELECT, FIRST, SECOND]);

    // Its header draws a checkbox rather than text, so there is no name to
    // read off it and the menu would otherwise list it by its raw id.
    const item = screen.getByRole("menuitemcheckbox", { name: "Select" });

    // Hiding it would take away the only way to make a selection, and leave
    // the bar above the table offering an action against nothing.
    fireEvent.click(item);
    expect(onVisibilityChange).not.toHaveBeenCalled();
    expect(item.getAttribute("aria-disabled")).toBe("true");
  });
});

// A param the schema drops is absent from the parsed search, so every expected
// value below is the whole object the route sees.
describe("organizationsSearchSchema", () => {
  const cases: [string, Record<string, unknown>, OrganizationsSearch][] = [
    ["reads a hand-written type", { type: "free" }, { type: ["free"] }],
    [
      "keeps a type the picker does not offer",
      { type: "startup" },
      { type: ["startup"] },
    ],
    [
      "reads a list of types",
      { type: ["pro", "enterprise"] },
      { type: ["pro", "enterprise"] },
    ],
    [
      "sorts a chosen set into the picker's order",
      { type: ["enterprise", "free"] },
      { type: ["free", "enterprise"] },
    ],
    ["drops a type named twice", { type: ["pro", "pro"] }, { type: ["pro"] }],
    [
      "keeps an unrecognised type after the ones the picker offers",
      { type: ["startup", "pro"] },
      { type: ["pro", "startup"] },
    ],
    ["drops an empty list of types", { type: [] }, {}],
    [
      "sorts trial states into the order the picker offers them",
      { trial: ["expired", "running"] },
      { trial: ["running", "expired"] },
    ],
    [
      "drops a trial state the server does not derive",
      { trial: ["hibernating"] },
      {},
    ],
    [
      "reads a status the picker offers",
      { disabled: ["disabled"] },
      { disabled: ["disabled"] },
    ],
    [
      "reads the flag this list used to carry as both statuses",
      { disabled: true },
      { disabled: ["active", "disabled"] },
    ],
    ["drops a status outside the two", { disabled: ["retired"] }, {}],
    ["reads an all-digit term the router coerced", { q: 123 }, { q: "123" }],
    ["reads a boolean term the router coerced", { q: true }, { q: "true" }],
    ["reads a null term the router coerced", { q: null }, { q: "null" }],
    ["drops a term that is a list, not a word", { q: ["acme"] }, {}],
    ["drops a term that is only whitespace", { q: "   " }, {}],
    ["trims the term a pasted link carries", { q: "  acme  " }, { q: "acme" }],
    ["reads a direction in the union", { dir: "desc" }, { dir: "desc" }],
    ["drops a direction outside the union", { dir: "sideways" }, {}],
    ["drops the old disabled flag when it is off", { disabled: false }, {}],
    ["drops a key the schema does not declare", { page: 2 }, {}],
  ];

  it.each(cases)("%s", (_name, search, expected) => {
    expect(organizationsSearchSchema(search)).toEqual(expected);
  });
});

// The one action whose own success takes its control off the page: a re-armed
// record is running rather than demoted, so the peek footer drops the Re-arm
// button and mounts an Extend button beside it. Its own block and its own
// fixture list, so nothing above this reads a fourth row.
describe("re-arming a trial from the peek panel", () => {
  const DEMOTED_ORG: AdminOrganization = {
    id: "org_placeholder_five",
    name: "Placeholder Five",
    slug: "placeholder-five",
    account_type: "free",
    whitelisted: false,
    trial_state: "demoted",
    member_count: 2,
    created_at: "2026-02-02T00:00:00Z",
    updated_at: "2026-02-07T00:00:00Z",
  };

  const REARMED_TRIAL_END = "2026-09-04T00:00:00Z";

  beforeEach(() => {
    mocks.listOrganizations.mockResolvedValue({ organizations: [DEMOTED_ORG] });
    mocks.rearmTrial.mockResolvedValue({
      ...DEMOTED_ORG,
      account_type: "enterprise",
      whitelisted: true,
      trial_state: "running",
      trial_ends_at: REARMED_TRIAL_END,
    });
  });

  async function pressRearm(): Promise<HTMLElement> {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(DEMOTED_ORG.name);
    const control = within(peekPanel()).getByRole("button", {
      name: `Re-arm trial for ${DEMOTED_ORG.name}`,
    });
    fireEvent.click(control);
    await screen.findByRole("dialog");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Re-arm" }));
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    return control;
  }

  it("re-arms from the panel and repaints the panel with the answer", async () => {
    await pressRearm();

    // The default day count, sent without the operator typing anything.
    expect(mocks.rearmTrial).toHaveBeenCalledWith({
      id: DEMOTED_ORG.id,
      days: 14,
    });
    expect(peekPanel().textContent).toContain(shortDate(REARMED_TRIAL_END));
    expect(announcement()).toBe(
      `${DEMOTED_ORG.name} trial re-armed for 14 days.`,
    );
  });

  it("gives the keyboard to the panel when the control that opened the dialog goes", async () => {
    const control = await pressRearm();

    // Not the same node through the write, unlike extend: the footer's two
    // trial controls are sibling conditionals in separate slots, so React
    // unmounts one and mounts the other rather than reusing the element.
    expect(control.isConnected).toBe(false);
    expect(
      within(peekPanel()).getByRole("button", {
        name: `Extend trial for ${DEMOTED_ORG.name}`,
      }),
    ).toBeTruthy();

    // The panel, and specifically not the body. Radix would have focused a
    // DialogTrigger that does not exist here, leaving the keyboard at the top
    // of the page with the panel still open beside it.
    await waitFor(() => {
      expect(document.activeElement).toBe(peekPanel());
    });
    expect(document.activeElement).not.toBe(document.body);
  });
});

describe("starting a trial from the peek panel", () => {
  async function pressStart(): Promise<HTMLElement> {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    await peekOn(SECOND_ORG.name);
    const control = within(peekPanel()).getByRole("button", {
      name: `Start trial for ${SECOND_ORG.name}`,
    });
    fireEvent.click(control);
    await screen.findByRole("dialog");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Start trial" }));
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    return control;
  }

  it("starts from the panel and repaints the panel with the answer", async () => {
    await pressStart();

    expect(mocks.startTrial).toHaveBeenCalledWith({
      id: SECOND_ORG.id,
      days: 14,
    });
    expect(peekPanel().textContent).toContain(shortDate(STARTED_TRIAL_END));
    expect(announcement()).toBe(
      `${SECOND_ORG.name} trial started for 14 days.`,
    );
  });

  it("gives the keyboard to the panel when the control that opened the dialog goes", async () => {
    const control = await pressStart();

    // Same sibling-slot unmount as re-arm: Start comes down and Extend comes
    // up, so the node the dialog opened from is gone.
    expect(control.isConnected).toBe(false);
    expect(
      within(peekPanel()).getByRole("button", {
        name: `Extend trial for ${SECOND_ORG.name}`,
      }),
    ).toBeTruthy();

    await waitFor(() => {
      expect(document.activeElement).toBe(peekPanel());
    });
    expect(document.activeElement).not.toBe(document.body);
  });
});
