import { useRef, useState, type JSX } from "react";
import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

import {
  GramAdminError,
  MAX_TRIAL_EXTENSION_DAYS,
  MAX_TRIAL_REARM_DAYS,
  MIN_TRIAL_EXTENSION_DAYS,
  MIN_TRIAL_REARM_DAYS,
  TRIAL_STATES,
  type AdminOrganization,
  type TrialState,
} from "@/lib/gramAdminApi";
import { renderWithApp } from "@/test/harness";
import { fmtDateShort } from "@/lib/utils";

import {
  OrganizationActions,
  TrialDaysDialog,
  WriteReportProvider,
} from "./OrganizationActions";

// One note for whoever writes the next test here. A synchronous read straight
// after `await act(...)` misses TanStack Query's pending state: its notify
// manager schedules on a macrotask, so `expect(button.textContent).toBe(
// "Disabling...")` on the line after the click reads the state from before the
// mutation started and passes for the wrong reason. Every assertion about a
// write in flight below goes through `findBy*` or `waitFor` for that reason.
//
// The same applies to focus after a dialog closes: Radix's FocusScope restores
// it from a `setTimeout(..., 0)`, so it has not moved yet when the close
// returns.

const mocks = vi.hoisted(() => ({
  disableOrganization:
    vi.fn<(body: { id: string }) => Promise<AdminOrganization>>(),
  enableOrganization:
    vi.fn<(body: { id: string }) => Promise<AdminOrganization>>(),
  extendTrial:
    vi.fn<(body: { id: string; days: number }) => Promise<AdminOrganization>>(),
  rearmTrial:
    vi.fn<(body: { id: string; days: number }) => Promise<AdminOrganization>>(),
}));

// The writes only. errorMessage stays real, because what the operator is told
// about a failure is the subject of several of these tests.
vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    disableOrganization: mocks.disableOrganization,
    enableOrganization: mocks.enableOrganization,
    extendTrial: mocks.extendTrial,
    rearmTrial: mocks.rearmTrial,
  };
});

// Live, and trialling: the record that offers Disable and Extend. Every case
// below that needs another state derives it from this one, so a field a test
// does not name is the same field in every test.
const ORG: AdminOrganization = {
  id: "org_placeholder_one",
  name: "Placeholder One",
  slug: "placeholder-one",
  account_type: "enterprise",
  whitelisted: false,
  trial_state: "running",
  trial_ends_at: "2026-05-06T00:00:00Z",
  member_count: 3,
  created_at: "2026-01-02T00:00:00Z",
  updated_at: "2026-01-07T00:00:00Z",
};

const DISABLED_ORG: AdminOrganization = {
  ...ORG,
  disabled_at: "2026-03-04T00:00:00Z",
};

// Demoted and back on the free tier: the record that offers Re-arm trial and
// no longer offers Extend trial. It carries no end date, so its dialog is the
// day count rather than the calendar.
const DEMOTED_ORG: AdminOrganization = {
  ...ORG,
  account_type: "free",
  trial_state: "demoted",
  trial_ends_at: undefined,
};

// What the endpoint answers a re-arm with.
const REARMED_ORG: AdminOrganization = {
  ...ORG,
  whitelisted: true,
  trial_ends_at: "2026-08-28T00:00:00Z",
};

// The record's trial ends on the 6th of May, which is deliberately not today.
// An extension anchored on today rather than on the record's own end date is
// the bug the server's own comment warns about, and it agrees with the right
// answer only where the two dates are the same day.
//
// The days the operator can reach from that anchor, as the calendar names them.
const EARLIEST = "2026-05-07";
const DEFAULT_END = "2026-05-20";
const LATEST = "2027-05-06";
const PAST_LATEST = "2027-05-07";

// `trial_ends_at` is a NOT NULL column and a live trial state only arises from
// a trial row, so the server cannot send this. The client types it optional,
// and an anchor guessed from today would extend a trial from a date the server
// is not holding, so the day count stays for it.
const ORG_NO_END: AdminOrganization = { ...ORG, trial_ends_at: undefined };

// The two states the server will extend. Written out rather than imported from
// the module under test: importing the set would move this expectation along
// with the rule it is meant to hold in place.
const EXTENDABLE: (TrialState | undefined)[] = ["running", "ending_soon"];

const DEFAULT_DAYS = "14";

const announce = vi.fn<(text: string) => void>();
const showFailure = vi.fn<(text: string | null) => void>();
const REPORTER = { announce, showFailure };

async function renderMenu(org: AdminOrganization = ORG): Promise<HTMLElement> {
  await renderWithApp(
    <WriteReportProvider value={REPORTER}>
      <OrganizationActions org={org} layout="menu" />
    </WriteReportProvider>,
  );
  const trigger = screen.getByRole("button", {
    name: `Actions for ${org.name}`,
  });
  // A Radix menu opens on pointerdown, not on click.
  fireEvent.pointerDown(trigger, {
    button: 0,
    ctrlKey: false,
    pointerType: "mouse",
  });
  return trigger;
}

async function renderFooter(org: AdminOrganization = ORG): Promise<void> {
  await renderWithApp(
    <WriteReportProvider value={REPORTER}>
      <OrganizationActions org={org} layout="buttons" />
    </WriteReportProvider>,
  );
}

// The menu layout with a half of the actions asked for. Nothing ships this
// today: the record's two split bars are both `buttons`. It exists so the gate
// is tested where a future caller would first exercise it, rather than found
// broken by that caller.
async function renderMenuWith(
  actions: "all" | "lifecycle" | "trial",
  org: AdminOrganization = ORG,
): Promise<void> {
  await renderWithApp(
    <WriteReportProvider value={REPORTER}>
      <OrganizationActions org={org} layout="menu" actions={actions} />
    </WriteReportProvider>,
  );
  fireEvent.pointerDown(
    screen.getByRole("button", { name: `Actions for ${org.name}` }),
    { button: 0, ctrlKey: false, pointerType: "mouse" },
  );
}

function menuItems(): string[] {
  return screen
    .queryAllByRole("menuitem")
    .map((item) => item.textContent ?? "");
}

function dialog(): HTMLElement {
  return screen.getByRole("dialog");
}

// The node, not the role. Radix takes the portal out of the accessibility tree
// while it works out whether a dismiss is allowed, so `queryByRole("dialog")`
// answers null for a dialog that is still on the page and refusing to close.
function dialogNode(): HTMLElement | null {
  return document.querySelector("[data-slot='dialog-content']");
}

function overlay(): HTMLElement {
  const node = document.querySelector("[data-slot='dialog-overlay']");
  if (!(node instanceof HTMLElement)) throw new Error("no dialog overlay");
  return node;
}

function dayInput(): HTMLInputElement {
  const input = screen.getByLabelText("Days");
  if (!(input instanceof HTMLInputElement)) {
    throw new Error("the day count is not an input");
  }
  return input;
}

// The control that opens the calendar, and whose label is the date it holds.
function endDateTrigger(): HTMLElement {
  return screen.getByLabelText("Ends on");
}

// react-day-picker names each cell by an ISO day, which is the one account of a
// day in this DOM that does not move with a locale. shadcn's day button carries
// a `data-day` too, formatted for the reader, so the cell is the stable handle.
function dayCell(iso: string): HTMLElement | null {
  const cell = document.querySelector(`td[data-day='${iso}']`);
  return cell instanceof HTMLElement ? cell : null;
}

function dayButton(iso: string): HTMLButtonElement {
  const button = dayCell(iso)?.querySelector("button");
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error(`the calendar is not offering ${iso}`);
  }
  return button;
}

async function openCalendar(): Promise<void> {
  fireEvent.click(endDateTrigger());
  await screen.findByRole("grid");
}

