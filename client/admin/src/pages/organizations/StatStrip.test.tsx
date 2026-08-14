import type { AnyRouter } from "@tanstack/react-router";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AdminOrganization,
  AdminOrganizationStats,
  ListOrganizationsParams,
  ListOrganizationsResult,
} from "@/lib/gramAdminApi";
import { routeTree } from "@/routeTree.gen";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  listOrganizations:
    vi.fn<
      (params?: ListOrganizationsParams) => Promise<ListOrganizationsResult>
    >(),
  // Takes whatever it is handed: React Query passes a queryFn its own context.
  getOrganizationStats:
    vi.fn<(...args: unknown[]) => Promise<AdminOrganizationStats>>(),
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listOrganizationMembers: vi.fn(),
}));

// Every endpoint the route reaches, so nothing here leaves a request in flight.
vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    listOrganizations: mocks.listOrganizations,
    getOrganizationStats: mocks.getOrganizationStats,
    getSession: mocks.getSession,
    getOrganization: mocks.getOrganization,
    listOrganizationProjects: mocks.listOrganizationProjects,
    listOrganizationMembers: mocks.listOrganizationMembers,
  };
});

// All distinct and all past a thousand: a misread field, or one rendered raw,
// is then a number no assertion here accepts.
const STATS: AdminOrganizationStats = {
  total: 48213,
  created_last_7_days: 1372,
  trials_ending_soon: 926,
  disabled: 5904,
  disabled_last_7_days: 1064,
};

const ORGS: AdminOrganization[] = [
  {
    id: "org_placeholder_one",
    name: "Placeholder One",
    slug: "placeholder-one",
    account_type: "pro",
    whitelisted: false,
    trial_state: "ending_soon",
    trial_ends_at: "2026-05-06T00:00:00Z",
    member_count: 3,
    created_at: "2026-01-02T00:00:00Z",
    updated_at: "2026-01-07T00:00:00Z",
  },
];

// Spoken, never painted: each cell ends with a sentence naming what pressing
// it does.
const ACTION: Record<string, string> = {
  Organizations: "Show every organization",
  "Trials ending in 7 days": "Show the trials ending in 7 days",
  Disabled: "Show the disabled organizations",
};

function figure(value: number): string {
  return value.toLocaleString();
}

function strip(): HTMLElement {
  return screen.getByRole("group", { name: "Platform totals" });
}

function cell(label: string): HTMLElement {
  return within(strip()).getByRole("button", {
    name: new RegExp(`^${label}\\b`),
  });
}

function currentSearch(router: AnyRouter): string {
  return decodeURIComponent(router.state.location.searchStr);
}

function urlFor(search: Record<string, unknown>): string {
  const qs = new URLSearchParams();
  for (const [key, value] of Object.entries(search)) {
    qs.set(key, typeof value === "string" ? value : JSON.stringify(value));
  }
  return `/organizations?${qs.toString()}`;
}

function lastListParams(): ListOrganizationsParams {
  const call = mocks.listOrganizations.mock.calls.at(-1);
  return call?.[0] ?? {};
}

async function renderList(initialPath = "/organizations"): Promise<AnyRouter> {
  const { router } = await renderRouteTree(routeTree, { initialPath });
  await waitFor(() => {
    expect(within(strip()).getByText(figure(STATS.total))).toBeTruthy();
  });
  return router;
}

beforeEach(() => {
  mocks.listOrganizations.mockReset();
  mocks.listOrganizations.mockResolvedValue({ organizations: ORGS });
  mocks.getOrganizationStats.mockReset();
  mocks.getOrganizationStats.mockResolvedValue(STATS);
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(ORGS[0]);
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
  mocks.listOrganizationMembers.mockReset();
  mocks.listOrganizationMembers.mockResolvedValue({ members: [] });
});

afterEach(cleanup);

