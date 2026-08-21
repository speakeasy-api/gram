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
  organizationQuery,
  organizationsListQuery,
  organizationsStatsQuery,
} from "@/lib/adminQueries";
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
  };
});

const ORG = anOrganization({
  account_type: "pro",
  whitelisted: true,
  // The stale pair, dated apart from the real trial on purpose. A page back on
  // `free_trial_ends_at` then shows the wrong date rather than the right one
  // by coincidence.
  free_trial_started_at: "2026-02-01T00:00:00Z",
  free_trial_ends_at: "2026-11-12T00:00:00Z",
  trial_state: "running",
  trial_ends_at: "2026-05-06T00:00:00Z",
});

// The trial is a date without a clock wherever it is read. UTC, because that is
// the zone the API states these dates in and the zone they are rendered in;
// see `utils.test.ts`.
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

// The labels of one group's rows, in the order they are drawn. Read off the
// group's heading, so a row that moves out of a group fails the group it left
// as well as the one it joined.
function labelsIn(group: string): string[] {
  const section = screen
    .getByRole("heading", { name: group })
    .closest("section");
  if (!section) throw new Error(`the ${group} heading is not in a group`);
  return [...section.querySelectorAll('[data-slot="field-label"]')].map(
    (n) => n.textContent ?? "",
  );
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
});

afterEach(() => {
  cleanup();
  vi.unstubAllEnvs();
});

describe("Overview", () => {
  it("reads the trial exactly the way the row does", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    const trialEndsAt = ORG.trial_ends_at;
    if (!trialEndsAt) throw new Error("the record under test needs a trial");

    const trial = await screen.findByText("Trial");
    // Written out, not built from the component. The same string is asserted
    // against the list's cell, which is the only way two pages that could
    // drift apart are held together.
    expect(valueBeside("Trial").textContent).toBe(
      `Running ends ${shortDate(trialEndsAt)}`,
    );
    expect(
      trial.parentElement?.querySelector('[data-slot="badge"]'),
    ).toBeTruthy();

    // The defaulted pair is gone from the page, not merely unread: an operator
    // who sees "Free trial ends" reads a date no organization ever earned.
    expect(screen.queryByText("Free trial started")).toBeNull();
    expect(screen.queryByText("Free trial ends")).toBeNull();
  });

  it("reads a dash for an organization that never trialled", async () => {
    mocks.getOrganization.mockResolvedValue({
      ...ORG,
      trial_state: "none",
      trial_ends_at: undefined,
    });
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByText("Trial");
    // `free_trial_ends_at` still dates this record, which is the whole reason
    // the page was moved off it.
    expect(ORG.free_trial_ends_at).toBeTruthy();
    expect(valueBeside("Trial").textContent).toBe("-No trial");
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

    await screen.findByText("Created");
    // The zone really moved, and in it this instant is the 15th locally.
    expect(new Date(created).getDate()).toBe(15);
    // Every date the view renders, not one of them: each row is a separate
    // call and the rule holds for all of them or for none.
    expect(valueBeside("Created").textContent).toBe(shortDate(created));
    expect(valueBeside("Updated").textContent).toBe(shortDate(updated));
    expect(valueBeside("Disabled at").textContent).toBe(shortDate(disabled));
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

    mocks.updateOrganization.mockResolvedValue({
      ...ORG,
      account_type: "enterprise",
    });
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
    mocks.updateOrganization.mockResolvedValue(saved);

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

  it("draws the facts in three named groups", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByRole("heading", { name: "Identity" });
    // Written out rather than counted: the label an operator reads is the
    // whole contract of a fact row, and "Whitelisted" in particular must not
    // drift to a name that reads like a preference. See S2-FACTS-AUDIT.md.
    expect(labelsIn("Identity")).toEqual([
      "Name",
      "Slug",
      "Organization id",
      "WorkOS id",
      "Created",
      "Updated",
    ]);
    expect(labelsIn("Plan")).toEqual(["Account type", "Trial"]);
    expect(labelsIn("Access")).toEqual(["Whitelisted", "Disabled at"]);
  });

  it("nests the group headings under the record's own heading", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    // The record names itself at level 4, which is the level the whole admin
    // app titles a page at. A group drawn above that level is read as a
    // sibling of the record rather than a part of it.
    expect(
      await screen.findByRole("heading", { level: 4, name: ORG.name }),
    ).toBeTruthy();
    for (const group of ["Identity", "Plan", "Access"]) {
      expect(screen.getByRole("heading", { name: group }).tagName).toBe("H5");
    }
  });

  it("no longer counts the members the record nav already counts", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByRole("heading", { name: "Identity" });
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
      ["Copy Organization id", identified.id],
      ["Copy WorkOS id", identified.workos_id],
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

  it("offers no copy control over an absent WorkOS id", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByText("WorkOS id");
    expect(ORG.workos_id).toBeUndefined();
    // A button that copies "-" is worse than no button.
    expect(valueBeside("WorkOS id").textContent).toBe("-");
    expect(screen.queryByRole("button", { name: "Copy WorkOS id" })).toBeNull();
  });

  it("renders a dash for a record that was never disabled", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByText("Disabled at");
    expect(valueBeside("Disabled at").textContent).toBe("-");
  });
});
