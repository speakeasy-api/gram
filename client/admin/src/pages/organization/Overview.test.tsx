import { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationQuery } from "@/lib/adminQueries";
import { routeTree } from "@/routeTree.gen";
import { anOrganization } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getSession: mocks.getSession,
    getOrganization: mocks.getOrganization,
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
    mocks.getOrganization.mockResolvedValue({ ...ORG, created_at: created });

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    await screen.findByText("Created");
    // The zone really moved, and in it this instant is the 15th locally.
    expect(new Date(created).getDate()).toBe(15);
    expect(valueBeside("Created").textContent).toBe(shortDate(created));
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