describe("organizations stat strip figures", () => {
  it("reads each cell out of the field the contract names for it", async () => {
    await renderList();

    expect(cell("Organizations").textContent).toBe(
      `Organizations${figure(STATS.total)}${figure(STATS.created_last_7_days)} new this week${ACTION["Organizations"]}`,
    );
    expect(cell("Trials ending in 7 days").textContent).toBe(
      `Trials ending in 7 days${figure(STATS.trials_ending_soon)}${ACTION["Trials ending in 7 days"]}`,
    );
    expect(cell("Disabled").textContent).toBe(
      `Disabled${figure(STATS.disabled)}${figure(STATS.disabled_last_7_days)} this week${ACTION["Disabled"]}`,
    );
  });

  it("leaves the middle cell without a second line", async () => {
    await renderList();

    const trials = cell("Trials ending in 7 days");
    expect(within(trials).queryByText(/this week/)).toBeNull();
    expect(trials.querySelectorAll("span").length).toBe(3);
    expect(cell("Organizations").querySelectorAll("span").length).toBe(4);
  });

  it("renders a figure of zero as a figure, not as a missing one", async () => {
    mocks.getOrganizationStats.mockResolvedValue({ ...STATS, disabled: 0 });

    await renderList();

    expect(cell("Disabled").textContent).toBe(
      `Disabled0${figure(STATS.disabled_last_7_days)} this week${ACTION["Disabled"]}`,
    );
  });

  it("stands up to a payload that left a field out", async () => {
    const { disabled_last_7_days: _dropped, ...partial } = STATS;
    mocks.getOrganizationStats.mockResolvedValue(
      partial as AdminOrganizationStats,
    );

    await renderList();

    expect(cell("Disabled").textContent).toBe(
      `Disabled${figure(STATS.disabled)}— this week${ACTION["Disabled"]}`,
    );
    expect(cell("Organizations").textContent).toContain(figure(STATS.total));
  });

  it("says nothing where the figures have not arrived", async () => {
    let settle: (stats: AdminOrganizationStats) => void = () => {};
    mocks.getOrganizationStats.mockReturnValue(
      new Promise((resolve) => {
        settle = resolve;
      }),
    );

    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    expect(cell("Organizations").textContent).toBe(
      `Organizations—${ACTION["Organizations"]}`,
    );
    expect(cell("Disabled").textContent).toBe(`Disabled—${ACTION["Disabled"]}`);

    settle(STATS);
    await waitFor(() => {
      expect(cell("Organizations").textContent).toContain(figure(STATS.total));
    });
  });

  it("says the totals failed rather than showing a figure for them", async () => {
    mocks.getOrganizationStats.mockRejectedValue(new Error("stats are down"));

    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    // The reason as well as the headline: a banner that drops it tells the
    // operator something failed and nothing about what.
    await screen.findByText(
      /Could not load the platform totals: stats are down/,
    );
    // The cells too: a failure falling back to a figure would put three zeroes
    // above a table full of rows.
    expect(cell("Organizations").textContent).toBe(
      `Organizations—${ACTION["Organizations"]}`,
    );
    expect(cell("Trials ending in 7 days").textContent).toBe(
      `Trials ending in 7 days—${ACTION["Trials ending in 7 days"]}`,
    );
    expect(cell("Disabled").textContent).toBe(`Disabled—${ACTION["Disabled"]}`);
  });

  it("says nothing about a failure that did not happen", async () => {
    await renderList();

    expect(screen.queryByText(/Could not load the platform totals/)).toBeNull();
  });

  it("still filters from a cell whose figure never arrived", async () => {
    mocks.getOrganizationStats.mockRejectedValue(new Error("stats are down"));

    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    fireEvent.click(cell("Disabled"));

    await waitFor(() => {
      expect(currentSearch(router)).toContain('disabled=["disabled"]');
    });
  });

  it("asks for the totals under a key that holds none of the list's parameters", async () => {
    await renderList(urlFor({ q: "acme", type: ["pro"], trial: ["running"] }));

    expect(mocks.getOrganizationStats).toHaveBeenCalled();
    for (const [context] of mocks.getOrganizationStats.mock.calls) {
      expect((context as { queryKey: unknown }).queryKey).toEqual([
        "gram-admin-organization-stats",
      ]);
    }
  });
});