// The calendar shows one month and opens on whichever month it was left on, so
// a day is reached the way the operator reaches it: back to the start of the
// range, then forward. Both loops are bounded by the year the server extends by.
function pageTo(iso: string): void {
  for (
    let month = 0;
    month < 12 && !navBlocked("Go to the Previous Month");
    month += 1
  ) {
    fireEvent.click(
      screen.getByRole("button", { name: "Go to the Previous Month" }),
    );
  }
  for (let month = 0; !dayCell(iso) && month < 12; month += 1) {
    fireEvent.click(
      screen.getByRole("button", { name: "Go to the Next Month" }),
    );
  }
}

async function submitExtend(): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Extend" }));
  });
}

async function pickDay(iso: string): Promise<void> {
  await openCalendar();
  pageTo(iso);
  fireEvent.click(dayButton(iso));
  // The calendar closes with the pick, and the trigger takes the date.
  await waitFor(() => {
    expect(screen.queryByRole("grid")).toBeNull();
  });
}

async function pickAndSubmit(iso: string): Promise<void> {
  await pickDay(iso);
  await submitExtend();
}

// Midday UTC, so the day is the same one in every populated zone. Through the
// app's own formatter, so the assertion is about which day is named and not
// about the runner's locale.
function rendered(day: string): string {
  return fmtDateShort(`${day}T12:00:00Z`);
}

// What the announcement says for a count of days, which is not `${n} days`.
function dayCountText(days: number): string {
  return `${days} ${days === 1 ? "day" : "days"}`;
}

// react-day-picker refuses a month press by marking the button rather than by
// removing it, and marks it as either `disabled` or `aria-disabled` depending on
// how it was built.
function navBlocked(name: string): boolean {
  const button = screen.getByRole("button", { name });
  return (
    button.hasAttribute("disabled") ||
    button.getAttribute("aria-disabled") === "true"
  );
}

// The dialog opens from a menu item, and the menu takes a moment to unmount
// itself around the state change that opens it.
async function openExtendDialog(): Promise<void> {
  fireEvent.click(screen.getByRole("menuitem", { name: "Extend trial" }));
  await screen.findByRole("dialog");
}

async function openRearmDialog(): Promise<void> {
  fireEvent.click(screen.getByRole("menuitem", { name: "Re-arm trial" }));
  await screen.findByRole("dialog");
}

async function submitDays(value: string): Promise<void> {
  fireEvent.change(dayInput(), { target: { value } });
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Extend" }));
  });
}

async function submitRearmDays(value: string): Promise<void> {
  fireEvent.change(dayInput(), { target: { value } });
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Re-arm" }));
  });
}

// A write held open, so a test can read the page while the request is in
// flight and settle it when it chooses.
function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

beforeEach(() => {
  announce.mockReset();
  showFailure.mockReset();
  mocks.disableOrganization.mockReset();
  mocks.disableOrganization.mockResolvedValue({
    ...ORG,
    disabled_at: "2026-08-01T00:00:00Z",
  });
  mocks.enableOrganization.mockReset();
  mocks.enableOrganization.mockResolvedValue(ORG);
  mocks.extendTrial.mockReset();
  mocks.extendTrial.mockResolvedValue({
    ...ORG,
    trial_ends_at: "2026-05-20T00:00:00Z",
  });
  mocks.rearmTrial.mockReset();
  mocks.rearmTrial.mockResolvedValue(REARMED_ORG);
});

afterEach(cleanup);

describe("the row menu", () => {
  it("offers Disable, and not Re-enable, for a live organization", async () => {
    await renderMenu();

    // Exactly one of the two. Both at once would be a menu offering to undo
    // something that has not happened, and the operator has no way to tell
    // which one the record is in.
    expect(menuItems()).toEqual(["Disable", "Extend trial"]);
  });

  it("offers Re-enable, and not Disable, for a disabled organization", async () => {
    await renderMenu(DISABLED_ORG);

    // Re-enable alone. The record is on a running trial, and the walk below
    // says why the trial is not extendable while the organization is off.
    expect(menuItems()).toEqual(["Re-enable"]);
  });

  it.each([...TRIAL_STATES, undefined])(
    "offers Extend trial for the %s trial only where the server would take it",
    async (state) => {
      await renderMenu({ ...ORG, trial_state: state });

      // Every state the server can send, walked at runtime. The server refuses
      // converted, demoted and expired with a conflict, and an organization
      // with no trial has no end date to add days to, so offering the action
      // there is offering a request that cannot succeed. A seventh state fails
      // here as well as in the build.
      expect(menuItems().includes("Extend trial")).toBe(
        EXTENDABLE.includes(state),
      );
    },
  );

  it.each([...TRIAL_STATES, undefined])(
    "keeps Extend trial off a disabled organization on the %s trial",
    async (state) => {
      await renderMenu({ ...DISABLED_ORG, trial_state: state });

      // The server would take this request: nothing in the extend handler
      // reads disabled_at, and the trial goes on running while every member is
      // locked out. Offering it anyway is offering to buy more of a trial
      // nobody can use.
      expect(menuItems().includes("Extend trial")).toBe(false);
    },
  );

  it.each([...TRIAL_STATES, undefined])(
    "offers Re-arm trial for the %s trial only where the server would take it",
    async (state) => {
      await renderMenu({ ...ORG, trial_state: state });

      // Only a demoted trial. A converted or running one is refused with a
      // conflict, and an expired trial has not been demoted yet: the sweeper
      // that demotes it has not reached it, so there is nothing to put back.
      expect(menuItems().includes("Re-arm trial")).toBe(state === "demoted");
    },
  );

  it.each([...TRIAL_STATES, undefined])(
    "never offers both trial actions at once, on the %s trial",
    async (state) => {
      await renderMenu({ ...ORG, trial_state: state });

      // The two menus read together rather than one at a time. Two separate
      // tests would each pass while one record offered both, and a seventh
      // state added to TRIAL_STATES is walked here as well as in the build.
      const offered = menuItems().filter(
        (item) => item === "Extend trial" || item === "Re-arm trial",
      );
      expect(offered.length).toBeLessThanOrEqual(1);
    },
  );

  it.each([...TRIAL_STATES, undefined])(
    "keeps Re-arm trial off a disabled organization on the %s trial",
    async (state) => {
      await renderMenu({ ...DISABLED_ORG, trial_state: state });

      // The server would take it: nothing in the re-arm handler reads
      // disabled_at, so the restored trial would run behind the lockout.
      expect(menuItems().includes("Re-arm trial")).toBe(false);
    },
  );

  it("waits for the confirmation before it disables anything", async () => {
    await renderMenu();

    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));

    await screen.findByRole("dialog");
    // The whole point of the confirmation. Disabling cuts a customer off, and
    // this menu sits one row away from four others.
    expect(mocks.disableOrganization).not.toHaveBeenCalled();
    expect(dialog().textContent).toContain(`Disable ${ORG.name}?`);
  });

  it("sends nothing when the operator cancels the confirmation", async () => {
    await renderMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    await screen.findByRole("dialog");

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(mocks.disableOrganization).not.toHaveBeenCalled();
  });

  it("disables the organization once the operator confirms", async () => {
    await renderMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    await screen.findByRole("dialog");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Disable" }));
    });

    expect(mocks.disableOrganization).toHaveBeenCalledWith({ id: ORG.id });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(announce).toHaveBeenCalledWith(`${ORG.name} is disabled.`);
  });

  it("re-enables without a confirmation, because nothing is lost", async () => {
    await renderMenu(DISABLED_ORG);

    await act(async () => {
      fireEvent.click(screen.getByRole("menuitem", { name: "Re-enable" }));
    });

    expect(mocks.enableOrganization).toHaveBeenCalledWith({ id: ORG.id });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(announce).toHaveBeenCalledWith(`${ORG.name} is enabled.`);
    // A write that succeeded clears the banner an earlier one left behind:
    // the operator has just been told the current state of this record.
    expect(showFailure).toHaveBeenCalledWith(null);
  });
});

