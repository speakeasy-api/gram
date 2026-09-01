import { QueryClient } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  organizationActivityQuery,
  organizationQuery,
  organizationsListQuery,
  organizationsStatsQuery,
} from "@/lib/adminQueries";
import { GramAdminError, type AdminOrganization } from "@/lib/gramAdminApi";
import { routeTree } from "@/routeTree.gen";
import { anOrganization } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listOrganizations: vi.fn(),
  getOrganizationStats: vi.fn(),
  updateOrganization: vi.fn(),
  markEnterpriseTrialConverted: vi.fn(),
  toastSuccess: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess },
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getSession: mocks.getSession,
    getOrganization: mocks.getOrganization,
    listOrganizationProjects: mocks.listOrganizationProjects,
    listOrganizations: mocks.listOrganizations,
    getOrganizationStats: mocks.getOrganizationStats,
    updateOrganization: mocks.updateOrganization,
    markEnterpriseTrialConverted: mocks.markEnterpriseTrialConverted,
  };
});

const ORG = anOrganization({
  account_type: "pro",
  whitelisted: true,
  trial_state: "running",
  trial_ends_at: "2099-05-06T00:00:00Z",
  trial_tier: "enterprise",
});

// Most record dates use the shared UTC date formatter.
function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { timeZone: "UTC" });
}

// The field rows carry no role, so a test reaches one by its label and takes
// the value beside it.
function valueBeside(label: string): HTMLElement {
  const value = screen.getByText(label).nextElementSibling;
  if (!(value instanceof HTMLElement)) {
    throw new Error(`the ${label} row has no value beside it`);
  }
  return value;
}

function panelNamed(name: string): HTMLElement {
  const panel = screen.getByRole("heading", { name }).closest("section, aside");
  if (!(panel instanceof HTMLElement)) {
    throw new Error(`the ${name} heading is not in a panel`);
  }
  return panel;
}

// Opens the account type select and picks an option, and hands back the
// trigger so a test can read what the control says afterwards.
async function pickAccountType(option: string): Promise<HTMLElement> {
  const select = await screen.findByRole("combobox");
  fireEvent.keyDown(select, { key: "ArrowDown" });
  fireEvent.click(await screen.findByRole("option", { name: option }));
  return select;
}

async function confirmDialog(): Promise<void> {
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
  await waitFor(() => {
    expect(screen.queryByRole("dialog")).toBeNull();
  });
}

// The body of the one write under test. Read off the mock rather than asserted
// through `toHaveBeenCalledWith`, so a test can name the keys it must not have.
function payloadOf(mock: { mock: { calls: unknown[][] } }): object {
  const [first] = mock.mock.calls;
  if (!first) throw new Error("no write was made");
  return first[0] as object;
}

// Both controls are buttons under the hood, so the attribute is the answer.
// This suite has no jest-dom matchers.
function isDisabled(control: HTMLElement): boolean {
  return control.hasAttribute("disabled");
}

// `RecordLayout`'s live region, the one place a write reported from inside a
// dialog reaches a screen reader.
function liveRegion(): HTMLElement {
  const region = document.querySelector('[aria-live="polite"]');
  if (!(region instanceof HTMLElement)) {
    throw new Error("the record has no live region");
  }
  return region;
}

const writeText = vi.fn<(text: string) => Promise<void>>(() =>
  Promise.resolve(),
);

beforeEach(() => {
  writeText.mockClear();
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
    writable: true,
  });
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(ORG);
  // Not this view's query: the record nav in the sidebar asks for it on every
  // view. Unmocked it reaches the real fetch and the suite waits on a socket.
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
  mocks.listOrganizations.mockReset();
  mocks.getOrganizationStats.mockReset();
  mocks.updateOrganization.mockReset();
  mocks.markEnterpriseTrialConverted.mockReset();
  mocks.toastSuccess.mockReset();
  mocks.markEnterpriseTrialConverted.mockResolvedValue({
    organization_id: ORG.id,
    converted_at: "2026-03-08T12:34:56Z",
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllEnvs();
});

