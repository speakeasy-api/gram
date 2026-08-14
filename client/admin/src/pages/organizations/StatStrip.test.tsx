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
  // Takes whatever it is handed: React Query calls a queryFn with its own
  // context, and the key on that context is what the test below reads.
  getOrganizationStats:
    vi.fn<(...args: unknown[]) => Promise<AdminOrganizationStats>>(),
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listOrganizationMembers: vi.fn(),
}));

// Every endpoint the organizations route reaches, so nothing in this file
// leaves a real request in flight. The rest of the module stays real: the query
// layer's own key building is part of what is under test here.
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

// Five distinct figures, none of them a multiple or a sum of another. A cell
// that reads the wrong field then renders a number no assertion here accepts,
// which is the whole reason not to reuse a value across two fields.
const STATS: AdminOrganizationStats = {
  total: 4821,
  created_last_7_days: 137,
  trials_ending_soon: 26,
  disabled: 59,
  disabled_last_7_days: 8,
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

// The figures are rendered through toLocaleString, so the expected text comes
// out of the same formatter. The thousands separator is not what is under test;
// which field reached the cell is.
function figure(value: number): string {
  return value.toLocaleString();
}

function strip(): HTMLElement {
  return screen.getByRole("group", { name: "Platform totals" });
}

// By the accessible name, which the cell's own rendered text produces: there is
// no aria-label standing in for it, so a cell that painted nothing cannot be
// found here at all.
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

// The strip renders as soon as the query settles, and every assertion below is
// about what it says once it has.
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

    // Whole text, not a substring match: a cell that rendered its label and no
    // number, or two numbers where the design has one, fails here. Every figure
    // in STATS is distinct, so each of these is a statement about which field
    // the cell read.
    expect(cell("Organizations").textContent).toBe(
      `Organizations${figure(STATS.total)}${figure(STATS.created_last_7_days)} new this week`,
    );
    expect(cell("Trials ending in 7 days").textContent).toBe(
      `Trials ending in 7 days${figure(STATS.trials_ending_soon)}`,
    );
    expect(cell("Disabled").textContent).toBe(
      `Disabled${figure(STATS.disabled)}${figure(STATS.disabled_last_7_days)} this week`,
    );
  });

  it("leaves the middle cell without a second line", async () => {
    await renderList();

    const trials = cell("Trials ending in 7 days");
    // The design's "N with no owner" is cut, and a placeholder for it is cut
    // too. Two assertions, because the wording of a sub-line nobody has written
    // yet is not knowable: one names the shape the other two cells use, and one
    // counts the lines.
    expect(within(trials).queryByText(/this week/)).toBeNull();
    expect(trials.querySelectorAll("span").length).toBe(2);
    expect(cell("Organizations").querySelectorAll("span").length).toBe(3);
  });

  it("says nothing where the figures have not arrived", async () => {
    // A number that has not been fetched must not be shown as a number, and a
    // cell must not be blank either: an operator reading a blank strip has no
    // way to tell it from a platform with nothing on it.
    let settle: (stats: AdminOrganizationStats) => void = () => {};
    mocks.getOrganizationStats.mockReturnValue(
      new Promise((resolve) => {
        settle = resolve;
      }),
    );

    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    expect(cell("Organizations").textContent).toBe("Organizations—");
    expect(cell("Disabled").textContent).toBe("Disabled—");

    settle(STATS);
    await waitFor(() => {
      expect(cell("Organizations").textContent).toContain(figure(STATS.total));
    });
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
      // The cache key React Query fetched under. A key that carried the term or
      // the filters is a separate entry per view, which is a strip that
      // refetches with the table and reports the filtered rows.
      expect((context as { queryKey: unknown }).queryKey).toEqual([
        "gram-admin-organization-stats",
      ]);
    }
  });
});