// The Overview separates lifecycle actions into Danger zone and trial actions
// into the Enterprise trial panel, so each instance needs its own action scope.
// Both layouts honor that scope, not just the buttons used by the Overview.
describe("the actions prop", () => {
  it.each([
    ["all", "live running", ORG, ["Disable", "Extend trial"]],
    ["lifecycle", "live running", ORG, ["Disable"]],
    ["trial", "live running", ORG, ["Extend trial"]],
    ["lifecycle", "demoted", DEMOTED_ORG, ["Disable"]],
    ["trial", "demoted", DEMOTED_ORG, ["Re-arm trial"]],
    ["trial", "disabled running", DISABLED_ORG, []],
  ] as ["all" | "lifecycle" | "trial", string, AdminOrganization, string[]][])(
    "draws %s as buttons for a %s organization",
    async (actions, _state, org, expected) => {
      await renderWithApp(
        <WriteReportProvider value={REPORTER}>
          <OrganizationActions org={org} layout="buttons" actions={actions} />
        </WriteReportProvider>,
      );

      expect(
        screen.queryAllByRole("button").map((button) => button.textContent),
      ).toEqual(expected);
    },
  );

  it.each([
    ["all", "live running", ORG, ["Disable", "Extend trial"]],
    ["lifecycle", "live running", ORG, ["Disable"]],
    ["trial", "live running", ORG, ["Extend trial"]],
    ["lifecycle", "demoted", DEMOTED_ORG, ["Disable"]],
    ["trial", "demoted", DEMOTED_ORG, ["Re-arm trial"]],
    ["trial", "disabled running", DISABLED_ORG, []],
  ] as ["all" | "lifecycle" | "trial", string, AdminOrganization, string[]][])(
    "draws %s in the menu for a %s organization",
    async (actions, _state, org, expected) => {
      await renderMenuWith(actions, org);

      expect(menuItems()).toEqual(expected);
    },
  );

  it("still opens the trial dialog from a bar that draws no lifecycle action", async () => {
    // The dialogs are mounted whatever this instance draws. A bar that offers
    // one action has to be able to finish it.
    await renderWithApp(
      <WriteReportProvider value={REPORTER}>
        <OrganizationActions org={ORG} layout="buttons" actions="trial" />
      </WriteReportProvider>,
    );

    fireEvent.click(
      screen.getByRole("button", { name: `Extend trial for ${ORG.name}` }),
    );
    await screen.findByRole("dialog");

    await pickAndSubmit("2026-06-05");

    expect(mocks.extendTrial).toHaveBeenCalledWith({ id: ORG.id, days: 30 });
  });
});