describe("Overview", () => {
  it("matches the approved active-trial hierarchy", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    const trialEndsAt = ORG.trial_ends_at;
    if (!trialEndsAt) throw new Error("the record under test needs a trial");

    await screen.findByRole("heading", { name: "Active trial" });
    const trial = panelNamed("Active trial");
    const detailsPanel = panelNamed("Details");
    const details = within(detailsPanel);
    expect(within(trial).getByText("RUNNING")).toBeTruthy();
    expect(within(trial).getByText(/days left$/)).toBeTruthy();
    expect(trial.textContent).toContain(
      `End date ${new Date(trialEndsAt).toLocaleDateString()} · Enterprise tier`,
    );
    expect(within(trial).queryByText("State")).toBeNull();
    expect(
      details.getByText("Trial state").nextElementSibling?.textContent,
    ).toBe("Running");
    expect(
      details.getByText("Trial tier").nextElementSibling?.textContent,
    ).toBe("Enterprise");
    expect(details.getByText("End date").nextElementSibling?.textContent).toBe(
      new Date(trialEndsAt).toLocaleDateString(),
    );
    const trialFacts =
      details.getByText("Trial state").parentElement?.parentElement;
    expect(trialFacts?.className).toContain("border-t");
    expect(trialFacts?.className).toContain("mt-5");

    const buttons = within(trial).getAllByRole("button");
    const convert = within(trial).getByRole("button", {
      name: `Mark ${ORG.name} as converted`,
    });
    const extend = within(trial).getByRole("button", {
      name: `Extend trial for ${ORG.name}`,
    });
    expect(buttons.indexOf(convert)).toBeLessThan(buttons.indexOf(extend));
    expect(convert.className).toContain("w-full");
    expect(extend.className).toContain("w-full");
    expect(trial.textContent).toContain(
      "Converting keeps enterprise access and closes the trial.",
    );
    expect(
      details.getByText(`Updated ${shortDate(ORG.updated_at)}`),
    ).toBeTruthy();
    expect(details.getByText("Platform access, no demo gate")).toBeTruthy();
    expect(details.queryByText("Created")).toBeNull();
    expect(details.queryByText("Disabled at")).toBeNull();
    const recordTitle = screen.getByRole("heading", {
      level: 4,
      name: ORG.name,
    });
    expect(recordTitle.parentElement?.textContent).not.toContain("Running");

    expect(screen.queryByText("Free trial started")).toBeNull();
    expect(screen.queryByText("Free trial ends")).toBeNull();
  });

  it("hides the trial panel for an organization that never trialled", async () => {
    mocks.getOrganization.mockResolvedValue({
      ...ORG,
      trial_state: "none",
      trial_ends_at: undefined,
    });
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByRole("heading", { name: "Details" });
    expect(
      screen.queryByRole("heading", { name: "Enterprise trial" }),
    ).toBeNull();
  });

  it("reads a date as the server's day, not the reader's", async () => {
    // The reader's zone is moved for the length of the render. Node re-reads TZ
    // when it next builds a Date. Written out rather than left to the machine:
    // CI runs in UTC, where a local-zone read renders the same day as a UTC one
    // and this assertion would pass without meaning anything. See utils.test.ts.
    vi.stubEnv("TZ", "America/Los_Angeles");

    // Early in the UTC day, which is where the fault shows.
    const created = "2026-01-16T03:00:00Z";
    const updated = "2026-03-04T02:00:00Z";
    const disabled = "2026-04-09T01:00:00Z";
    mocks.getOrganization.mockResolvedValue({
      ...ORG,
      created_at: created,
      updated_at: updated,
      disabled_at: disabled,
    });

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByText(`Updated ${shortDate(updated)}`);
    // The zone really moved, and in it this instant is the 15th locally.
    expect(new Date(created).getDate()).toBe(15);
    expect(screen.getByText(`Created ${shortDate(created)}`)).toBeTruthy();
    expect(panelNamed("Danger zone").textContent).toContain(
      `Disabled ${shortDate(disabled)}`,
    );
  });

  it("leaves the control reading the record while the dialog asks", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    const select = await pickAccountType("enterprise");
    await screen.findByRole("dialog");

    // Honest: the record has not changed yet, and the dialog carries the words
    // for what is about to change. A control that ran ahead of the write would
    // read as saved to an operator who then cancels.
    expect(select.textContent).toBe(ORG.account_type);
    expect(ORG.account_type).not.toBe("enterprise");
  });

  it("names the change from the old value to the new one", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await pickAccountType("enterprise");
    const dialog = await screen.findByRole("dialog");

    expect(within(dialog).getByRole("heading").textContent).toBe(
      `Update ${ORG.name}?`,
    );
    // Written out with both values in order. A description built backwards
    // still names the right two words and asks the operator to approve the
    // opposite of what will be written.
    expect(dialog.textContent).toContain(
      `Account type: ${ORG.account_type} → enterprise`,
    );
    expect(dialog.textContent).not.toContain(
      `Account type: enterprise → ${ORG.account_type}`,
    );
  });

  it("names the whitelisted change as yes and no, in that order", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    fireEvent.click(await screen.findByRole("switch"));
    const dialog = await screen.findByRole("dialog");

    // The record under test is whitelisted, so the change reads yes → no. Both
    // halves are asserted: a description that renders the boolean backwards, or
    // one that names the two states the wrong way round, still reads like a
    // sentence and asks the operator to approve the opposite of the write.
    expect(ORG.whitelisted).toBe(true);
    expect(dialog.textContent).toContain("Whitelisted: yes → no");
    expect(dialog.textContent).not.toContain("Whitelisted: no → yes");
  });

  it("does not run a new write under the last one's failure", async () => {
    mocks.updateOrganization.mockRejectedValueOnce(new Error("update failed"));
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    fireEvent.click(await screen.findByRole("switch"));
    await confirmDialog();
    await waitFor(() => {
      expect(document.querySelector(".text-destructive")?.textContent).toMatch(
        /update failed/,
      );
    });

    const saved = { ...ORG, account_type: "enterprise" };
    mocks.updateOrganization.mockResolvedValue(saved);
    mocks.getOrganization.mockResolvedValue(saved);
    await pickAccountType("enterprise");
    await confirmDialog();

    // A failure left standing over a write that landed tells the operator the
    // change they just watched succeed did not happen.
    await waitFor(() => {
      expect(screen.getByRole("combobox").textContent).toBe("enterprise");
    });
    expect(document.querySelector(".text-destructive")).toBeNull();
  });

  it("keeps a failure showing when the next change is cancelled", async () => {
    mocks.updateOrganization.mockRejectedValue(new Error("update failed"));
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    fireEvent.click(await screen.findByRole("switch"));
    await confirmDialog();
    await waitFor(() => {
      expect(document.querySelector(".text-destructive")?.textContent).toMatch(
        /update failed/,
      );
    });

    await pickAccountType("enterprise");
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    // Only a write clears the failure, and a cancelled change is not a write.
    // Clearing it here would take away the only account of a failure the
    // operator may not have read yet, in exchange for nothing happening.
    expect(document.querySelector(".text-destructive")?.textContent).toMatch(
      /update failed/,
    );
    expect(mocks.updateOrganization).toHaveBeenCalledTimes(1);
  });

  it("writes the account type on its own, with no whitelisted field", async () => {
    mocks.updateOrganization.mockResolvedValue({
      ...ORG,
      account_type: "enterprise",
    });
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await pickAccountType("enterprise");
    await confirmDialog();

    await waitFor(() => {
      expect(mocks.updateOrganization).toHaveBeenCalledTimes(1);
    });
    // The keys, not just the values. `toHaveBeenCalledWith` treats an absent
    // key and a key set to `undefined` as the same object, so a write that
    // sends `whitelisted: undefined` alongside passes a loose assertion while
    // the endpoint reads it as a field the operator never touched.
    expect(payloadOf(mocks.updateOrganization)).toStrictEqual({
      id: ORG.id,
      account_type: "enterprise",
    });
    expect(Object.keys(payloadOf(mocks.updateOrganization)).sort()).toEqual([
      "account_type",
      "id",
    ]);
  });

  it("writes whitelisted on its own, with no account type field", async () => {
    mocks.updateOrganization.mockResolvedValue({ ...ORG, whitelisted: false });
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    fireEvent.click(await screen.findByRole("switch"));
    await confirmDialog();

    // The field by name, not merely that a write happened: the two editors
    // write one record between them, and a switch that lands on the account
    // type changes the plan of an organization nobody meant to touch.
    await waitFor(() => {
      expect(mocks.updateOrganization).toHaveBeenCalledTimes(1);
    });
    expect(payloadOf(mocks.updateOrganization)).toStrictEqual({
      id: ORG.id,
      whitelisted: false,
    });
    expect(Object.keys(payloadOf(mocks.updateOrganization)).sort()).toEqual([
      "id",
      "whitelisted",
    ]);
  });

  it("writes nothing when the operator cancels, and the control snaps back", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    const select = await pickAccountType("enterprise");
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(mocks.updateOrganization).not.toHaveBeenCalled();
    // No revert code wrote this back. The control reads `org`, so a change
    // nobody confirmed was never anywhere but the dialog's words.
    expect(select.textContent).toBe(ORG.account_type);

    fireEvent.click(screen.getByRole("switch"));
    const second = await screen.findByRole("dialog");
    fireEvent.click(within(second).getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(mocks.updateOrganization).not.toHaveBeenCalled();
    expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe(
      String(ORG.whitelisted),
    );
  });

  it("shows a saved change on a record the operator reached by id", async () => {
    const saved = { ...ORG, account_type: "enterprise" };
    mocks.updateOrganization.mockImplementation(async () => {
      mocks.getOrganization.mockResolvedValue(saved);
      return saved;
    });

    // By id, which is how the list opens a record whose slug it has not got.
    // The record is written back under one address only, so the other one keeps
    // serving the record as it was before the write.
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.id}`,
    });

    await pickAccountType("enterprise");
    await confirmDialog();

    // An operator who watched the save land and then reads the old value back
    // has no way to tell a stale page from a write that did not happen.
    await waitFor(() => {
      expect(screen.getByRole("combobox").textContent).toBe("enterprise");
    });
    expect(mocks.getOrganization.mock.calls.length).toBeGreaterThan(1);
    expect(ORG.account_type).not.toBe("enterprise");
  });

  it("announces a write that lands", async () => {
    mocks.updateOrganization.mockResolvedValue({
      ...ORG,
      account_type: "enterprise",
    });
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await pickAccountType("enterprise");
    await confirmDialog();

    // Spoken, because the record repaints in place and a screen reader is told
    // nothing by a select whose text changed.
    await waitFor(() => {
      expect(liveRegion().textContent).toContain(
        `${ORG.name} updated. Account type: ${ORG.account_type} → enterprise.`,
      );
    });
  });

  it("keeps a saved change through a list read that was already in flight", async () => {
    const saved = { ...ORG, account_type: "enterprise" };
    mocks.updateOrganization.mockResolvedValue(saved);

    // Held open, so its answer can be made to land after the write instead of
    // before it. It carries the row as it was, which is what makes it a lie.
    let answerListRead = () => {};
    mocks.listOrganizations.mockImplementation(
      () =>
        new Promise((resolve) => {
          answerListRead = () => resolve({ organizations: [ORG] });
        }),
    );

    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    qc.setQueryData(organizationQuery(ORG.slug).queryKey, ORG);
    qc.setQueryData(organizationsListQuery().queryKey, {
      organizations: [ORG],
    });

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
      queryClient: qc,
    });

    // The ordinary window, not an exotic one: the list query sets no staleTime,
    // so returning to the tab starts this read and the operator's next press
    // lands while it is still open.
    //
    // `fetchQuery`, not `refetchQueries`, which defaults to active queries only
    // and would start nothing at all from this route.
    void qc.fetchQuery(organizationsListQuery()).catch(() => {});
    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });

    await pickAccountType("enterprise");
    await confirmDialog();
    await waitFor(() => {
      expect(mocks.updateOrganization).toHaveBeenCalled();
    });

    // Now, after the write. React Query commits whatever a request answers
    // whenever it lands, so an uncancelled read undoes the write here.
    answerListRead();
    await waitFor(() => {
      expect(
        qc.getQueryState(organizationsListQuery().queryKey)?.fetchStatus,
      ).toBe("idle");
    });

    const row = qc.getQueryData(organizationsListQuery().queryKey)
      ?.organizations[0];
    expect(row?.account_type).toBe("enterprise");
    expect(ORG.account_type).not.toBe("enterprise");
  });

  it("asks for the totals again when the save fails", async () => {
    mocks.updateOrganization.mockRejectedValue(new Error("update failed"));

    // Held open, so the cancel in `onMutate` has a real read to drop. Without
    // one there is nothing for the failed write to owe the strip.
    let answerStatsRead = () => {};
    mocks.getOrganizationStats.mockImplementation(
      () =>
        new Promise((resolve) => {
          answerStatsRead = () =>
            resolve({
              total: 1,
              created_last_7_days: 0,
              customers: 0,
              customers_created_last_7_days: 0,
              trials_ending_soon: 0,
              disabled: 0,
              disabled_last_7_days: 0,
            });
        }),
    );

    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    qc.setQueryData(organizationQuery(ORG.slug).queryKey, ORG);
    qc.setQueryData(organizationsStatsQuery.queryKey, {
      total: 2,
      created_last_7_days: 0,
      customers: 0,
      customers_created_last_7_days: 0,
      trials_ending_soon: 0,
      disabled: 0,
      disabled_last_7_days: 0,
    });

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
      queryClient: qc,
    });

    void qc.fetchQuery(organizationsStatsQuery).catch(() => {});
    await waitFor(() => {
      expect(mocks.getOrganizationStats).toHaveBeenCalled();
    });

    await pickAccountType("enterprise");
    await confirmDialog();

    // Shown as well as spoken. The save bar carried this line and went with the
    // draft, so without the reporter a failed write is silent on the page.
    await waitFor(() => {
      expect(document.querySelector(".text-destructive")?.textContent).toMatch(
        /Could not update .*update failed/,
      );
    });
    expect(liveRegion().textContent).toContain("update failed");
    answerStatsRead();

    // Cancelled and replaced by nothing: a failed write repaints no record, so
    // the strip would hold totals from before the save until a refocus or a
    // remount asked again.
    await waitFor(() => {
      const state = qc.getQueryState(organizationsStatsQuery.queryKey);
      expect(state?.fetchStatus).toBe("idle");
      expect(state?.isInvalidated).toBe(true);
    });
  });

  it("does not carry an open dialog to the next record", async () => {
    const other = anOrganization({
      id: "org_2",
      name: "Second Org",
      slug: "second-org",
    });
    // Both records are already in the cache, which is the state the list
    // navigates from: `useOpenOrganization` writes the record before it moves.
    // Without the seed the second record arrives pending, the layout paints its
    // loading state, and the unmount that follows closes the dialog for reasons
    // that have nothing to do with the record it belonged to.
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    qc.setQueryData(organizationQuery(ORG.slug).queryKey, ORG);
    qc.setQueryData(organizationQuery(other.slug).queryKey, other);

    const { router } = await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
      queryClient: qc,
    });

    fireEvent.click(await screen.findByRole("switch"));
    expect((await screen.findByRole("dialog")).textContent).toContain(ORG.name);

    await router.navigate({
      to: "/organizations/$idOrSlug",
      params: { idOrSlug: other.slug },
    });

    expect(
      await screen.findByRole("heading", { name: other.name }),
    ).toBeTruthy();
    // The question belonged to the record it was asked about. Carried over, it
    // offers to write one organization's change against another.
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(mocks.updateOrganization).not.toHaveBeenCalled();
  });

  it.each([
    ["running enterprise trial", { ...ORG, trial_state: "running" }, true],
    ["ending enterprise trial", { ...ORG, trial_state: "ending_soon" }, true],
    ["expired enterprise trial", { ...ORG, trial_state: "expired" }, true],
    ["demoted enterprise trial", { ...ORG, trial_state: "demoted" }, true],
    [
      "disabled enterprise trial",
      { ...ORG, disabled_at: "2026-03-01T00:00:00Z" },
      true,
    ],
    [
      "enterprise trial with an enterprise account type",
      { ...ORG, account_type: "enterprise" },
      true,
    ],
    [
      "enterprise trial with a free account type",
      { ...ORG, account_type: "free" },
      true,
    ],
    ["non-enterprise trial", { ...ORG, trial_tier: "pro" }, false],
    [
      "already converted timestamp",
      { ...ORG, trial_converted_at: "2026-03-01T00:00:00Z" },
      false,
    ],
    ["converted state", { ...ORG, trial_state: "converted" }, false],
    ["no trial state", { ...ORG, trial_state: "none" }, false],
  ] as const)(
    "offers conversion only for a %s",
    async (_case, org, eligible) => {
      mocks.getOrganization.mockResolvedValue(org);
      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${org.slug}`,
      });

      await screen.findByRole("heading", { name: "Details" });
      const control = screen.queryByRole("button", {
        name: `Mark ${org.name} as converted`,
      });
      expect(Boolean(control)).toBe(eligible);
    },
  );

  it.each([
    [
      "running",
      ORG,
      "This action ends the enterprise trial and prevents automatic demotion. Use this action only after the enterprise contract is confirmed.",
      undefined,
    ],
    [
      "ending soon",
      { ...ORG, trial_state: "ending_soon" as const },
      "This action ends the enterprise trial and prevents automatic demotion. Use this action only after the enterprise contract is confirmed.",
      undefined,
    ],
    [
      "expired",
      { ...ORG, trial_state: "expired" as const },
      "The trial period has ended, but demotion is not complete. This action prevents demotion and keeps enterprise access.",
      undefined,
    ],
    [
      "demoted",
      { ...ORG, trial_state: "demoted" as const },
      "This action records the enterprise contract and restores enterprise access. Model provider keys with an admin lock or billing restriction remain disabled.",
      undefined,
    ],
    [
      "disabled",
      { ...ORG, disabled_at: "2026-03-01T00:00:00Z" },
      "This action ends the enterprise trial and prevents automatic demotion. Use this action only after the enterprise contract is confirmed.",
      "The organization remains disabled after conversion.",
    ],
  ] as const)(
    "renders AGE-3149 confirmation copy for a %s trial",
    async (_case, org, stateCopy, disabledCopy) => {
      mocks.getOrganization.mockResolvedValue(org);
      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${ORG.slug}`,
      });

      fireEvent.click(
        await screen.findByRole("button", {
          name: `Mark ${org.name} as converted`,
        }),
      );
      const dialog = await screen.findByRole("dialog");
      expect(within(dialog).getByRole("heading").textContent).toBe(
        "Mark trial as converted?",
      );
      expect(dialog.textContent).toContain(stateCopy);
      if (disabledCopy) expect(dialog.textContent).toContain(disabledCopy);
    },
  );

  it("returns focus to the conversion opener when the operator cancels", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });
    const opener = await screen.findByRole("button", {
      name: `Mark ${ORG.name} as converted`,
    });
    fireEvent.click(opener);
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Cancel",
      }),
    );

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(document.activeElement).toBe(opener);
    expect(mocks.markEnterpriseTrialConverted).not.toHaveBeenCalled();
  });

  it("invalidates detail, list, stats, and activity, refetches canonically, and never caches the narrow result as an organization", async () => {
    const converted = {
      ...ORG,
      trial_state: "converted" as const,
      trial_converted_at: "2026-03-08T12:34:56Z",
    };
    mocks.getOrganization
      .mockResolvedValueOnce(ORG)
      .mockResolvedValue(converted);
    const qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    qc.setQueryData(organizationsListQuery().queryKey, {
      organizations: [ORG],
      next_cursor: undefined,
    });
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
      queryClient: qc,
    });
    invalidate.mockClear();

    fireEvent.click(
      await screen.findByRole("button", {
        name: `Mark ${ORG.name} as converted`,
      }),
    );
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Mark as converted",
      }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    const invalidatedKeys = invalidate.mock.calls.map(
      ([filters]) => filters?.queryKey,
    );
    expect(invalidatedKeys).toContainEqual(organizationsListQuery().queryKey);
    expect(invalidatedKeys).toContainEqual(["gram-admin-organization"]);
    expect(invalidatedKeys).toContainEqual(organizationsStatsQuery.queryKey);
    expect(invalidatedKeys).toContainEqual(
      organizationActivityQuery(ORG.id).queryKey,
    );
    expect(mocks.getOrganization).toHaveBeenCalledWith(ORG.id);
    expect(qc.getQueryData(organizationQuery(ORG.id).queryKey)).toEqual(
      converted,
    );
    expect(qc.getQueryData(organizationsListQuery().queryKey)).toEqual({
      organizations: [ORG],
      next_cursor: undefined,
    });
  });

  it.each([
    [
      "running",
      ORG,
      "Enterprise access remains active. Conversion details are available on the Activity page.",
    ],
    [
      "ending soon",
      { ...ORG, trial_state: "ending_soon" as const },
      "Enterprise access remains active. Conversion details are available on the Activity page.",
    ],
    [
      "expired",
      { ...ORG, trial_state: "expired" as const },
      "Enterprise access remains active. Conversion details are available on the Activity page.",
    ],
    [
      "demoted",
      { ...ORG, trial_state: "demoted" as const },
      "Enterprise access was restored. Conversion details are available on the Activity page.",
    ],
    [
      "disabled",
      {
        ...ORG,
        trial_state: "demoted" as const,
        disabled_at: "2026-03-01T00:00:00Z",
      },
      "The organization remains disabled. Conversion details are available on the Activity page.",
    ],
  ] as const)(
    "shows AGE-3149 conversion success feedback for a %s trial",
    async (_case, org, description) => {
      const converted = {
        ...org,
        account_type: "enterprise",
        whitelisted: true,
        trial_state: "converted" as const,
        trial_converted_at: "2026-03-08T12:34:56Z",
      };
      mocks.getOrganization.mockImplementation((idOrSlug: string) =>
        Promise.resolve(idOrSlug === ORG.id ? converted : org),
      );
      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${ORG.slug}`,
      });

      fireEvent.click(
        await screen.findByRole("button", {
          name: `Mark ${org.name} as converted`,
        }),
      );
      fireEvent.click(
        within(await screen.findByRole("dialog")).getByRole("button", {
          name: "Mark as converted",
        }),
      );

      await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
      expect(mocks.toastSuccess).toHaveBeenCalledWith(
        "Trial marked as converted",
        { description },
      );
    },
  );

  it("keeps the whole operation visibly locked after POST success until canonical reconciliation settles", async () => {
    const converted = {
      ...ORG,
      trial_state: "converted" as const,
      trial_converted_at: "2026-03-08T12:34:56Z",
    };
    let settleRefresh!: (value: AdminOrganization) => void;
    const refresh = new Promise<AdminOrganization>((resolve) => {
      settleRefresh = resolve;
    });
    mocks.getOrganization.mockResolvedValueOnce(ORG).mockReturnValue(refresh);
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    const opener = await screen.findByRole("button", {
      name: `Mark ${ORG.name} as converted`,
    });
    fireEvent.click(opener);
    const dialog = await screen.findByRole("dialog");
    const submit = within(dialog).getByRole("button", {
      name: "Mark as converted",
    });
    fireEvent.click(submit);
    fireEvent.click(submit);
    await waitFor(() => expect(mocks.getOrganization).toHaveBeenCalledTimes(2));

    expect(mocks.markEnterpriseTrialConverted).toHaveBeenCalledTimes(1);
    const pendingSubmit = within(dialog).getByRole("button", {
      name: "Marking as converted…",
    });
    expect(isDisabled(pendingSubmit)).toBe(true);
    expect(pendingSubmit.getAttribute("aria-busy")).toBe("true");
    expect(
      pendingSubmit.querySelector('[data-slot="conversion-spinner"]'),
    ).toBeTruthy();
    expect(
      isDisabled(within(dialog).getByRole("button", { name: "Cancel" })),
    ).toBe(true);
    expect(isDisabled(opener)).toBe(true);
    expect(within(dialog).queryByRole("button", { name: "Close" })).toBeNull();
    fireEvent.keyDown(document, { key: "Escape" });
    const overlay = document.querySelector('[data-slot="dialog-overlay"]');
    if (!(overlay instanceof HTMLElement))
      throw new Error("dialog has no overlay");
    fireEvent.pointerDown(overlay);
    fireEvent.click(overlay);
    fireEvent.click(submit);
    expect(screen.getByRole("dialog")).toBe(dialog);
    expect(mocks.markEnterpriseTrialConverted).toHaveBeenCalledTimes(1);

    settleRefresh(converted);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(document.activeElement).toBe(
      screen.getByRole("heading", { name: "Details" }),
    );
  });

  it.each([
    [400, "Bad Request"],
    [401, "Unauthorized"],
    [403, "Forbidden"],
    [404, "Not Found"],
    [422, "Unprocessable Entity"],
  ] as const)(
    "closes and restores the opener for a pre-commit %s error",
    async (status, label) => {
      mocks.markEnterpriseTrialConverted.mockRejectedValue(
        new GramAdminError(
          status,
          { message: label },
          `gram admin ${status} ${label}`,
        ),
      );
      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${ORG.slug}`,
      });
      const opener = await screen.findByRole("button", {
        name: `Mark ${ORG.name} as converted`,
      });
      fireEvent.click(opener);
      fireEvent.click(
        within(await screen.findByRole("dialog")).getByRole("button", {
          name: "Mark as converted",
        }),
      );

      await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
      expect(document.activeElement).toBe(opener);
      expect(liveRegion().textContent).toContain(label);
      expect(mocks.toastSuccess).not.toHaveBeenCalled();
    },
  );

  it("treats a stale concurrent 409 as potentially committed, locks through truth reconciliation, and retries idempotently", async () => {
    const converted = {
      ...ORG,
      account_type: "enterprise",
      whitelisted: true,
      trial_state: "converted" as const,
      trial_converted_at: "2026-03-08T12:34:56Z",
    };
    let settleRefresh!: (value: AdminOrganization) => void;
    const refresh = new Promise<AdminOrganization>((resolve) => {
      settleRefresh = resolve;
    });
    mocks.getOrganization
      .mockResolvedValueOnce(ORG)
      .mockImplementation((idOrSlug: string) =>
        idOrSlug === ORG.slug ? refresh : Promise.resolve(converted),
      );
    mocks.markEnterpriseTrialConverted
      .mockRejectedValueOnce(
        new GramAdminError(
          409,
          { message: "converted access is not normalized" },
          "gram admin 409 Conflict",
        ),
      )
      .mockResolvedValueOnce({
        organization_id: ORG.id,
        converted_at: converted.trial_converted_at,
      });
    const qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    qc.setQueryData(organizationsListQuery().queryKey, {
      organizations: [ORG],
      next_cursor: undefined,
    });
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
      queryClient: qc,
    });
    invalidate.mockClear();

    fireEvent.click(
      await screen.findByRole("button", {
        name: `Mark ${ORG.name} as converted`,
      }),
    );
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Mark as converted",
      }),
    );

    const dialog = await screen.findByRole("dialog");
    const pendingRetry = await within(dialog).findByRole("button", {
      name: "Marking as converted…",
    });
    expect(dialog.textContent).toContain("may already be recorded");
    expect(isDisabled(pendingRetry)).toBe(true);
    expect(pendingRetry.getAttribute("aria-busy")).toBe("true");
    expect(
      isDisabled(within(dialog).getByRole("button", { name: "Cancel" })),
    ).toBe(true);
    fireEvent.keyDown(document, { key: "Escape" });
    const overlay = document.querySelector('[data-slot="dialog-overlay"]');
    if (!(overlay instanceof HTMLElement))
      throw new Error("dialog has no overlay");
    fireEvent.pointerDown(overlay);
    fireEvent.click(overlay);
    fireEvent.click(pendingRetry);
    expect(screen.getByRole("dialog")).toBe(dialog);
    expect(mocks.markEnterpriseTrialConverted).toHaveBeenCalledTimes(1);

    settleRefresh(converted);
    const retry = await within(dialog).findByRole("button", { name: "Retry" });
    expect(isDisabled(retry)).toBe(false);
    expect(
      screen.queryByRole("heading", { name: "Enterprise trial" }),
    ).toBeNull();
    const invalidatedKeys = invalidate.mock.calls.map(
      ([filters]) => filters?.queryKey,
    );
    expect(invalidatedKeys).toContainEqual(organizationsListQuery().queryKey);
    expect(invalidatedKeys).toContainEqual(["gram-admin-organization"]);
    expect(invalidatedKeys).toContainEqual(organizationsStatsQuery.queryKey);
    expect(invalidatedKeys).toContainEqual(
      organizationActivityQuery(ORG.id).queryKey,
    );
    expect(qc.getQueryData(organizationQuery(ORG.id).queryKey)).toEqual(
      converted,
    );
    expect(qc.getQueryData(organizationsListQuery().queryKey)).toEqual({
      organizations: [ORG],
      next_cursor: undefined,
    });

    fireEvent.click(retry);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(mocks.markEnterpriseTrialConverted).toHaveBeenCalledTimes(2);
    expect(mocks.toastSuccess).toHaveBeenCalledTimes(1);
    expect(document.activeElement).toBe(
      screen.getByRole("heading", { name: "Details" }),
    );
  });

  it("keeps an ambiguous post-commit error open, refetches truth, and safely retries after the panel unmounts", async () => {
    const original = { ...ORG, trial_state: "demoted" as const };
    const converted = {
      ...original,
      trial_state: "converted" as const,
      trial_converted_at: "2026-03-08T12:34:56Z",
    };
    mocks.getOrganization
      .mockResolvedValueOnce(original)
      .mockResolvedValue(converted);
    mocks.markEnterpriseTrialConverted
      .mockRejectedValueOnce(new Error("provider unavailable"))
      .mockResolvedValueOnce({
        organization_id: ORG.id,
        converted_at: converted.trial_converted_at,
      });
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });
    fireEvent.click(
      await screen.findByRole("button", {
        name: `Mark ${ORG.name} as converted`,
      }),
    );
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Mark as converted",
      }),
    );

    const dialog = await screen.findByRole("dialog");
    await waitFor(() => {
      expect(dialog.textContent).toContain("may already be recorded");
      expect(liveRegion().textContent).toContain("may already be recorded");
      expect(dialog.textContent).toContain(
        "records the enterprise contract and restores enterprise access",
      );
      expect(
        screen.queryByRole("heading", { name: "Enterprise trial" }),
      ).toBeNull();
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(mocks.markEnterpriseTrialConverted).toHaveBeenCalledTimes(2);
    expect(document.activeElement).toBe(
      screen.getByRole("heading", { name: "Details" }),
    );
    expect(mocks.toastSuccess).toHaveBeenCalledTimes(1);
  });

  it.each(["running", "ending_soon", "expired", "demoted"] as const)(
    "shows the trial panel for a %s trial",
    async (trialState) => {
      mocks.getOrganization.mockResolvedValue({
        ...ORG,
        trial_state: trialState,
      });
      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${ORG.slug}`,
      });

      const heading =
        trialState === "running" || trialState === "ending_soon"
          ? "Active trial"
          : "Enterprise trial";
      expect(
        await screen.findByRole("heading", { name: heading }),
      ).toBeTruthy();
    },
  );

  it.each(["none", "converted", undefined] as const)(
    "hides the trial panel for a %s trial",
    async (trialState) => {
      mocks.getOrganization.mockResolvedValue({
        ...ORG,
        trial_state: trialState,
      });
      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${ORG.slug}`,
      });

      await screen.findByRole("heading", { name: "Details" });
      expect(
        screen.queryByRole("heading", { name: "Enterprise trial" }),
      ).toBeNull();
    },
  );

  it.each(["none", undefined] as const)(
    "keeps no-trial facts in Details for a %s trial state",
    async (trialState) => {
      mocks.getOrganization.mockResolvedValue({
        ...ORG,
        trial_state: trialState,
      });
      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${ORG.slug}`,
      });

      await screen.findByRole("heading", { name: "Details" });
      const details = panelNamed("Details");
      expect(within(details).getByText("No trial")).toBeTruthy();
      expect(screen.getAllByText("No trial")).toHaveLength(1);
      expect(
        screen.queryByRole("heading", { name: "Enterprise trial" }),
      ).toBeNull();
    },
  );

  it("keeps converted trial facts in Details without showing the side panel", async () => {
    const convertedAt = "2026-08-20T00:00:00Z";
    mocks.getOrganization.mockResolvedValue({
      ...ORG,
      trial_state: "converted",
      trial_converted_at: convertedAt,
    });
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByRole("heading", { name: "Details" });
    const details = panelNamed("Details");
    expect(
      within(details).getByText("Trial state").nextElementSibling?.textContent,
    ).toBe("Converted");
    expect(
      within(details).getByText("Conversion date").nextElementSibling
        ?.textContent,
    ).toBe(new Date(convertedAt).toLocaleDateString());
    expect(
      screen.queryByRole("heading", { name: "Enterprise trial" }),
    ).toBeNull();
  });

  it.each([
    [
      "enabled",
      ORG,
      "Every member loses access to Gram until the organization is re-enabled.",
      "Sessions end immediately; nothing is deleted.",
    ],
    [
      "disabled",
      { ...ORG, disabled_at: "2026-02-01T00:00:00Z" },
      "Re-enabling restores organization access for every member",
      "Model provider keys with admin, billing, or unknown disable causes remain disabled.",
    ],
  ] as const)(
    "explains the scoped impact for a %s organization in Danger zone",
    async (_state, org, impact, causeQualification) => {
      mocks.getOrganization.mockResolvedValue(org);
      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${org.slug}`,
      });

      await screen.findByRole("heading", { name: "Danger zone" });
      const danger = panelNamed("Danger zone");
      expect(danger.textContent).toContain(impact);
      expect(danger.textContent).toContain(causeQualification);
    },
  );

  it("scopes trial and lifecycle actions to their new panels", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByRole("heading", { name: "Active trial" });
    const trial = panelNamed("Active trial");
    const danger = panelNamed("Danger zone");
    expect(
      within(trial).getByRole("button", {
        name: `Extend trial for ${ORG.name}`,
      }),
    ).toBeTruthy();
    expect(
      within(trial).queryByRole("button", { name: `Disable ${ORG.name}` }),
    ).toBeNull();
    expect(
      within(danger).getByRole("button", { name: `Disable ${ORG.name}` }),
    ).toBeTruthy();
    expect(
      within(danger).queryByRole("button", {
        name: `Extend trial for ${ORG.name}`,
      }),
    ).toBeNull();
  });

  it("offers the existing re-arm action for a demoted trial", async () => {
    mocks.getOrganization.mockResolvedValue({ ...ORG, trial_state: "demoted" });
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByRole("heading", { name: "Enterprise trial" });
    expect(
      within(panelNamed("Enterprise trial")).getByRole("button", {
        name: `Re-arm trial for ${ORG.name}`,
      }),
    ).toBeTruthy();
  });

  it("offers only the conversion action for an expired trial", async () => {
    mocks.getOrganization.mockResolvedValue({ ...ORG, trial_state: "expired" });
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByRole("heading", { name: "Enterprise trial" });
    const trial = within(panelNamed("Enterprise trial"));
    expect(
      trial.getByRole("button", { name: `Mark ${ORG.name} as converted` }),
    ).toBeTruthy();
    expect(trial.queryByRole("button", { name: /Extend|Re-arm/ })).toBeNull();
  });

  it("wraps a wide Details and Danger zone column beside the trial panel", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByRole("heading", { name: "Details" });
    const details = panelNamed("Details");
    const danger = panelNamed("Danger zone");
    const trial = panelNamed("Active trial");
    const left = details.parentElement;
    const layout = left?.parentElement;

    expect(left).toBe(danger.parentElement);
    expect(layout).toBe(trial.parentElement);
    // jsdom does not calculate geometry. These stable slots and responsive
    // primitives are the strongest component-level contract; browser coverage
    // can measure the named regions at wide and narrow viewports without
    // coupling to incidental descendants.
    expect(layout?.dataset.slot).toBe("organization-overview");
    expect(left?.dataset.slot).toBe("organization-overview-main");
    expect(trial.dataset.slot).toBe("organization-overview-trial");
    expect(layout?.className).toContain("flex-wrap");
    expect(left?.className).toContain("min-w-[min(100%,32rem)]");
    expect(left?.className).toContain("flex-[2_1_32rem]");
    expect(trial.className).toContain("w-full");
    expect(trial.className).toContain("max-w-[21rem]");
    expect(trial.className).toContain("flex-[1_1_18rem]");
  });

  it("nests the panel headings under the record's own heading", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    expect(
      await screen.findByRole("heading", { level: 4, name: ORG.name }),
    ).toBeTruthy();
    for (const panel of ["Details", "Active trial", "Danger zone"]) {
      expect(screen.getByRole("heading", { name: panel }).tagName).toBe("H5");
    }
  });

  it("no longer counts the members the record nav already counts", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByRole("heading", { name: "Details" });
    // By label, not by text: the record nav says "Members" too, and that one
    // is the count this row was dropped in favour of.
    const labels = [
      ...document.querySelectorAll('[data-slot="field-label"]'),
    ].map((n) => n.textContent);
    expect(labels).not.toContain("Members");
  });

  it("raises no save bar over the record when a fact changes", async () => {
    mocks.updateOrganization.mockResolvedValue({ ...ORG, whitelisted: false });
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    fireEvent.click(await screen.findByRole("switch"));
    await confirmDialog();
    await waitFor(() => {
      expect(mocks.updateOrganization).toHaveBeenCalled();
    });

    // The record commits one fact at a time now. A bar left over the whole
    // record offers to write facts the operator never touched, and the dialog
    // it would raise could not name what it was about to save.
    const overview = screen
      .getByText("Whitelisted")
      .closest("section")?.parentElement;
    expect(overview).toBeTruthy();
    expect(
      within(overview!).queryByRole("button", { name: "Save" }),
    ).toBeNull();
    expect(
      within(overview!).queryByRole("button", { name: "Cancel" }),
    ).toBeNull();
  });

  it("returns focus to the control the dialog was opened from", async () => {
    mocks.updateOrganization.mockResolvedValue({ ...ORG, whitelisted: false });
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    // Radix restores focus to a `DialogTrigger`, and this dialog has none, so
    // both exits drop the keyboard on `document.body` unless the control is
    // focused back. The confirmed half of this test proves only that the
    // control ends up focused; it cannot see the browser's blur-on-disable, so
    // the real cover for that path is the two tests below.
    const select = await pickAccountType("enterprise");
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(document.activeElement).toBe(select);
    });

    const toggle = screen.getByRole("switch");
    fireEvent.click(toggle);
    await confirmDialog();
    await waitFor(() => {
      expect(document.activeElement).toBe(toggle);
    });
  });

  // The control is disabled while its write is in flight, and a browser drops
  // focus to the body when the focused element becomes disabled. jsdom does not:
  // it leaves focus where it was, so a restore that runs before the disable
  // passes here and fails in front of an operator. Both tests below blur the
  // control by hand to stand in for the browser, then require the write to put
  // the keyboard back.
  async function landsFocusBackAfter(settle: () => void): Promise<void> {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    const toggle = await screen.findByRole("switch");
    fireEvent.click(toggle);
    await confirmDialog();
    await waitFor(() => {
      expect(isDisabled(screen.getByRole("switch"))).toBe(true);
    });

    // What the browser does when a focused control becomes disabled.
    (document.activeElement as HTMLElement | null)?.blur();
    expect(document.activeElement).not.toBe(toggle);

    settle();
    await waitFor(() => {
      expect(isDisabled(screen.getByRole("switch"))).toBe(false);
    });
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole("switch"));
    });
  }

  it("puts the keyboard back on the control after a write lands", async () => {
    let landTheWrite = () => {};
    mocks.updateOrganization.mockImplementation(
      () =>
        new Promise((resolve) => {
          landTheWrite = () => resolve({ ...ORG, whitelisted: false });
        }),
    );

    await landsFocusBackAfter(() => landTheWrite());
  });

  it("puts the keyboard back on the control after a write fails", async () => {
    let failTheWrite = () => {};
    mocks.updateOrganization.mockImplementation(
      () =>
        new Promise((_resolve, reject) => {
          failTheWrite = () => reject(new Error("update failed"));
        }),
    );

    await landsFocusBackAfter(() => failTheWrite());
  });

  it("puts the keyboard back on the control on a second write", async () => {
    // Fast enough that neither write commits a pending render, which is the
    // case where one settled write looks exactly like the last one.
    mocks.updateOrganization.mockImplementation((body: { id: string }) =>
      Promise.resolve({ ...ORG, ...body }),
    );
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    const toggle = await screen.findByRole("switch");
    fireEvent.click(toggle);
    await confirmDialog();
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole("switch"));
    });

    // The second write is the one at issue: the first moves the write off
    // `idle`, and after that a repeat has nothing new to say about itself.
    (document.activeElement as HTMLElement | null)?.blur();
    fireEvent.click(screen.getByRole("switch"));
    await confirmDialog();
    await waitFor(() => {
      expect(mocks.updateOrganization).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole("switch"));
    });
  });

  it("disables both controls while a write is in flight", async () => {
    let landTheWrite = () => {};
    mocks.updateOrganization.mockImplementation(
      () =>
        new Promise((resolve) => {
          landTheWrite = () => resolve({ ...ORG, whitelisted: false });
        }),
    );
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    fireEvent.click(await screen.findByRole("switch"));
    await confirmDialog();

    // Two writes in flight against one record race each other into the cache,
    // and the loser's answer is the one that stays.
    await waitFor(() => {
      expect(isDisabled(screen.getByRole("switch"))).toBe(true);
    });
    expect(isDisabled(screen.getByRole("combobox"))).toBe(true);

    landTheWrite();
    await waitFor(() => {
      expect(isDisabled(screen.getByRole("switch"))).toBe(false);
    });
    expect(isDisabled(screen.getByRole("combobox"))).toBe(false);
  });

  it("copies each identifier off its own row", async () => {
    const identified = {
      ...ORG,
      workos_id: "org_workos_placeholder_identifier",
    };
    mocks.getOrganization.mockResolvedValue(identified);
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${identified.slug}`,
    });

    // Row by row, each against its own value. Three controls stand side by
    // side, and one wired to a neighbour hands the operator an identifier that
    // belongs to a different field of the same record, which reads as right.
    const rows: [string, string][] = [
      ["Copy Slug", identified.slug],
      ["Copy Organization ID", identified.id],
      ["Copy WorkOS org ID", identified.workos_id],
    ];
    for (const [name, value] of rows) {
      const control = await screen.findByRole("button", { name });
      fireEvent.click(control);
      await waitFor(() => {
        expect(writeText).toHaveBeenCalledWith(value);
      });
      expect(
        await screen.findByRole("button", {
          name: `${name.replace("Copy ", "")} copied`,
        }),
      ).toBeTruthy();
    }
    expect(writeText).toHaveBeenCalledTimes(rows.length);
    // The three values are told apart, so a control pointed at a neighbour
    // cannot pass by writing the same string twice.
    expect(new Set(rows.map(([, value]) => value)).size).toBe(rows.length);
  });

  it("offers no copy control over an absent WorkOS org ID", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByText("WorkOS org ID");
    expect(ORG.workos_id).toBeUndefined();
    // A button that copies "-" is worse than no button.
    expect(valueBeside("WorkOS org ID").textContent).toBe("-");
    expect(
      screen.queryByRole("button", { name: "Copy WorkOS org ID" }),
    ).toBeNull();
  });

  it("keeps disabled status in Danger zone rather than Details", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByRole("heading", { name: "Danger zone" });
    expect(screen.queryByText("Disabled at")).toBeNull();
  });
});
