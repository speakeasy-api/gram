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

import { AnnounceProvider, OrganizationActions } from "./OrganizationActions";

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

async function renderMenu(org: AdminOrganization = ORG): Promise<HTMLElement> {
  await renderWithApp(
    <AnnounceProvider value={announce}>
      <OrganizationActions org={org} layout="menu" />
    </AnnounceProvider>,
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
    <AnnounceProvider value={announce}>
      <OrganizationActions org={org} layout="footer" />
    </AnnounceProvider>,
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

    expect(menuItems()).toEqual(["Re-enable", "Extend trial"]);
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

describe("the peek panel footer", () => {
  it("offers the same three actions as buttons rather than a menu", async () => {
    await renderFooter();

    expect(
      screen.queryByRole("button", { name: `Actions for ${ORG.name}` }),
    ).toBeNull();
    expect(screen.getByRole("button", { name: "Disable" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Extend trial" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Re-enable" })).toBeNull();
  });

  it("shows Re-enable for a disabled organization", async () => {
    await renderFooter(DISABLED_ORG);

    expect(screen.getByRole("button", { name: "Re-enable" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Disable" })).toBeNull();
  });

  it("hides Extend trial for a trial the server would refuse", async () => {
    await renderFooter({ ...ORG, trial_state: "converted" });

    expect(screen.queryByRole("button", { name: "Extend trial" })).toBeNull();
  });

  it("confirms before it disables, the same as the row menu does", async () => {
    await renderFooter();

    fireEvent.click(screen.getByRole("button", { name: "Disable" }));

    await screen.findByRole("dialog");
    expect(mocks.disableOrganization).not.toHaveBeenCalled();
  });

  it("stays live while a write is in flight rather than going disabled", async () => {
    const held = deferred<AdminOrganization>();
    mocks.enableOrganization.mockReturnValue(held.promise);
    await renderFooter(DISABLED_ORG);
    const button = screen.getByRole("button", { name: "Re-enable" });

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