describe("the button styling prop", () => {
  it("leaves every other surface's buttons stock", async () => {
    // The trial callout asks for a toned control because it is a toned panel.
    // The row menu, the peek panel footer and the record header draw on the
    // page's own background, where that treatment would be a button with no
    // fill and a border belonging to a warning nothing on screen gives.
    await renderFooter();

    for (const button of screen.getAllByRole("button")) {
      const classes = button.className.split(" ");
      expect(classes).toContain("bg-background");
      expect(classes).not.toContain("bg-transparent");
      expect(button.className).not.toMatch(/border-\[hsl/);
    }
  });

  it("draws the classes the caller asks for", async () => {
    await renderWithApp(
      <WriteReportProvider value={REPORTER}>
        <OrganizationActions
          org={ORG}
          layout="buttons"
          buttonClassName="bg-transparent"
        />
      </WriteReportProvider>,
    );

    // Both of them: a bar styles the bar, not whichever action happens to be
    // first.
    for (const button of screen.getAllByRole("button")) {
      expect(button.className.split(" ")).toContain("bg-transparent");
    }
  });
});

describe("the extend trial dialog", () => {
  // Every test here runs in a zone that is not UTC, because the fault this
  // dialog is most likely to carry is invisible in UTC: the record's trial ends
  // at midnight UTC, which is the day before locally, so a conversion that
  // reads the anchor in the reader's own zone is out by one and no assertion
  // on a UTC runner can tell.
  beforeEach(() => {
    vi.stubEnv("TZ", "America/Los_Angeles");
  });

  afterEach(() => {
    vi.unstubAllEnvs();
  });

  // The other half of the pair. One dialog component draws both, so the extend
  // words have to be asserted somewhere or they can be replaced by the re-arm
  // words without a test noticing.
  it("names the record and says what the date moves", async () => {
    await renderMenu();
    await openExtendDialog();

    const text = dialog().textContent ?? "";
    expect(text).toContain(`Extend the trial for ${ORG.name}?`);
    expect(text).toContain("The trial ends on");
    // Extend moves a date and nothing else, so it must not carry the sentence
    // that describes what re-arm restores.
    expect(text).not.toContain("model provider keys");
  });

  it("opens on the trial length the rest of the system assumes", async () => {
    await renderMenu();
    await openExtendDialog();

    // Selected, not merely shown: the operator who wants the usual extension
    // presses Extend and touches the calendar at all.
    expect(endDateTrigger().textContent).toBe(rendered(DEFAULT_END));
    await openCalendar();
    expect(dayCell(DEFAULT_END)?.getAttribute("data-selected")).toBe("true");
  });

  it("reads the trial's end as the server's day, not the reader's", async () => {
    // The zone really moved, and in it the record's trial ends on the 5th.
    // Without this the two assertions below pass for the wrong reason.
    expect(new Date(ORG.trial_ends_at ?? "").getDate()).toBe(5);

    await renderMenu();
    await openExtendDialog();

    // The day the server acts on, which is the day the record shows elsewhere.
    expect(dialog().textContent).toContain(`ends on ${rendered("2026-05-06")}`);

    await pickAndSubmit(EARLIEST);

    // One day, not two. An anchor read in the reader's zone would put the
    // trial's last day on the 5th and make the 7th two days away.
    expect(mocks.extendTrial).toHaveBeenCalledWith({ id: ORG.id, days: 1 });
  });

  it("names and counts the same day in a zone ahead of UTC", async () => {
    // The other side of UTC, which the zone above cannot stand in for. West of
    // UTC a local midnight is a later instant than the day it stands for and
    // east of it an earlier one, so two faults that America/Los_Angeles hides
    // are visible here: a count taken as a subtraction of instants floors a day
    // short, and a day handed back as a local midnight renders as the day
    // before.
    vi.stubEnv("TZ", "Asia/Tokyo");
    await renderMenu();
    await openExtendDialog();

    await pickDay("2026-06-05");

    expect(endDateTrigger().textContent).toBe(rendered("2026-06-05"));
    expect(dialog().textContent).toContain(
      `end on ${rendered("2026-06-05")}, 30 days later`,
    );

    await submitExtend();

    expect(mocks.extendTrial).toHaveBeenCalledWith({ id: ORG.id, days: 30 });
  });

  // Three dates rather than one, and none of them today: the conversion the
  // server's own comment warns about anchors on today instead of on the trial's
  // current end, and a single date near today would let that through.
  it.each([
    [EARLIEST, MIN_TRIAL_EXTENSION_DAYS],
    ["2026-06-05", 30],
    [LATEST, MAX_TRIAL_EXTENSION_DAYS],
  ] as [string, number][])(
    "sends the day count that reaches %s",
    async (day, days) => {
      await renderMenu();
      await openExtendDialog();

      await pickAndSubmit(day);

      expect(mocks.extendTrial).toHaveBeenCalledWith({ id: ORG.id, days });
      await waitFor(() => {
        expect(screen.queryByRole("dialog")).toBeNull();
      });
      expect(announce).toHaveBeenCalledWith(
        `${ORG.name} trial extended by ${dayCountText(days)}.`,
      );
    },
  );

  it("says the date the count it sends will reach", async () => {
    await renderMenu();
    await openExtendDialog();

    await pickDay("2026-06-05");

    // The operator picks a date and the request sends a count. Both are the
    // record's future, and the dialog is the only place they are shown to
    // agree.
    expect(dialog().textContent).toContain(
      `The trial will end on ${rendered("2026-06-05")}, 30 days later`,
    );
  });

  it("offers no day the server would refuse to extend to", async () => {
    await renderMenu();
    await openExtendDialog();
    await openCalendar();

    // The trial's own last day is one day short of the minimum extension, so
    // it is the first day off the bottom of the range.
    expect(dayCell("2026-05-06")?.getAttribute("data-disabled")).toBe("true");
    expect(dayButton(EARLIEST).hasAttribute("disabled")).toBe(false);
    // And the calendar cannot be paged back to a month made entirely of them.
    expect(navBlocked("Go to the Previous Month")).toBe(true);
  });

  it("offers no day past the year the server would extend by", async () => {
    await renderMenu();
    await openExtendDialog();
    await openCalendar();

    // Twelve presses from May 2026 to the month the last extendable day is in.
    for (let month = 0; month < 12; month += 1) {
      fireEvent.click(
        screen.getByRole("button", { name: "Go to the Next Month" }),
      );
    }

    expect(dayButton(LATEST).hasAttribute("disabled")).toBe(false);
    expect(dayCell(PAST_LATEST)?.getAttribute("data-disabled")).toBe("true");
    expect(navBlocked("Go to the Next Month")).toBe(true);
  });

  it("refuses an empty calendar rather than sending it as NaN", async () => {
    await renderMenu();
    await openExtendDialog();

    // The one refusal the calendar's own bounds cannot prevent: pressing the
    // selected day again clears the selection.
    await pickDay(DEFAULT_END);
    await submitExtend();

    expect(mocks.extendTrial).not.toHaveBeenCalled();
    expect(endDateTrigger().textContent).toBe("Pick a date");
    expect((await screen.findByRole("alert")).textContent).toContain(
      `between ${rendered(EARLIEST)} and ${rendered(LATEST)}`,
    );
    expect(endDateTrigger().getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByRole("dialog")).toBeTruthy();
  });

  it("says so again when the same empty calendar is refused twice", async () => {
    await renderMenu();
    await openExtendDialog();
    await pickDay(DEFAULT_END);

    await submitExtend();
    await submitExtend();

    // Nothing on screen moves on the second press: the state is already
    // rejected and the text is a constant, so the alert is not re-inserted and
    // a role="alert" announces only what is inserted or changed. The live
    // region is what carries the second refusal, which is why this path
    // announces rather than relying on the node.
    expect(announce).toHaveBeenCalledTimes(2);
    expect(announce).toHaveBeenNthCalledWith(
      2,
      `Could not extend the trial for ${ORG.name}: Pick a date between ${rendered(EARLIEST)} and ${rendered(LATEST)}.`,
    );
    expect(mocks.extendTrial).not.toHaveBeenCalled();
  });

  it("points the date at the message under it", async () => {
    await renderMenu();
    await openExtendDialog();
    await pickDay(DEFAULT_END);

    await submitExtend();

    // aria-invalid says the value is wrong. Only this says what would make it
    // right, to a user who has moved back to the field and cannot see the line
    // beneath it.
    const alert = await screen.findByRole("alert");
    expect(alert.id).toBeTruthy();
    expect(endDateTrigger().getAttribute("aria-describedby")).toBe(alert.id);
  });

  it("stops calling a corrected date out of bounds", async () => {
    const held = deferred<AdminOrganization>();
    await renderMenu();
    await openExtendDialog();
    await pickDay(DEFAULT_END);
    await submitExtend();
    expect(await screen.findByRole("alert")).toBeTruthy();

    mocks.extendTrial.mockReturnValue(held.promise);
    await pickAndSubmit("2026-06-05");

    // While the corrected request is still in flight, which is the only moment
    // it is visible: success unmounts the dialog. The field would otherwise
    // sit there marked invalid, under a bounds message, while its own request
    // runs.
    await screen.findByRole("button", { name: "Extending..." });
    expect(endDateTrigger().getAttribute("aria-invalid")).toBe("false");
    expect(screen.queryByRole("alert")).toBeNull();

    await act(async () => {
      held.resolve({ ...ORG, trial_ends_at: "2026-06-05T00:00:00Z" });
    });
  });

  it("shows the bounds alone when a refusal follows a server failure", async () => {
    mocks.extendTrial.mockRejectedValue(
      new GramAdminError(
        409,
        { name: "conflict", message: "organization has no running trial" },
        "gram admin 409 Conflict",
      ),
    );
    await renderMenu();
    await openExtendDialog();
    await submitExtend();
    expect(await screen.findByRole("alert")).toBeTruthy();

    await pickDay(DEFAULT_END);
    await submitExtend();

    // One alert, not two. The failed request and the refused value are both
    // true, and showing both gives the operator two reasons with nothing
    // saying which one the next press answers.
    const alerts = screen.getAllByRole("alert");
    expect(alerts).toHaveLength(1);
    expect(alerts[0]?.textContent).toContain(
      `between ${rendered(EARLIEST)} and ${rendered(LATEST)}`,
    );
  });

  it("keeps the dialog open and names the conflict the server answered", async () => {
    mocks.extendTrial.mockRejectedValue(
      new GramAdminError(
        409,
        {
          name: "conflict",
          message: "organization has no running enterprise trial to extend",
        },
        "gram admin 409 Conflict",
      ),
    );
    await renderMenu();
    await openExtendDialog();

    await submitExtend();

    // A modal takes the page's live region out of the accessibility tree, so
    // the dialog carries its own account of the failure.
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect((await screen.findByRole("alert")).textContent).toBe(
      "organization has no running enterprise trial to extend",
    );
    expect(announce).toHaveBeenCalledWith(
      `Could not extend the trial for ${ORG.name}: organization has no running enterprise trial to extend`,
    );
  });

  it("opens the next attempt without the last one's failure", async () => {
    mocks.extendTrial.mockRejectedValue(
      new GramAdminError(404, null, "gram admin 404 Not Found"),
    );
    await renderMenu();
    await openExtendDialog();
    await pickAndSubmit("2026-06-05");
    expect(await screen.findByRole("alert")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    fireEvent.pointerDown(
      screen.getByRole("button", { name: `Actions for ${ORG.name}` }),
      { button: 0, ctrlKey: false, pointerType: "mouse" },
    );
    await openExtendDialog();

    // The failure belonged to the attempt the operator abandoned. Opening the
    // dialog on it would report a request this one has not made, and the date
    // it left behind is not this attempt's either.
    expect(screen.queryByRole("alert")).toBeNull();
    expect(endDateTrigger().textContent).toBe(rendered(DEFAULT_END));
  });

  it("holds the operator out of the dialog while the write is in flight", async () => {
    const held = deferred<AdminOrganization>();
    mocks.extendTrial.mockReturnValue(held.promise);
    await renderMenu();
    await openExtendDialog();

    await submitExtend();

    const submit = await screen.findByRole("button", { name: "Extending..." });
    expect(submit.hasAttribute("disabled")).toBe(true);
    expect(endDateTrigger().hasAttribute("disabled")).toBe(true);

    await act(async () => {
      held.resolve({ ...ORG, trial_ends_at: "2026-05-20T00:00:00Z" });
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(mocks.extendTrial).toHaveBeenCalledTimes(1);
  });

  it("says one day rather than 1 days", async () => {
    await renderMenu();
    await openExtendDialog();

    await pickAndSubmit(EARLIEST);

    expect(announce).toHaveBeenCalledWith(
      `${ORG.name} trial extended by 1 day.`,
    );
  });
});

// The client types `trial_ends_at` optional, so there is a record shape with a
// live trial and no date to pick against. The day count is what that record
// gets: an anchor guessed from today would extend the trial from a date the
// server is not holding.
describe("the extend trial dialog, with no end date to anchor on", () => {
  it("starts on the trial length the rest of the system assumes", async () => {
    await renderMenu(ORG_NO_END);
    await openExtendDialog();

    expect(dayInput().value).toBe(DEFAULT_DAYS);
    expect(screen.queryByLabelText("Ends on")).toBeNull();
  });

  it("extends by the day count the operator typed", async () => {
    await renderMenu(ORG_NO_END);
    await openExtendDialog();

    await submitDays("30");

    expect(mocks.extendTrial).toHaveBeenCalledWith({ id: ORG.id, days: 30 });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(announce).toHaveBeenCalledWith(
      `${ORG.name} trial extended by 30 days.`,
    );
  });

  // Both edges and both sides of each. The server takes 1 to 365 inclusive, so
  // a bound written as an exclusive comparison passes a one-sided test. This is
  // the surface the guard is reachable from: on the calendar the same bounds
  // are what the operator is offered, and the guard behind them is a backstop.
  it.each([
    [String(MIN_TRIAL_EXTENSION_DAYS - 1), false],
    [String(MIN_TRIAL_EXTENSION_DAYS), true],
    [String(MAX_TRIAL_EXTENSION_DAYS), true],
    [String(MAX_TRIAL_EXTENSION_DAYS + 1), false],
    ["-7", false],
    ["1.5", false],
    ["", false],
  ] as [string, boolean][])(
    "sends %s to the server only where the server would take it",
    async (value, sent) => {
      await renderMenu(ORG_NO_END);
      await openExtendDialog();

      await submitDays(value);

      expect(mocks.extendTrial).toHaveBeenCalledTimes(sent ? 1 : 0);
      if (!sent) {
        // Refused, and said so. A request the server is certain to reject is
        // worse than no request, and a dialog that swallows the press is
        // worse than either.
        expect((await screen.findByRole("alert")).textContent).toContain(
          `between ${MIN_TRIAL_EXTENSION_DAYS} and ${MAX_TRIAL_EXTENSION_DAYS}`,
        );
        expect(dayInput().getAttribute("aria-invalid")).toBe("true");
        expect(screen.getByRole("dialog")).toBeTruthy();
      }
    },
  );

  it("says so again when the same value is refused twice", async () => {
    await renderMenu(ORG_NO_END);
    await openExtendDialog();

    await submitDays("0");
    await submitDays("0");

    expect(announce).toHaveBeenCalledTimes(2);
    expect(announce).toHaveBeenNthCalledWith(
      2,
      `Could not extend the trial for ${ORG.name}: Enter a whole number of days between ${MIN_TRIAL_EXTENSION_DAYS} and ${MAX_TRIAL_EXTENSION_DAYS}.`,
    );
    expect(mocks.extendTrial).not.toHaveBeenCalled();
  });

  it("points the day count at the message under it", async () => {
    await renderMenu(ORG_NO_END);
    await openExtendDialog();

    await submitDays("0");

    const alert = await screen.findByRole("alert");
    expect(alert.id).toBeTruthy();
    expect(dayInput().getAttribute("aria-describedby")).toBe(alert.id);
  });

  it("stops calling a corrected value out of bounds", async () => {
    const held = deferred<AdminOrganization>();
    await renderMenu(ORG_NO_END);
    await openExtendDialog();
    await submitDays("0");
    expect(await screen.findByRole("alert")).toBeTruthy();

    mocks.extendTrial.mockReturnValue(held.promise);
    await submitDays("30");

    await screen.findByRole("button", { name: "Extending..." });
    expect(dayInput().getAttribute("aria-invalid")).toBe("false");
    expect(screen.queryByRole("alert")).toBeNull();

    await act(async () => {
      held.resolve({ ...ORG, trial_ends_at: "2026-05-20T00:00:00Z" });
    });
  });

  it("says one day rather than 1 days", async () => {
    await renderMenu(ORG_NO_END);
    await openExtendDialog();

    await submitDays("1");

    expect(announce).toHaveBeenCalledWith(
      `${ORG.name} trial extended by 1 day.`,
    );
  });
});

describe("the re-arm trial dialog", () => {
  it("starts on the trial length the rest of the system assumes", async () => {
    await renderMenu(DEMOTED_ORG);
    await openRearmDialog();

    expect(dayInput().value).toBe(DEFAULT_DAYS);
  });

  // Re-arm is not extend with another verb. An operator who reads a day count
  // and expects only a new date has been told less than half of what the write
  // does, and three of the four things it does are not dates at all.
  //
  // Named here as well as in the extend test below, because one dialog draws
  // both: with only one path asserted, the two sets of words are a prop swap
  // apart and the suite stays green either way round.
  it("says what the write does besides moving a date", async () => {
    await renderMenu(DEMOTED_ORG);
    await openRearmDialog();

    const text = dialog().textContent ?? "";
    expect(text).toContain(`Re-arm the trial for ${DEMOTED_ORG.name}?`);
    expect(text).toContain("account type");
    expect(text).toContain("removes the trial disable cause");
    expect(text).toContain("admin, billing, or unknown causes remain disabled");
    expect(text).toContain("book-a-demo gate");
    expect(text).toContain("counted from now");
  });

  it("re-arms for the day count the operator typed", async () => {
    await renderMenu(DEMOTED_ORG);
    await openRearmDialog();

    await submitRearmDays("30");

    expect(mocks.rearmTrial).toHaveBeenCalledWith({
      id: DEMOTED_ORG.id,
      days: 30,
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(announce).toHaveBeenCalledWith(
      `${DEMOTED_ORG.name} trial re-armed for 30 days.`,
    );
  });

  it("says one day rather than 1 days", async () => {
    await renderMenu(DEMOTED_ORG);
    await openRearmDialog();

    await submitRearmDays("1");

    expect(announce).toHaveBeenCalledWith(
      `${DEMOTED_ORG.name} trial re-armed for 1 day.`,
    );
  });

  // Both edges and both sides of each, against the re-arm bounds rather than
  // the extension ones. The two pairs are equal today and are separate names on
  // the server so they can stop being.
  it.each([
    [String(MIN_TRIAL_REARM_DAYS - 1), false],
    [String(MIN_TRIAL_REARM_DAYS), true],
    [String(MAX_TRIAL_REARM_DAYS), true],
    [String(MAX_TRIAL_REARM_DAYS + 1), false],
    ["-7", false],
    ["1.5", false],
    ["", false],
  ] as [string, boolean][])(
    "sends %s to the server only where the server would take it",
    async (value, sent) => {
      await renderMenu(DEMOTED_ORG);
      await openRearmDialog();

      await submitRearmDays(value);

      expect(mocks.rearmTrial).toHaveBeenCalledTimes(sent ? 1 : 0);
      if (!sent) {
        expect((await screen.findByRole("alert")).textContent).toContain(
          `between ${MIN_TRIAL_REARM_DAYS} and ${MAX_TRIAL_REARM_DAYS}`,
        );
        expect(dayInput().getAttribute("aria-invalid")).toBe("true");
        expect(screen.getByRole("dialog")).toBeTruthy();
        expect(announce).toHaveBeenCalledWith(
          `Could not re-arm the trial for ${DEMOTED_ORG.name}: Enter a whole number of days between ${MIN_TRIAL_REARM_DAYS} and ${MAX_TRIAL_REARM_DAYS}.`,
        );
      }
    },
  );

  it("keeps the dialog open on a refusal, holding the day count", async () => {
    mocks.rearmTrial.mockRejectedValue(
      new GramAdminError(
        409,
        {
          name: "conflict",
          message: "organization has no demoted enterprise trial to re-arm",
        },
        "gram admin 409 Conflict",
      ),
    );
    await renderMenu(DEMOTED_ORG);
    await openRearmDialog();

    await submitRearmDays("30");

    // A rejected attempt is one the operator adjusts, not one they retype.
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(dayInput().value).toBe("30");
    expect((await screen.findByRole("alert")).textContent).toBe(
      "organization has no demoted enterprise trial to re-arm",
    );
    expect(announce).toHaveBeenCalledWith(
      `Could not re-arm the trial for ${DEMOTED_ORG.name}: organization has no demoted enterprise trial to re-arm`,
    );
  });

  it("sends one request however many times the button is pressed", async () => {
    const held = deferred<AdminOrganization>();
    mocks.rearmTrial.mockReturnValue(held.promise);
    await renderMenu(DEMOTED_ORG);
    await openRearmDialog();

    await submitRearmDays("30");
    const submit = await screen.findByRole("button", { name: "Re-arming..." });
    await act(async () => {
      fireEvent.click(submit);
      fireEvent.click(submit);
    });

    // Re-arm is not idempotent: a second one that landed would restart the
    // trial from later, and the operator would have no account of which run
    // the row is showing.
    expect(mocks.rearmTrial).toHaveBeenCalledTimes(1);

    await act(async () => {
      held.resolve(REARMED_ORG);
    });
    await waitFor(() => {
      expect(dialogNode()).toBeNull();
    });
  });

  it("cannot be dismissed while the write is in flight", async () => {
    const held = deferred<AdminOrganization>();
    mocks.rearmTrial.mockReturnValue(held.promise);
    await renderMenu(DEMOTED_ORG);
    await openRearmDialog();
    await submitRearmDays("30");
    await screen.findByRole("button", { name: "Re-arming..." });

    await act(async () => {
      fireEvent.keyDown(document, { key: "Escape" });
    });

    expect(dialogNode()).not.toBeNull();

    await act(async () => {
      held.resolve(REARMED_ORG);
    });
    await waitFor(() => {
      expect(dialogNode()).toBeNull();
    });
  });

  it("opens the next attempt without the last one's failure", async () => {
    mocks.rearmTrial.mockRejectedValue(
      new GramAdminError(404, null, "gram admin 404 Not Found"),
    );
    const trigger = await renderMenu(DEMOTED_ORG);
    await openRearmDialog();
    await submitRearmDays("30");
    expect(await screen.findByRole("alert")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(dialogNode()).toBeNull();
    });
    fireEvent.pointerDown(trigger, {
      button: 0,
      ctrlKey: false,
      pointerType: "mouse",
    });
    await openRearmDialog();

    expect(screen.queryByRole("alert")).toBeNull();
    expect(dayInput().value).toBe(DEFAULT_DAYS);
  });
});

// The five ways out of the re-arm dialog, each its own Radix path and each one
// a way to end with the keyboard on document.body.
describe("the keyboard when the re-arm dialog closes", () => {
  it("goes back to the row menu trigger when the write succeeds", async () => {
    const trigger = await renderMenu(DEMOTED_ORG);
    await openRearmDialog();

    await submitRearmDays("30");

    await waitFor(() => {
      expect(dialogNode()).toBeNull();
    });
    await waitFor(() => {
      expect(document.activeElement).toBe(trigger);
    });
  });

  it.each([
    [
      "Escape",
      (): void => {
        fireEvent.keyDown(document, { key: "Escape" });
      },
    ],
    [
      "Cancel",
      (): void => {
        fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
      },
    ],
    [
      "the backdrop",
      (): void => {
        fireEvent.pointerDown(overlay(), {
          button: 0,
          ctrlKey: false,
          pointerType: "mouse",
        });
        fireEvent.click(overlay(), { button: 0, ctrlKey: false });
      },
    ],
    [
      "the X",
      (): void => {
        fireEvent.click(screen.getByRole("button", { name: "Close" }));
      },
    ],
  ] as [string, () => void][])(
    "goes back to the row menu trigger when %s closes it",
    async (_path, close) => {
      const trigger = await renderMenu(DEMOTED_ORG);
      await openRearmDialog();

      await act(async () => {
        close();
      });

      await waitFor(() => {
        expect(dialogNode()).toBeNull();
      });
      await waitFor(() => {
        expect(document.activeElement).toBe(trigger);
      });
      expect(mocks.rearmTrial).not.toHaveBeenCalled();
    },
  );

  // The connected-control branch only: this harness pins `org`, so the button
  // outlives its own write. The fallback for when it does not is held by
  // "gives the keyboard to the panel ..." in index.test.tsx, which has a cache.
  it("goes back to the footer control while that control is still mounted", async () => {
    await renderFooter(DEMOTED_ORG);
    const button = screen.getByRole("button", {
      name: `Re-arm trial for ${DEMOTED_ORG.name}`,
    });
    fireEvent.click(button);
    await screen.findByRole("dialog");

    await submitRearmDays("30");

    await waitFor(() => {
      expect(document.activeElement).toBe(button);
    });
  });
});

// Both endpoints bound their day count at 1 and 365, so nothing drawn from the
// app can tell a dialog that reads its `bounds` prop from one that hardcodes
// the extension pair. A pair neither endpoint uses is the only way to hold the
// parameterisation, and it is the reason the component is exported.
describe("the day-count dialog on bounds of its own", () => {
  const BOUNDS = { min: 7, max: 30 };

  async function renderOnFabricatedBounds(): Promise<
    ReturnType<typeof vi.fn<(days: number) => void>>
  > {
    const onSubmit = vi.fn<(days: number) => void>();
    await renderWithApp(
      <WriteReportProvider value={REPORTER}>
        <TrialDaysDialog
          bounds={BOUNDS}
          // No range, so the dialog draws the day count this canary types into.
          range={undefined}
          title="Fabricated?"
          description="Fabricated."
          submitLabel="Go"
          pendingLabel="Going..."
          failureLead="Could not go"
          pending={false}
          failure={null}
          onCancel={() => {}}
          onCloseAutoFocus={() => {}}
          onSubmit={onSubmit}
        />
      </WriteReportProvider>,
    );
    return onSubmit;
  }

  async function submit(label: string): Promise<void> {
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: label }));
    });
  }

  it("takes its hint and its field attributes from the bounds it was given", async () => {
    await renderOnFabricatedBounds();

    expect(dialog().textContent).toContain(
      `between ${BOUNDS.min} and ${BOUNDS.max}`,
    );
    expect(dayInput().getAttribute("min")).toBe(String(BOUNDS.min));
    expect(dayInput().getAttribute("max")).toBe(String(BOUNDS.max));
  });

  it("refuses a value the bounds it was given exclude", async () => {
    const onSubmit = await renderOnFabricatedBounds();

    // Inside the extension bounds and outside these, which is the whole point:
    // a dialog reading the wrong pair sends it.
    fireEvent.change(dayInput(), { target: { value: "365" } });
    await submit("Go");

    expect(onSubmit).not.toHaveBeenCalled();
    expect((await screen.findByRole("alert")).textContent).toContain(
      `between ${BOUNDS.min} and ${BOUNDS.max}`,
    );
  });

  it("takes a value only these bounds allow", async () => {
    const onSubmit = await renderOnFabricatedBounds();

    fireEvent.change(dayInput(), { target: { value: String(BOUNDS.max) } });
    await submit("Go");

    expect(onSubmit).toHaveBeenCalledWith(BOUNDS.max);
  });
});