describe("organizations stat strip navigation", () => {
  it("clears every filter from the first cell", async () => {
    const router = await renderList(
      urlFor({ type: ["pro"], trial: ["running"], disabled: ["disabled"] }),
    );

    fireEvent.click(cell("Organizations"));

    await waitFor(() => {
      expect(currentSearch(router)).toBe("");
    });
    await waitFor(() => {
      expect(lastListParams().account_types).toBeUndefined();
    });
    expect(lastListParams().trial_states).toBeUndefined();
    expect(lastListParams().disabled_states).toBeUndefined();
  });

  it("filters to the trials the middle cell counted", async () => {
    const router = await renderList();

    fireEvent.click(cell("Trials ending in 7 days"));

    // `ending_soon`, which is the state the figure above it counts. Any other
    // trial state would be a cell whose count and whose rows disagree.
    await waitFor(() => {
      expect(currentSearch(router)).toBe('?trial=["ending_soon"]');
    });
    await waitFor(() => {
      expect(lastListParams().trial_states).toEqual(["ending_soon"]);
    });
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

    // Only the filter the cell names. A cell that merged would leave the type
    // and the trial in place, and the rows would be the disabled pro
    // organizations whose trial is running, which is not the figure clicked.
    await waitFor(() => {
      expect(currentSearch(router)).toBe('?disabled=["disabled"]');
    });
    await waitFor(() => {
      expect(lastListParams().disabled_states).toEqual(["disabled"]);
    });
    expect(lastListParams().account_types).toBeUndefined();
    expect(lastListParams().trial_states).toBeUndefined();
  });

  it("leaves the search term alone", async () => {
    // The term is not one of the three filters, and an operator who clicked a
    // figure has not asked to type it again.
    const router = await renderList(urlFor({ q: "acme", type: ["pro"] }));

    fireEvent.click(cell("Trials ending in 7 days"));

    await waitFor(() => {
      expect(lastListParams().trial_states).toEqual(["ending_soon"]);
    });
    expect(lastListParams().q).toBe("acme");
    expect(currentSearch(router)).toContain("q=acme");
    expect(
      (screen.getByLabelText("Search organizations") as HTMLInputElement).value,
    ).toBe("acme");
  });

  it("shows the applied filter on the control that opens the sheet", async () => {
    await renderList(urlFor({ trial: ["running"] }));

    fireEvent.click(cell("Disabled"));

    // The URL is the state, so the sheet's own triggers have to follow a write
    // the strip made. A strip that navigated some other way would leave these
    // reading the filter the operator just replaced.
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

    // The label the picker shows, not the value the URL carries: the two are
    // different words, and the operator only ever reads the first.
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
    // The figures describe the platform. A strip keyed on the filters would
    // refetch here and start reporting the rows already on screen.
    expect(mocks.getOrganizationStats.mock.calls.length).toBe(before);
  });

  it("holds its figures still while the table reloads under a filter", async () => {
    const router = await renderList();

    // A second answer, so a strip that did refetch would visibly move rather
    // than merely call the endpoint again.
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
  it("paints above the table rather than inside the box that scrolls", async () => {
    await renderList();

    const scrollBox = screen.getByRole("region", {
      name: "Organizations table",
    });
    // Outside the scroll box, and before it in the document. A strip inside it
    // would scroll away with the rows it leads to.
    expect(scrollBox.contains(strip())).toBe(false);
    expect(
      strip().compareDocumentPosition(scrollBox) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("offers each figure as a control the keyboard reaches", async () => {
    await renderList();

    for (const label of [
      "Organizations",
      "Trials ending in 7 days",
      "Disabled",
    ]) {
      const control = cell(label);
      // A div with an onClick is not reachable by Tab and answers no key. The
      // type matters too: a bare <button> inside a form submits it.
      expect(control.tagName).toBe("BUTTON");
      expect(control.getAttribute("type")).toBe("button");
    }
  });

  it("filters from the keyboard as well as the pointer", async () => {
    const router = await renderList();

    const control = cell("Trials ending in 7 days");
    control.focus();
    expect(document.activeElement).toBe(control);
    // A real button answers Enter and Space by firing a click, which is what
    // happy-dom's click dispatch stands in for here.
    fireEvent.click(control);

    await waitFor(() => {
      expect(currentSearch(router)).toBe('?trial=["ending_soon"]');
    });
  });
});
