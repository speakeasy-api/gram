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
  MIN_TRIAL_EXTENSION_DAYS,
  TRIAL_STATES,
  type AdminOrganization,
  type TrialState,
} from "@/lib/gramAdminApi";
import { renderWithApp } from "@/test/harness";

import {
  OrganizationActions,
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
}));

// The three writes only. errorMessage stays real, because what the operator is
// told about a failure is the subject of several of these tests.
vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    disableOrganization: mocks.disableOrganization,
    enableOrganization: mocks.enableOrganization,
    extendTrial: mocks.extendTrial,
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
): Promise<void> {
  await renderWithApp(
    <WriteReportProvider value={REPORTER}>
      <OrganizationActions org={ORG} layout="menu" actions={actions} />
    </WriteReportProvider>,
  );
  fireEvent.pointerDown(
    screen.getByRole("button", { name: `Actions for ${ORG.name}` }),
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

// The dialog opens from a menu item, and the menu takes a moment to unmount
// itself around the state change that opens it.
async function openExtendDialog(): Promise<void> {
  fireEvent.click(screen.getByRole("menuitem", { name: "Extend trial" }));
  await screen.findByRole("dialog");
}

async function submitDays(value: string): Promise<void> {
  fireEvent.change(dayInput(), { target: { value } });
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Extend" }));
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

// The record draws two bars at once, so each has to be able to ask for its own
// half. Both layouts are gated, not just the one the record happens to use.
describe("the actions prop", () => {
  it.each([
    ["all", ["Disable", "Extend trial"]],
    ["lifecycle", ["Disable"]],
    ["trial", ["Extend trial"]],
  ] as ["all" | "lifecycle" | "trial", string[]][])(
    "draws %s as buttons",
    async (actions, expected) => {
      await renderWithApp(
        <WriteReportProvider value={REPORTER}>
          <OrganizationActions org={ORG} layout="buttons" actions={actions} />
        </WriteReportProvider>,
      );

      expect(
        screen.queryAllByRole("button").map((button) => button.textContent),
      ).toEqual(expected);
    },
  );

  it.each([
    ["all", ["Disable", "Extend trial"]],
    ["lifecycle", ["Disable"]],
    ["trial", ["Extend trial"]],
  ] as ["all" | "lifecycle" | "trial", string[]][])(
    "draws %s in the menu too",
    async (actions, expected) => {
      await renderMenuWith(actions);

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

    await submitDays("30");

    expect(mocks.extendTrial).toHaveBeenCalledWith({ id: ORG.id, days: 30 });
  });
});

describe("the extend trial dialog", () => {
  it("starts on the trial length the rest of the system assumes", async () => {
    await renderMenu();
    await openExtendDialog();

    expect(dayInput().value).toBe(DEFAULT_DAYS);
  });

  it("extends by the day count the operator typed", async () => {
    await renderMenu();
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
  // a bound written as an exclusive comparison passes a one-sided test.
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
      await renderMenu();
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
    await renderMenu();
    await openExtendDialog();

    await submitDays("0");
    await submitDays("0");

    // Nothing on screen moves on the second press: the state is already
    // rejected and the text is a constant, so the alert is not re-inserted and
    // a role="alert" announces only what is inserted or changed. The live
    // region is what carries the second refusal, which is why this path
    // announces rather than relying on the node.
    expect(announce).toHaveBeenCalledTimes(2);
    expect(announce).toHaveBeenNthCalledWith(
      2,
      `Could not extend the trial for ${ORG.name}: Enter a whole number of days between ${MIN_TRIAL_EXTENSION_DAYS} and ${MAX_TRIAL_EXTENSION_DAYS}.`,
    );
    expect(mocks.extendTrial).not.toHaveBeenCalled();
  });

  it("points the day count at the message under it", async () => {
    await renderMenu();
    await openExtendDialog();

    await submitDays("0");

    // aria-invalid says the value is wrong. Only this says what would make it
    // right, to a user who has moved back to the field and cannot see the line
    // beneath it.
    const alert = await screen.findByRole("alert");
    expect(alert.id).toBeTruthy();
    expect(dayInput().getAttribute("aria-describedby")).toBe(alert.id);
  });

  it("stops calling a corrected value out of bounds", async () => {
    const held = deferred<AdminOrganization>();
    await renderMenu();
    await openExtendDialog();
    await submitDays("0");
    expect(await screen.findByRole("alert")).toBeTruthy();

    mocks.extendTrial.mockReturnValue(held.promise);
    await submitDays("30");

    // While the corrected request is still in flight, which is the only moment
    // it is visible: success unmounts the dialog. The field would otherwise
    // sit there marked invalid, under a bounds message, while its own request
    // runs.
    await screen.findByRole("button", { name: "Extending..." });
    expect(dayInput().getAttribute("aria-invalid")).toBe("false");
    expect(screen.queryByRole("alert")).toBeNull();

    await act(async () => {
      held.resolve({ ...ORG, trial_ends_at: "2026-05-20T00:00:00Z" });
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
    await submitDays("30");
    expect(await screen.findByRole("alert")).toBeTruthy();

    await submitDays("0");

    // One alert, not two. The failed request and the refused value are both
    // true, and showing both gives the operator two reasons with nothing
    // saying which one the next press answers.
    const alerts = screen.getAllByRole("alert");
    expect(alerts).toHaveLength(1);
    expect(alerts[0]?.textContent).toContain(
      `between ${MIN_TRIAL_EXTENSION_DAYS} and ${MAX_TRIAL_EXTENSION_DAYS}`,
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

    await submitDays("30");

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
    await submitDays("30");
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
    // dialog on it would report a request this one has not made.
    expect(screen.queryByRole("alert")).toBeNull();
    expect(dayInput().value).toBe(DEFAULT_DAYS);
  });

  it("holds the operator out of the dialog while the write is in flight", async () => {
    const held = deferred<AdminOrganization>();
    mocks.extendTrial.mockReturnValue(held.promise);
    await renderMenu();
    await openExtendDialog();

    await submitDays("30");

    const submit = await screen.findByRole("button", { name: "Extending..." });
    expect(submit.hasAttribute("disabled")).toBe(true);
    expect(dayInput().hasAttribute("disabled")).toBe(true);

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

    await submitDays("1");

    expect(announce).toHaveBeenCalledWith(
      `${ORG.name} trial extended by 1 day.`,
    );
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
    await submitDays("30");
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
    await submitDays("30");

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