describe("a rejected write", () => {
  it("leaves the confirmation open, holding the reason", async () => {
    mocks.disableOrganization.mockRejectedValue(
      new GramAdminError(
        404,
        { name: "not_found", message: "organization not found" },
        "gram admin 404 Not Found",
      ),
    );
    await renderMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    await screen.findByRole("dialog");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Disable" }));
    });

    expect(screen.getByRole("dialog")).toBeTruthy();
    expect((await screen.findByRole("alert")).textContent).toBe(
      "organization not found",
    );
    expect(announce).toHaveBeenCalledWith(
      `Could not disable ${ORG.name}: organization not found`,
    );
  });

  it("opens the next confirmation without the last one's failure", async () => {
    mocks.disableOrganization.mockRejectedValue(
      new GramAdminError(404, null, "gram admin 404 Not Found"),
    );
    const trigger = await renderMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    await screen.findByRole("dialog");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Disable" }));
    });
    expect(await screen.findByRole("alert")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(dialogNode()).toBeNull();
    });
    fireEvent.pointerDown(trigger, {
      button: 0,
      ctrlKey: false,
      pointerType: "mouse",
    });
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    await screen.findByRole("dialog");

    // The same rule the extend dialog follows. The failure belonged to the
    // attempt the operator abandoned, and reporting it here reports a request
    // this confirmation has not made.
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("gives a re-enable failure the sentence the server sent", async () => {
    mocks.enableOrganization.mockRejectedValue(
      new GramAdminError(
        409,
        { name: "conflict", message: "organization is not disabled" },
        "gram admin 409 Conflict",
      ),
    );
    await renderMenu(DISABLED_ORG);

    await act(async () => {
      fireEvent.click(screen.getByRole("menuitem", { name: "Re-enable" }));
    });

    // A 4xx body carries a sentence the operator can act on, and the status
    // line does not. The 5xx case below is the other half of that rule, and
    // only the two together tell the message apart from the status line.
    const text = `Could not re-enable ${ORG.name}: organization is not disabled`;
    expect(announce).toHaveBeenCalledWith(text);
    // Shown as well as spoken. This is the one write with no dialog, so
    // without the banner the whole account of the failure is inside sr-only.
    expect(showFailure).toHaveBeenCalledWith(text);
  });

  it("reports a re-enable that failed, which has no dialog to report in", async () => {
    mocks.enableOrganization.mockRejectedValue(
      new GramAdminError(
        500,
        { name: "internal", message: "enable" },
        "gram admin 500 Internal Server Error",
      ),
    );
    await renderMenu(DISABLED_ORG);

    await act(async () => {
      fireEvent.click(screen.getByRole("menuitem", { name: "Re-enable" }));
    });

    // The status line, not the body: a 5xx body carries the handler's verb
    // phrase, and "enable" is not a sentence an operator can act on.
    expect(announce).toHaveBeenCalledWith(
      `Could not re-enable ${ORG.name}: gram admin 500 Internal Server Error`,
    );
  });
});