describe("organizations stat strip navigation", () => {
  it("opens every organization from the first cell, disabled ones included", async () => {
    const router = await renderList(
      urlFor({ type: ["pro"], trial: ["running"], disabled: ["disabled"] }),
    );

    fireEvent.click(cell("Organizations"));

    // Spelled out, because an absent status filter is not "no filter": the
    // server reads it as active only.
    await waitFor(() => {
      expect(currentSearch(router)).toBe('?disabled=["active","disabled"]');
    });
    await waitFor(() => {
      expect(lastListParams().disabled_states).toEqual(["active", "disabled"]);
    });
    expect(lastListParams().account_types).toBeUndefined();
    expect(lastListParams().trial_states).toBeUndefined();
  });

  it("filters to the trials the middle cell counted, at either status", async () => {
    const router = await renderList();

    fireEvent.click(cell("Trials ending in 7 days"));

    // The figure counts `ending_soon` over every organization, so the list it
    // opens has to ask for both statuses to reach the same rows.
    await waitFor(() => {
      expect(currentSearch(router)).toBe(
        '?trial=["ending_soon"]&disabled=["active","disabled"]',
      );
    });
    await waitFor(() => {
      expect(lastListParams().trial_states).toEqual(["ending_soon"]);
    });
    expect(lastListParams().disabled_states).toEqual(["active", "disabled"]);
  });

  it("filters to disabled organizations from the last cell", async () => {
    const router = await renderList();

    fireEvent.click(cell("Disabled"));

    await waitFor(() => {
      expect(currentSearch(router)).toBe('?disabled=["disabled"]');
    });
    await waitFor(() => {
      expect(lastListParams().disabled_states).toEqual(["disabled"]);
    });
  });

  it("replaces the filters that were set rather than adding to them", async () => {
    const router = await renderList(
      urlFor({ type: ["pro"], trial: ["running"] }),
    );

    fireEvent.click(cell("Disabled"));

    await waitFor(() => {
      expect(currentSearch(router)).toBe('?disabled=["disabled"]');
    });
    await waitFor(() => {
      expect(lastListParams().disabled_states).toEqual(["disabled"]);
    });
    expect(lastListParams().account_types).toBeUndefined();
    expect(lastListParams().trial_states).toBeUndefined();
  });

  it("drops the search term the figure never counted", async () => {
    const router = await renderList(urlFor({ q: "acme", type: ["pro"] }));

    fireEvent.click(cell("Trials ending in 7 days"));

    await waitFor(() => {
      expect(lastListParams().trial_states).toEqual(["ending_soon"]);
    });
    expect(lastListParams().q).toBeUndefined();
    expect(currentSearch(router)).not.toContain("q=");
    // The box too, or the term is gone from the request and still on screen.
    await waitFor(() => {
      expect(
        (screen.getByLabelText("Search organizations") as HTMLInputElement)
          .value,
      ).toBe("");
    });
  });

  it("returns to the first page even where the filters do not change", async () => {
    mocks.listOrganizations.mockResolvedValue({
      organizations: ORGS,
      next_cursor: "cursor_page_two",
    });
    await renderList(urlFor({ disabled: ["disabled"] }));

    const next = screen.getByRole("button", { name: "Next" });
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });
    fireEvent.click(next);
    await waitFor(() => {
      expect(lastListParams().cursor).toBe("cursor_page_two");
    });

    // The set this cell applies is the set already applied, so nothing in the
    // URL moves and the pager has to be told rather than notice.
    fireEvent.click(cell("Disabled"));

    await waitFor(() => {
      expect(lastListParams().cursor).toBeUndefined();
    });
  });

  it("turns Previous off with the page it sends the operator back to", async () => {
    mocks.listOrganizations.mockResolvedValue({
      organizations: ORGS,
      next_cursor: "cursor_page_two",
    });
    await renderList(urlFor({ disabled: ["disabled"] }));

    const next = screen.getByRole("button", { name: "Next" });
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });
    fireEvent.click(next);
    await waitFor(() => {
      expect(lastListParams().cursor).toBe("cursor_page_two");
    });

    fireEvent.click(cell("Disabled"));
    await waitFor(() => {
      expect(lastListParams().cursor).toBeUndefined();
    });
    // Enabled again, so the rows on screen are the first page rather than the
    // one before it. Previous is disabled off the pages behind, not off this.
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });

    // Page one has nothing behind it. Left on, Previous would go forwards.
    expect(
      screen.getByRole("button", { name: "Previous" }).hasAttribute("disabled"),
    ).toBe(true);
  });

  it("names the whole platform on the status control rather than counting it", async () => {
    await renderList();

    fireEvent.click(cell("Organizations"));

    // "2 selected" reads as a narrowing, and "Active only" would be false.
    await waitFor(() => {
      expect(
        screen
          .getByRole("button", { name: /^Status filter:/ })
          .getAttribute("aria-label"),
      ).toBe("Status filter: Active and disabled");
    });
  });

  it("shows the applied filter on the control that opens the sheet", async () => {
    await renderList(urlFor({ trial: ["running"] }));

    fireEvent.click(cell("Disabled"));

    await waitFor(() => {
      expect(
        screen
          .getByRole("button", { name: /^Status filter:/ })
          .getAttribute("aria-label"),
      ).toContain("Disabled");
    });
    expect(
      screen
        .getByRole("button", { name: /^Trial filter:/ })
        .getAttribute("aria-label"),
    ).toContain("All trial states");
  });

  it("shows the strip's own filter inside the sheet it opens", async () => {
    await renderList();

    fireEvent.click(cell("Trials ending in 7 days"));
    await waitFor(() => {
      expect(lastListParams().trial_states).toEqual(["ending_soon"]);
    });

    fireEvent.click(screen.getByRole("button", { name: /^Trial filter:/ }));
    const sheet = await screen.findByRole("dialog");

    expect(
      within(sheet).getByRole("combobox", { name: /^Trial/ }).textContent,
    ).toContain("Ending soon");
  });
});

