import { useQuery } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationQuery, organizationsListQuery } from "@/lib/adminQueries";
import {
  GramAdminError,
  type AdminOrganization,
  type CreateOrganizationRequest,
  type ListOrganizationsResult,
} from "@/lib/gramAdminApi";
import { renderWithApp } from "@/test/harness";

import { CreateOrganization } from "./CreateOrganization";

// A synchronous read straight after a click misses TanStack Query's pending
// state: its notify manager schedules on a macrotask. Every assertion about a
// write in flight below goes through findBy* or waitFor for that reason.

const mocks = vi.hoisted(() => ({
  createOrganization:
    vi.fn<(body: CreateOrganizationRequest) => Promise<AdminOrganization>>(),
  listOrganizations: vi.fn<() => Promise<ListOrganizationsResult>>(),
  getOrganization: vi.fn<(idOrSlug: string) => Promise<AdminOrganization>>(),
  getOrganizationStats: vi.fn(),
}));

// The write and the reads it stales. errorMessage stays real, because what the
// operator is told about a refusal is the subject of several of these tests.
vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    createOrganization: mocks.createOrganization,
    listOrganizations: mocks.listOrganizations,
    getOrganization: mocks.getOrganization,
    getOrganizationStats: mocks.getOrganizationStats,
  };
});

const CREATED: AdminOrganization = {
  id: "org_placeholder_new",
  name: "Placeholder New",
  slug: "placeholder-new",
  account_type: "free",
  whitelisted: false,
  member_count: 0,
  created_at: "2026-01-02T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

// A filtered page, not the bare key the component invalidates. The operator can
// be on any filter and any page when they create, so a probe on the same key the
// component names would leave that claim undefended: narrowing the invalidation,
// or making it exact, would keep passing.
const PROBE_PARAMS = { q: "placeholder", account_types: ["free"] };

function ListProbe(): React.JSX.Element {
  const { data } = useQuery(organizationsListQuery(PROBE_PARAMS));
  return <span data-testid="rows">{data?.organizations.length ?? -1}</span>;
}

// Reads the detail entry the write fills in, by slug. Disabled, so it never
// fetches: whatever it shows came out of the cache.
function DetailProbe({ idOrSlug }: { idOrSlug: string }): React.JSX.Element {
  const { data } = useQuery({ ...organizationQuery(idOrSlug), enabled: false });
  return <span data-testid="detail">{data?.name ?? ""}</span>;
}

const announce = vi.fn<(text: string) => void>();
const showFailure = vi.fn<(text: string | null) => void>();
const REPORTER = { announce, showFailure };

async function open(extra?: React.ReactNode): Promise<void> {
  await renderWithApp(
    <>
      <CreateOrganization reporter={REPORTER} />
      {extra}
    </>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Create organization" }));
  await screen.findByRole("dialog");
}

function nameField(): HTMLInputElement {
  return screen.getByLabelText("Organization name") as HTMLInputElement;
}

function submitButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: /^(Create|Creating\.\.\.)$/ });
}

// The form rather than the button: Enter in the field submits it too, so this
// is the path a guard on the button alone does not cover.
function submitForm(): void {
  const form = screen.getByRole("dialog").querySelector("form");
  if (!form) throw new Error("the dialog has no form");
  fireEvent.submit(form);
}

function type(value: string): void {
  fireEvent.change(nameField(), { target: { value } });
}

// A refusal that carries a readable body, which is what a deployment with no
// WorkOS configuration answers with.
function refusal(message: string): GramAdminError {
  return new GramAdminError(422, { message }, "Unprocessable Entity");
}

// A write held open, so the pending state can be read before it lands.
function deferred(): {
  resolve: (org: AdminOrganization) => void;
  reject: (error: Error) => void;
} {
  let resolve!: (org: AdminOrganization) => void;
  let reject!: (error: Error) => void;
  mocks.createOrganization.mockReturnValue(
    new Promise<AdminOrganization>((res, rej) => {
      resolve = res;
      reject = rej;
    }),
  );
  return { resolve, reject };
}

beforeEach(() => {
  mocks.createOrganization.mockReset();
  mocks.createOrganization.mockResolvedValue(CREATED);
  mocks.listOrganizations.mockReset();
  mocks.listOrganizations.mockResolvedValue({ organizations: [] });
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(CREATED);
  announce.mockReset();
  showFailure.mockReset();
  mocks.getOrganizationStats.mockReset();
  mocks.getOrganizationStats.mockResolvedValue({
    total: 0,
    created_last_7_days: 0,
    customers: 0,
    customers_created_last_7_days: 0,
    trials_ending_soon: 0,
    disabled: 0,
    disabled_last_7_days: 0,
  });
});