// Radix hard-codes its close behaviour: onCloseAutoFocus cancels FocusScope's
// restore of the previously focused element and focuses the DialogTrigger
// instead. These dialogs have no trigger, so without a restore of their own
// every one of these paths ends with the keyboard on document.body, at the top
// of the page, after an action the operator took on one row of a long table.
describe("the keyboard when a dialog closes", () => {
  function ActionsWithStableFallback({
    initialOrg,
    exposeSetOrg,
  }: {
    initialOrg: AdminOrganization;
    exposeSetOrg: (setOrg: (org: AdminOrganization) => void) => void;
  }): JSX.Element {
    const [org, setOrg] = useState(initialOrg);
    const fallback = useRef<HTMLHeadingElement>(null);
    exposeSetOrg(setOrg);

    return (
      <>
        <h5 ref={fallback} tabIndex={-1}>
          Details
        </h5>
        <WriteReportProvider value={REPORTER}>
          <OrganizationActions
            org={org}
            layout="buttons"
            focusFallbackRef={fallback}
          />
        </WriteReportProvider>
      </>
    );
  }

  it.each(["disable", "re-enable", "re-arm"] as const)(
    "uses the caller fallback after a successful %s replaces its opener",
    async (action) => {
      const initialOrg =
        action === "disable"
          ? ORG
          : action === "re-enable"
            ? DISABLED_ORG
            : DEMOTED_ORG;
      const nextOrg = action === "disable" ? DISABLED_ORG : REARMED_ORG;
      let setOrg = (_org: AdminOrganization): void => {};
      const write =
        action === "disable"
          ? mocks.disableOrganization
          : action === "re-enable"
            ? mocks.enableOrganization
            : mocks.rearmTrial;
      write.mockImplementation(async () => {
        setOrg(nextOrg);
        return nextOrg;
      });

      await renderWithApp(
        <ActionsWithStableFallback
          initialOrg={initialOrg}
          exposeSetOrg={(next) => {
            setOrg = next;
          }}
        />,
      );
      fireEvent.click(
        screen.getByRole("button", {
          name:
            action === "disable"
              ? `Disable ${ORG.name}`
              : action === "re-enable"
                ? `Re-enable ${ORG.name}`
                : `Re-arm trial for ${ORG.name}`,
        }),
      );

      if (action === "re-arm") {
        await screen.findByRole("dialog");
        await submitRearmDays("30");
      } else if (action === "disable") {
        await screen.findByRole("dialog");
        await act(async () => {
          fireEvent.click(screen.getByRole("button", { name: "Disable" }));
        });
      }

      await waitFor(() => {
        expect(document.activeElement).toBe(
          screen.getByRole("heading", { name: "Details" }),
        );
      });
      expect(document.activeElement).not.toBe(document.body);
    },
  );

  it("goes back to the row menu trigger when the write succeeds", async () => {
    const trigger = await renderMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    await screen.findByRole("dialog");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Disable" }));
    });

    await waitFor(() => {
      expect(document.activeElement).toBe(trigger);
    });
  });

  // The four ways out that are not the write itself. Each is its own Radix
  // path, and each one used to end on the body.
  it.each([
    [
      "Escape",
      (): void => {
        fireEvent.keyDown(document, { key: "Escape" });
      },
    ],
    [
      "Cancel",
      (): void => {
        fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
      },
    ],
    [
      "the backdrop",
      (): void => {
        fireEvent.pointerDown(overlay(), {
          button: 0,
          ctrlKey: false,
          pointerType: "mouse",
        });
        fireEvent.click(overlay(), { button: 0, ctrlKey: false });
      },
    ],
    [
      "the X",
      (): void => {
        fireEvent.click(screen.getByRole("button", { name: "Close" }));
      },
    ],
  ] as [string, () => void][])(
    "goes back to the row menu trigger when %s closes the dialog",
    async (_path, close) => {
      const trigger = await renderMenu();
      fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
      await screen.findByRole("dialog");

      await act(async () => {
        close();
      });

      await waitFor(() => {
        expect(dialogNode()).toBeNull();
      });
      await waitFor(() => {
        expect(document.activeElement).toBe(trigger);
      });
      expect(mocks.disableOrganization).not.toHaveBeenCalled();
    },
  );

  it("goes back to the peek footer control the dialog opened from", async () => {
    await renderFooter();
    const button = screen.getByRole("button", { name: `Disable ${ORG.name}` });
    fireEvent.click(button);
    await screen.findByRole("dialog");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Disable" }));
    });

    await waitFor(() => {
      expect(document.activeElement).toBe(button);
    });
  });
});

