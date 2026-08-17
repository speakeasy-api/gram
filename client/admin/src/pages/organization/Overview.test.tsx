import { QueryClient } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationQuery, organizationsListQuery } from "@/lib/adminQueries";
import { routeTree } from "@/routeTree.gen";
import { anOrganization } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listOrganizations: vi.fn(),
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

beforeEach(() => {
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

  it("shows the account type the operator picked, not the saved one", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    const select = await screen.findByRole("combobox");
    fireEvent.keyDown(select, { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: "enterprise" }));

    // A control that keeps reading the record shows the old value while Save
    // offers to write the new one, so the operator saves a change the page
    // never showed them.
    expect(select.textContent).toBe("enterprise");
    expect(ORG.account_type).not.toBe("enterprise");
    expect(screen.getByRole("button", { name: "Save" })).toBeTruthy();
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

    const select = await screen.findByRole("combobox");
    fireEvent.keyDown(select, { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: "enterprise" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    // Waited out rather than asserted straight away: while the write is in
    // flight the draft is still what the control reads, so it says "enterprise"
    // either way. Save leaving is the record answering for itself again.
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    });
    // An operator who watched the save land and then reads the old value back
    // has no way to tell a stale page from a write that did not happen.
    expect(screen.getByRole("combobox").textContent).toBe("enterprise");
    expect(ORG.account_type).not.toBe("enterprise");
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

    const select = await screen.findByRole("combobox");
    fireEvent.keyDown(select, { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: "enterprise" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
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

  it("does not carry an unsaved draft to the next record", async () => {
    const other = anOrganization({
      id: "org_2",
      name: "Second Org",
      slug: "second-org",
    });
    // Both records are already in the cache, which is the state the list
    // navigates from: `useOpenOrganization` writes the record before it moves.
    // Without the seed the second record arrives pending, the layout paints its
    // loading state, and the unmount that follows clears the draft for reasons
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
    expect(screen.getByRole("button", { name: "Save" })).toBeTruthy();

    await router.navigate({
      to: "/organizations/$idOrSlug",
      params: { idOrSlug: other.slug },
    });

    expect(
      await screen.findByRole("heading", { name: other.name }),
    ).toBeTruthy();
    // The edit belonged to the record that was open when it was made. Carrying
    // it over offers to save one organization's change against another.
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("renders a dash for a record that was never disabled", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByText("Disabled at");
    expect(valueBeside("Disabled at").textContent).toBe("-");
  });
});