describe("organizations stat strip and the table's filters", () => {
  it("does not fetch again when the operator filters the table", async () => {
    const router = await renderList();
    const before = mocks.getOrganizationStats.mock.calls.length;
    expect(before).toBeGreaterThan(0);

    await router.navigate({
      to: "/organizations",
      search: { trial: ["running"] },
    });

    await waitFor(() => {
      expect(lastListParams().trial_states).toEqual(["running"]);
    });
    expect(mocks.getOrganizationStats.mock.calls.length).toBe(before);
  });

  it("holds its figures still while the table reloads under a filter", async () => {
    const router = await renderList();

    // A second answer, so a strip that did refetch would visibly move.
    mocks.getOrganizationStats.mockResolvedValue({
      total: 1,
      created_last_7_days: 1,
      trials_ending_soon: 1,
      disabled: 1,
      disabled_last_7_days: 1,
    });

    await router.navigate({
      to: "/organizations",
      search: { disabled: ["disabled"] },
    });
    await waitFor(() => {
      expect(lastListParams().disabled_states).toEqual(["disabled"]);
    });

    expect(cell("Organizations").textContent).toContain(figure(STATS.total));
    expect(cell("Disabled").textContent).toContain(figure(STATS.disabled));
  });

  it("keeps the figures the same across a click of its own", async () => {
    const router = await renderList();

    fireEvent.click(cell("Disabled"));
    await waitFor(() => {
      expect(lastListParams().disabled_states).toEqual(["disabled"]);
    });

    expect(currentSearch(router)).toBe('?disabled=["disabled"]');
    expect(cell("Organizations").textContent).toContain(figure(STATS.total));
    expect(cell("Trials ending in 7 days").textContent).toContain(
      figure(STATS.trials_ending_soon),
    );
  });
});

describe("organizations stat strip placement", () => {
  it("sits outside the table's scroll box, and before it in the document", async () => {
    await renderList();

    const scrollBox = screen.getByRole("region", {
      name: "Organizations table",
    });
    // Tree facts only. happy-dom lays nothing out, so that the strip does not
    // scroll away with the rows is checked by eye and stated in the pull
    // request, not here.
    expect(scrollBox.contains(strip())).toBe(false);
    expect(
      strip().compareDocumentPosition(scrollBox) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("deals the three figures out in the order the design names", async () => {
    await renderList();

    // Document order: every other assertion here finds a cell by its label.
    expect(
      within(strip())
        .getAllByRole("button")
        .map((control) => control.querySelector("span")?.textContent),
    ).toEqual(["Organizations", "Trials ending in 7 days", "Disabled"]);
  });

  it("offers each figure as a control the keyboard reaches", async () => {
    await renderList();

    for (const label of [
      "Organizations",
      "Trials ending in 7 days",
      "Disabled",
    ]) {
      const control = cell(label);
      // A div with an onClick answers no key; a bare button in a form submits.
      expect(control.tagName).toBe("BUTTON");
      expect(control.getAttribute("type")).toBe("button");
    }
  });
});

describe("organizations stat strip accessible names", () => {
  it("speaks the figure, not only what it counts", async () => {
    await renderList();

    // The computed name: an aria-label added later would drop the figure.
    for (const name of [
      `Organizations ${figure(STATS.total)} ${figure(STATS.created_last_7_days)} new this week ${ACTION["Organizations"]}`,
      `Trials ending in 7 days ${figure(STATS.trials_ending_soon)} ${ACTION["Trials ending in 7 days"]}`,
      `Disabled ${figure(STATS.disabled)} ${figure(STATS.disabled_last_7_days)} this week ${ACTION["Disabled"]}`,
    ]) {
      expect(within(strip()).getByRole("button", { name })).toBeTruthy();
    }
  });

  it("says what pressing does without painting it", async () => {
    await renderList();

    const spoken = within(cell("Disabled")).getByText(ACTION["Disabled"] ?? "");
    expect(spoken.className).toContain("sr-only");
  });

  it("leaves the name to come from the contents", async () => {
    await renderList();

    for (const label of [
      "Organizations",
      "Trials ending in 7 days",
      "Disabled",
    ]) {
      expect(cell(label).getAttribute("aria-label")).toBeNull();
      expect(cell(label).getAttribute("aria-labelledby")).toBeNull();
    }
  });

  it("says the figures are on their way while they are in flight", async () => {
    let settle: (stats: AdminOrganizationStats) => void = () => {};
    mocks.getOrganizationStats.mockReturnValue(
      new Promise((resolve) => {
        settle = resolve;
      }),
    );

    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    expect(cell("Organizations").getAttribute("aria-busy")).toBe("true");

    settle(STATS);
    await waitFor(() => {
      expect(cell("Organizations").getAttribute("aria-busy")).toBe("false");
    });
  });
});