describe("a write in flight", () => {
  // Cancel carries `disabled` while the write runs, so Escape and the overlay
  // are the only ways in and the guard on onOpenChange is the only thing
  // holding them off.
  it("cannot be dismissed out of the disable confirmation", async () => {
    const held = deferred<AdminOrganization>();
    mocks.disableOrganization.mockReturnValue(held.promise);
    await renderMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    await screen.findByRole("dialog");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Disable" }));
    });
    await screen.findByRole("button", { name: "Disabling..." });

    await act(async () => {
      fireEvent.keyDown(document, { key: "Escape" });
    });

    // The answer decides what the row says next. A dialog dismissed mid-write
    // leaves the operator with no account of how their action ended.
    expect(dialogNode()).not.toBeNull();

    await act(async () => {
      held.resolve({ ...ORG, disabled_at: "2026-08-01T00:00:00Z" });
    });
    await waitFor(() => {
      expect(dialogNode()).toBeNull();
    });
  });

  it("cannot be dismissed out of the extend dialog", async () => {
    const held = deferred<AdminOrganization>();
    mocks.extendTrial.mockReturnValue(held.promise);
    await renderMenu();
    await openExtendDialog();
    await submitExtend();
    await screen.findByRole("button", { name: "Extending..." });

    await act(async () => {
      fireEvent.keyDown(document, { key: "Escape" });
    });

    expect(dialogNode()).not.toBeNull();

    await act(async () => {
      held.resolve({ ...ORG, trial_ends_at: "2026-05-20T00:00:00Z" });
    });
    await waitFor(() => {
      expect(dialogNode()).toBeNull();
    });
  });

  it("marks the row menu trigger busy rather than disabling it", async () => {
    const held = deferred<AdminOrganization>();
    mocks.disableOrganization.mockReturnValue(held.promise);
    const trigger = await renderMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    await screen.findByRole("dialog");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Disable" }));
    });

    // Read off the node: an open modal takes the rest of the page out of the
    // accessibility tree, so the trigger cannot be found by role from here.
    await waitFor(() => {
      expect(trigger.getAttribute("aria-busy")).toBe("true");
    });
    expect(trigger.hasAttribute("disabled")).toBe(false);

    await act(async () => {
      held.resolve({ ...ORG, disabled_at: "2026-08-01T00:00:00Z" });
    });
    await waitFor(() => {
      expect(trigger.getAttribute("aria-busy")).toBe("false");
    });
  });

  it("marks both peek footer controls busy rather than disabling them", async () => {
    const held = deferred<AdminOrganization>();
    mocks.extendTrial.mockReturnValue(held.promise);
    await renderFooter();
    const disable = screen.getByRole("button", { name: `Disable ${ORG.name}` });
    const extend = screen.getByRole("button", {
      name: `Extend trial for ${ORG.name}`,
    });

    fireEvent.click(extend);
    await screen.findByRole("dialog");
    await submitExtend();

    // Both, not just the one that started it. A control that goes dead under
    // the operator's hand is the thing this design exists to avoid, and the
    // write is the record's, not the button's.
    await waitFor(() => {
      expect(extend.getAttribute("aria-busy")).toBe("true");
    });
    expect(disable.getAttribute("aria-busy")).toBe("true");
    expect(extend.hasAttribute("disabled")).toBe(false);
    expect(disable.hasAttribute("disabled")).toBe(false);

    await act(async () => {
      held.resolve({ ...ORG, trial_ends_at: "2026-05-20T00:00:00Z" });
    });
    await waitFor(() => {
      expect(extend.getAttribute("aria-busy")).toBe("false");
    });
  });
});