afterEach(cleanup);

describe("creating an organization", () => {
  it("sends the name and closes", async () => {
    await open();
    type("Placeholder New");
    // The button itself, once. Everything else here drives the form, so an
    // inert primary control would otherwise go unnoticed.
    fireEvent.click(submitButton());

    await waitFor(() => {
      expect(mocks.createOrganization).toHaveBeenCalledWith({
        name: "Placeholder New",
      });
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("names the organization it created, on screen and out loud", async () => {
    // The server normalises the name it stores and answers with what it
    // stored, so the two differ here on purpose: an assertion against a
    // fixture that echoes the field would hold nothing.
    mocks.createOrganization.mockResolvedValue({
      ...CREATED,
      name: "Placeholder New",
    });
    await open();
    type("placeholder    new");
    submitForm();

    await screen.findByText(/Created Placeholder New\./);
    expect(screen.queryByText(/placeholder    new/)).toBeNull();
    await waitFor(() => {
      expect(announce).toHaveBeenCalledWith(
        expect.stringContaining("Created Placeholder New."),
      );
    });
  });

  it("clears the page's failure banner", async () => {
    await open();
    type("Placeholder New");
    submitForm();

    // A re-enable that failed reports in a banner on the page, and every
    // sibling write clears it when the next one lands.
    await waitFor(() => {
      expect(showFailure).toHaveBeenCalledWith(null);
    });
  });

  it("says the new row may not be on the page", async () => {
    await open();
    type("Placeholder New");
    submitForm();

    // The record is free tier with no trial, so a filtered list can be right
    // to leave it out and the confirmation cannot be "look at the table".
    const line = await screen.findByText(
      /may not show it under the current filters/,
    );
    // The line is truncated in a row that also holds a search box and the
    // filter chips, and the caveat is the half that gets clipped. This is an
    // attribute assertion: happy-dom lays nothing out and cannot say what is
    // on screen.
    expect(line.getAttribute("title")).toBe(line.textContent);
  });

  it("trims the name before sending it", async () => {
    await open();
    type("  Placeholder New  ");
    submitForm();

    await waitFor(() => {
      expect(mocks.createOrganization).toHaveBeenCalledWith({
        name: "Placeholder New",
      });
    });
  });

  it("refetches the list", async () => {
    await open(<ListProbe />);
    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalledTimes(1);
    });

    // The new record belongs wherever the sort, the filter and the cursor put
    // it, so the page is fetched again rather than patched.
    mocks.listOrganizations.mockResolvedValue({ organizations: [CREATED] });
    type("Placeholder New");
    submitForm();

    await waitFor(() => {
      expect(screen.getByTestId("rows").textContent).toBe("1");
    });
  });

  it("fills the detail cache under the slug", async () => {
    await open(<DetailProbe idOrSlug={CREATED.slug} />);
    type("Placeholder New");
    submitForm();

    await waitFor(() => {
      expect(screen.getByTestId("detail").textContent).toBe(CREATED.name);
    });
    expect(mocks.getOrganization).not.toHaveBeenCalled();
  });
});

describe("an empty name", () => {
  it("does not reach the server", async () => {
    await open();
    submitForm();
    type("   ");
    submitForm();

    await act(async () => {
      await Promise.resolve();
    });
    expect(mocks.createOrganization).not.toHaveBeenCalled();
  });

  it("is marked required on the field", async () => {
    await open();
    // The submit is disabled until there is a name, and a disabled control is
    // no explanation. This is what a screen reader has to go on.
    expect(nameField().required).toBe(true);
  });

  it("leaves the submit disabled", async () => {
    await open();
    expect(submitButton().disabled).toBe(true);
    type("   ");
    expect(submitButton().disabled).toBe(true);
    type("Placeholder New");
    expect(submitButton().disabled).toBe(false);
  });
});

describe("cancelling", () => {
  it("gives the keyboard back to the trigger", async () => {
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    // Radix restores through the trigger, which is why this dialog needs no
    // focus rescue of its own. Restoration runs on a timeout, so this waits.
    await waitFor(() => {
      expect(document.activeElement).toBe(
        screen.getByRole("button", { name: "Create organization" }),
      );
    });
  });

  it("sends nothing", async () => {
    await open();
    type("Placeholder New");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    await act(async () => {
      await Promise.resolve();
    });
    // Cancel sits inside the form, so losing type="button" would make it
    // submit: the button that abandons the write would create the
    // organization.
    expect(mocks.createOrganization).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("a write in flight", () => {
  it("sends one request however many times it is submitted", async () => {
    const write = deferred();
    await open();
    type("Placeholder New");
    submitForm();

    await waitFor(() => {
      expect(mocks.createOrganization).toHaveBeenCalledTimes(1);
    });
    submitForm();
    submitForm();
    await act(async () => {
      await Promise.resolve();
    });
    expect(mocks.createOrganization).toHaveBeenCalledTimes(1);

    await act(async () => {
      write.resolve(CREATED);
    });
  });

  it("disables both buttons and the close control", async () => {
    const write = deferred();
    await open();
    type("Placeholder New");
    submitForm();

    await waitFor(() => {
      expect(submitButton().disabled).toBe(true);
    });
    expect(submitButton().textContent).toBe("Creating...");
    const cancel = screen.getByRole("button", { name: "Cancel" });
    expect((cancel as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole("button", { name: "Close" })).toBeNull();

    await act(async () => {
      write.resolve(CREATED);
    });
  });

  it("does not close on Escape", async () => {
    const write = deferred();
    await open();
    type("Placeholder New");
    submitForm();
    await waitFor(() => {
      expect(mocks.createOrganization).toHaveBeenCalledTimes(1);
    });

    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.queryByRole("dialog")).not.toBeNull();

    await act(async () => {
      write.resolve(CREATED);
    });
  });
});

describe("a refusal", () => {
  const REASON =
    "this server has no WorkOS configuration, so it cannot create organizations";

  it("stays open holding the reason and the name", async () => {
    mocks.createOrganization.mockRejectedValue(refusal(REASON));
    await open();
    type("Placeholder New");
    submitForm();

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe(REASON);
    expect(screen.queryByRole("dialog")).not.toBeNull();
    // A rejected name is one the operator wants to edit, not retype.
    expect(nameField().value).toBe("Placeholder New");
  });

  it("marks the field invalid and points it at the reason", async () => {
    mocks.createOrganization.mockRejectedValue(refusal(REASON));
    await open();
    type("Placeholder New");
    submitForm();

    const alert = await screen.findByRole("alert");
    const field = nameField();
    expect(field.getAttribute("aria-invalid")).toBe("true");
    // The operator tabs back to edit the rejected name, and the alert has
    // already fired by then.
    expect(field.getAttribute("aria-describedby")).toBe(alert.id);
    expect(alert.id).toBeTruthy();
  });

  it("reports nothing as created", async () => {
    mocks.createOrganization.mockRejectedValue(refusal(REASON));
    await open();
    type("Placeholder New");
    submitForm();

    await screen.findByRole("alert");
    expect(screen.queryByText(/^Created /)).toBeNull();
    expect(announce).not.toHaveBeenCalled();
  });

  it("can be submitted again after an edit", async () => {
    mocks.createOrganization.mockRejectedValueOnce(refusal(REASON));
    await open();
    type("Placeholder New");
    submitForm();
    await screen.findByRole("alert");

    type("Placeholder Newer");
    submitForm();

    await waitFor(() => {
      expect(mocks.createOrganization).toHaveBeenLastCalledWith({
        name: "Placeholder Newer",
      });
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("does not sit beside the previous create's confirmation", async () => {
    await open();
    type("Placeholder New");
    submitForm();
    await screen.findByText(/Created Placeholder New\./);

    mocks.createOrganization.mockRejectedValue(refusal(REASON));
    fireEvent.click(
      screen.getByRole("button", { name: "Create organization" }),
    );
    await screen.findByRole("dialog");

    // Opening the dialog again is the operator saying the last create is
    // done with. Leaving it up would put a success and a refusal on screen
    // together.
    expect(screen.queryByText(/Created Placeholder New\./)).toBeNull();
  });

  it("is not still showing when the dialog is reopened", async () => {
    mocks.createOrganization.mockRejectedValue(refusal(REASON));
    await open();
    type("Placeholder New");
    submitForm();
    await screen.findByRole("alert");

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    fireEvent.click(
      screen.getByRole("button", { name: "Create organization" }),
    );
    await screen.findByRole("dialog");

    expect(screen.queryByRole("alert")).toBeNull();
    expect(nameField().value).toBe("");
  });
});