describe("the peek panel footer", () => {
  it("offers the same three actions as buttons rather than a menu", async () => {
    await renderFooter();

    expect(
      screen.queryByRole("button", { name: `Actions for ${ORG.name}` }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: `Disable ${ORG.name}` }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: `Extend trial for ${ORG.name}` }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: `Re-enable ${ORG.name}` }),
    ).toBeNull();
  });

  it("shows Re-enable for a disabled organization", async () => {
    await renderFooter(DISABLED_ORG);

    expect(
      screen.getByRole("button", { name: `Re-enable ${ORG.name}` }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: `Disable ${ORG.name}` }),
    ).toBeNull();
  });

  it("offers Re-arm trial, and not Extend trial, for a demoted trial", async () => {
    await renderFooter(DEMOTED_ORG);

    expect(
      screen.getByRole("button", {
        name: `Re-arm trial for ${DEMOTED_ORG.name}`,
      }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", {
        name: `Extend trial for ${DEMOTED_ORG.name}`,
      }),
    ).toBeNull();
  });

  it("hides Re-arm trial for a trial the server would refuse", async () => {
    await renderFooter();

    expect(
      screen.queryByRole("button", { name: `Re-arm trial for ${ORG.name}` }),
    ).toBeNull();
  });

  it("marks the re-arm control busy rather than disabling it", async () => {
    const held = deferred<AdminOrganization>();
    mocks.rearmTrial.mockReturnValue(held.promise);
    await renderFooter(DEMOTED_ORG);
    const rearm = screen.getByRole("button", {
      name: `Re-arm trial for ${DEMOTED_ORG.name}`,
    });

    fireEvent.click(rearm);
    await screen.findByRole("dialog");
    await submitRearmDays("30");

    // A control that goes dead under the operator's hand drops the keyboard
    // onto the body, which is the thing this design exists to avoid.
    await waitFor(() => {
      expect(rearm.getAttribute("aria-busy")).toBe("true");
    });
    expect(rearm.hasAttribute("disabled")).toBe(false);

    await act(async () => {
      held.resolve(REARMED_ORG);
    });
    await waitFor(() => {
      expect(rearm.getAttribute("aria-busy")).toBe("false");
    });
  });

  it("hides Extend trial for a trial the server would refuse", async () => {
    await renderFooter({ ...ORG, trial_state: "converted" });

    expect(
      screen.queryByRole("button", { name: `Extend trial for ${ORG.name}` }),
    ).toBeNull();
  });

  it("keeps a press on its controls and its dialog off the surface under them", async () => {
    const underneath = vi.fn<() => void>();
    await renderWithApp(
      // A handler in the position the list's row keeps one. The dialog is
      // portalled to the end of the document, but a portal's events still
      // travel up the React tree, so the confirmation reaches this too.
      <div onClick={underneath}>
        <WriteReportProvider value={REPORTER}>
          <OrganizationActions org={ORG} layout="buttons" />
        </WriteReportProvider>
      </div>,
    );

    fireEvent.click(
      screen.getByRole("button", { name: `Disable ${ORG.name}` }),
    );
    fireEvent.click(await screen.findByRole("dialog"));

    expect(underneath).not.toHaveBeenCalled();
  });

  it("confirms before it disables, the same as the row menu does", async () => {
    await renderFooter();

    fireEvent.click(
      screen.getByRole("button", { name: `Disable ${ORG.name}` }),
    );

    await screen.findByRole("dialog");
    expect(mocks.disableOrganization).not.toHaveBeenCalled();
  });

  it("stays live while a write is in flight rather than going disabled", async () => {
    const held = deferred<AdminOrganization>();
    mocks.enableOrganization.mockReturnValue(held.promise);
    await renderFooter(DISABLED_ORG);
    const button = screen.getByRole("button", {
      name: `Re-enable ${ORG.name}`,
    });

    fireEvent.click(button);

    // Disabling the control the operator just pressed drops the keyboard onto
    // the document body. The busy state rides on aria-busy instead, and the
    // accessible name does not move, so a screen reader is not told the
    // control was replaced.
    await waitFor(() => {
      expect(button.getAttribute("aria-busy")).toBe("true");
    });
    expect(button.hasAttribute("disabled")).toBe(false);
    expect(button.textContent).toBe("Re-enable");

    await act(async () => {
      held.resolve(ORG);
    });
    await waitFor(() => {
      expect(button.getAttribute("aria-busy")).toBe("false");
    });
  });
});
