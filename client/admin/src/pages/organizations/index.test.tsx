import type { AnyRouter } from "@tanstack/react-router";
import {
  useTable,
  type ColumnVisibilityState,
  type Updater,
} from "@tanstack/react-table";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
  type Mock,
} from "vitest";

import type {
  AdminOrganization,
  ListOrganizationsParams,
  ListOrganizationsResult,
} from "@/lib/gramAdminApi";
import { useState, type JSX } from "react";

import { routeTree } from "@/routeTree.gen";
import {
  organizationsSearchSchema,
  type OrganizationsSearch,
} from "@/routes/organizations.index";
import { dataTableFeatures } from "@/components/data-table";
import { renderRouteTree } from "@/test/harness";

import { ORG_COLUMNS } from "./columns";
import { TableActionBar } from "./Toolbar";

const mocks = vi.hoisted(() => ({
  listOrganizations:
    vi.fn<
      (params?: ListOrganizationsParams) => Promise<ListOrganizationsResult>
    >(),
  getSession: vi.fn(),
}));

// Only the two endpoints this page's route tree reaches are replaced. The rest
// of the module stays real, so toSearchParams and omitUnset still decide what
// counts as an unset param.
vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    listOrganizations: mocks.listOrganizations,
    getSession: mocks.getSession,
  };
});

const ORGS: AdminOrganization[] = [
  {
    id: "org_placeholder_one",
    name: "Placeholder One",
    slug: "placeholder-one",
    account_type: "pro",
    whitelisted: true,
    member_count: 3,
    created_at: "2026-01-02T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
  },
];

function lastListParams(): ListOrganizationsParams {
  const call = mocks.listOrganizations.mock.calls.at(-1);
  return call?.[0] ?? {};
}

// Written out rather than imported from the toolbar. Reading the constant under
// test would let a shortened debounce move these assertions along with it.
const DEBOUNCE_MS = 300;

// `waitFor` polls on a timer, so it never settles while the clock is faked.
// Advance the clock instead, and let `act` flush the render and the navigation
// each tick releases.
async function withFakeTimers(
  body: (tick: (ms: number) => Promise<void>) => Promise<void>,
): Promise<void> {
  vi.useFakeTimers();
  try {
    await body(async (ms) => {
      await act(async () => {
        vi.advanceTimersByTime(ms);
      });
    });
  } finally {
    vi.useRealTimers();
  }
}

// The router JSON-encodes and then percent-encodes an array, so decoding once
// lets an assertion name the value the way the schema declares it.
function currentSearch(router: AnyRouter): string {
  return decodeURIComponent(router.state.location.searchStr);
}

// A Radix menu and a Radix select both open on pointerdown, not on click.
function openOn(trigger: HTMLElement): void {
  fireEvent.pointerDown(trigger, {
    button: 0,
    ctrlKey: false,
    pointerType: "mouse",
  });
}

function urlFor(search: Record<string, unknown>): string {
  // The router JSON-encodes a non-string value, so a bookmarked URL is built
  // the same way here rather than hand-written.
  const qs = new URLSearchParams();
  for (const [key, value] of Object.entries(search)) {
    qs.set(key, typeof value === "string" ? value : JSON.stringify(value));
  }
  return `/organizations?${qs.toString()}`;
}

beforeEach(() => {
  mocks.listOrganizations.mockReset();
  mocks.listOrganizations.mockResolvedValue({ organizations: ORGS });
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
});

afterEach(cleanup);

describe("organizations list", () => {
  it("takes the search term from the URL and sends it with the request", async () => {
    await renderRouteTree(routeTree, {
      initialPath: "/organizations?q=acme",
    });

    const input = screen.getByLabelText("Search organizations");
    expect((input as HTMLInputElement).value).toBe("acme");

    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });
    expect(lastListParams().q).toBe("acme");
  });

  it("holds a keystroke out of the URL until the debounce elapses", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });
    const input = screen.getByLabelText("Search organizations");

    let navigations = 0;
    router.subscribe("onResolved", () => {
      navigations += 1;
    });

    await withFakeTimers(async (tick) => {
      fireEvent.change(input, { target: { value: "acme" } });

      await tick(DEBOUNCE_MS - 1);
      expect(router.state.location.searchStr).toBe("");
      expect(navigations).toBe(0);

      await tick(1);
      expect(router.state.location.searchStr).toContain("q=acme");

      // A settled term must not re-arm the timer.
      await tick(DEBOUNCE_MS * 2);
    });

    expect(navigations).toBe(1);
  });

  it("commits a burst of typing as one history entry", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });
    const before = router.history.length;
    const input = screen.getByLabelText("Search organizations");

    await withFakeTimers(async (tick) => {
      for (const value of ["a", "ac", "acm", "acme"]) {
        fireEvent.change(input, { target: { value } });
        // Each keystroke has to restart the timer. Without a gap between them
        // the four changes coalesce through the effect cleanup alone, and the
        // debounce this test is named for is never exercised.
        await tick(DEBOUNCE_MS - 1);
        expect(router.state.location.searchStr).toBe("");
      }

      await tick(DEBOUNCE_MS);
      expect(router.state.location.searchStr).toContain("q=acme");
    });

    expect(router.history.length).toBe(before);
  });

  it("keeps a space the operator typed at the end of the term", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });
    const input = screen.getByLabelText("Search organizations");

    fireEvent.change(input, { target: { value: "acme " } });

    await waitFor(
      () => {
        expect(router.state.location.searchStr).toContain("q=acme");
      },
      { timeout: 2000 },
    );
    expect((input as HTMLInputElement).value).toBe("acme ");
  });

  it("follows the search term back when the operator goes back", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations?q=acme",
    });
    const input = screen.getByLabelText("Search organizations");

    await router.navigate({ to: "/organizations", search: { q: "widget" } });
    expect((input as HTMLInputElement).value).toBe("widget");

    router.history.back();

    await waitFor(() => {
      expect((input as HTMLInputElement).value).toBe("acme");
    });
  });

  it("hides a column the Columns control unchecks", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    expect(screen.getByRole("columnheader", { name: "Slug" })).toBeTruthy();

    openOn(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Slug" }));

    // The open menu hides the rest of the page from the accessibility tree, so
    // wait for a column that stays before reading the one that went.
    await waitFor(() => {
      expect(screen.getByRole("columnheader", { name: "Name" })).toBeTruthy();
    });
    expect(screen.queryByRole("columnheader", { name: "Slug" })).toBeNull();

    // The row has to drop the same cell the header dropped, or every cell
    // after it slides one column to the left.
    expect(screen.getAllByRole("cell").length).toBe(
      screen.getAllByRole("columnheader").length,
    );

    // Reopening reads the menu against the page's own state. Without this the
    // bar could take a constant and its guard would never fire.
    openOn(screen.getByRole("button", { name: "Columns" }));
    expect(
      screen
        .getByRole("menuitemcheckbox", { name: "Slug" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(
      screen
        .getByRole("menuitemcheckbox", { name: "Name" })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("shows the filters a reloaded URL carries", async () => {
    await renderRouteTree(routeTree, {
      initialPath: urlFor({ type: "pro", disabled: true }),
    });

    expect(screen.getByLabelText("Account type").textContent).toContain("pro");
    expect(screen.getByRole("button", { name: "Hide disabled" })).toBeTruthy();

    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });
    expect(lastListParams().account_type).toBe("pro");
    expect(lastListParams().include_disabled).toBe(true);
  });

  it("writes the filter controls back to the URL", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    openOn(screen.getByLabelText("Account type"));
    fireEvent.click(await screen.findByRole("option", { name: "enterprise" }));

    await waitFor(() => {
      expect(currentSearch(router)).toContain("type=enterprise");
    });

    fireEvent.click(screen.getByRole("button", { name: "Show disabled" }));

    await waitFor(() => {
      expect(currentSearch(router)).toContain("disabled=true");
    });
    expect(currentSearch(router)).toContain("type=enterprise");
  });

  it("clears the account type filter back to every type", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: urlFor({ type: "pro" }),
    });

    await waitFor(() => {
      expect(lastListParams().account_type).toBe("pro");
    });

    openOn(screen.getByLabelText("Account type"));
    fireEvent.click(await screen.findByRole("option", { name: "All types" }));

    // "All types" is the only route back to an unfiltered list.
    await waitFor(() => {
      expect(lastListParams().account_type).toBeUndefined();
    });
    expect(currentSearch(router)).not.toContain("type=");
  });

  it("sends a pasted term without the whitespace around it", async () => {
    await renderRouteTree(routeTree, {
      initialPath: urlFor({ q: "acme " }),
    });

    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });
    // Same request, and so the same cache entry, as `?q=acme`.
    expect(lastListParams().q).toBe("acme");
  });

  it("drops the cursor when a filter changes", async () => {
    mocks.listOrganizations.mockResolvedValue({
      organizations: ORGS,
      next_cursor: "cursor_page_two",
    });
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    const next = await screen.findByRole("button", { name: "Next" });
    await waitFor(() => {
      expect(next.hasAttribute("disabled")).toBe(false);
    });
    fireEvent.click(next);
    await waitFor(() => {
      expect(lastListParams().cursor).toBe("cursor_page_two");
    });

    fireEvent.click(screen.getByRole("button", { name: "Show disabled" }));

    await waitFor(() => {
      expect(lastListParams().include_disabled).toBe(true);
    });
    // The cursor was minted by the previous filter set and points into a
    // different result set.
    expect(lastListParams().cursor).toBeUndefined();
  });
});

// The bar takes a table, so the last-column case is reachable here by handing
// it a two column table rather than by clicking seven items shut through a
// menu that closes each time.
describe("TableActionBar", () => {
  const [FIRST, SECOND] = ORG_COLUMNS;
  if (!FIRST || !SECOND) throw new Error("ORG_COLUMNS needs two columns");

  // An accessor column takes its id from its key unless it names one, and that
  // id is what the visibility state is keyed by.
  const SECOND_ID = String(SECOND.id ?? SECOND.accessorKey);

  // Sliced, so the array carries the element type useTable asks for.
  const MENU_COLUMNS = ORG_COLUMNS.slice(0, 2);

  function ColumnsMenu({
    initialVisibility,
    onVisibilityChange,
    columns,
  }: {
    initialVisibility: ColumnVisibilityState;
    onVisibilityChange: (updater: Updater<ColumnVisibilityState>) => void;
    columns: typeof MENU_COLUMNS;
  }): JSX.Element {
    const [columnVisibility, setColumnVisibility] = useState(initialVisibility);
    const table = useTable({
      features: dataTableFeatures,
      columns,
      data: ORGS,
      getRowId: (org) => org.id,
      state: { columnVisibility },
      // The spy sits in front of the state, so a blocked toggle is a call that
      // never happened rather than a state that happened to settle back.
      onColumnVisibilityChange: (updater) => {
        onVisibilityChange(updater);
        setColumnVisibility(updater);
      },
    });
    return <TableActionBar table={table} />;
  }

  function openColumnsMenu(
    initialVisibility: ColumnVisibilityState,
    columns: typeof MENU_COLUMNS = MENU_COLUMNS,
  ): Mock<(updater: Updater<ColumnVisibilityState>) => void> {
    const onVisibilityChange =
      vi.fn<(updater: Updater<ColumnVisibilityState>) => void>();
    render(
      <ColumnsMenu
        initialVisibility={initialVisibility}
        onVisibilityChange={onVisibilityChange}
        columns={columns}
      />,
    );
    openOn(screen.getByRole("button", { name: "Columns" }));
    return onVisibilityChange;
  }

  // A toggle that lands closes the menu. Reopening also reads the item back
  // against the state that settled, not against the click that asked for it.
  function reopenColumnsMenu(): void {
    openOn(screen.getByRole("button", { name: "Columns" }));
  }

  // Found by the name an operator reads, so the query goes through the same
  // accessible name a screen reader would announce. A header is a node in
  // general, and only a string carries a name this query can match.
  function itemFor(header: unknown): HTMLElement {
    if (typeof header !== "string") {
      throw new Error("the column under test needs a text header");
    }
    return screen.getByRole("menuitemcheckbox", { name: header });
  }

  it("stops the operator hiding the last visible column", () => {
    const onVisibilityChange = openColumnsMenu({ [SECOND_ID]: false });

    const item = itemFor(FIRST.header);
    fireEvent.click(item);
    expect(onVisibilityChange).not.toHaveBeenCalled();

    // Marked rather than disabled. Radix drops a disabled item out of the
    // menu's roving focus, and a screen reader then never reaches it.
    expect(item.getAttribute("aria-disabled")).toBe("true");
    expect(item.hasAttribute("data-disabled")).toBe(false);

    // The other half of the guard. Radix dismisses the menu on select unless
    // the default is prevented, and a menu that closes on a locked item reads
    // as if the click had worked.
    expect(itemFor(FIRST.header)).toBe(item);
  });

  it("still unhides a column while only one is visible", () => {
    const onVisibilityChange = openColumnsMenu({ [SECOND_ID]: false });

    const item = itemFor(SECOND.header);
    expect(item.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(item);
    expect(onVisibilityChange).toHaveBeenCalled();

    // The lock holds the last visible column, not the whole menu. Locking the
    // menu would trap the operator in the state the lock exists to prevent.
    reopenColumnsMenu();
    expect(itemFor(SECOND.header).getAttribute("aria-checked")).toBe("true");
  });

  it("hides a column while a second one is still visible", () => {
    const onVisibilityChange = openColumnsMenu({});

    fireEvent.click(itemFor(FIRST.header));
    expect(onVisibilityChange).toHaveBeenCalled();

    reopenColumnsMenu();
    expect(itemFor(FIRST.header).getAttribute("aria-checked")).toBe("false");
  });

  it("locks a column that opts out of hiding", () => {
    // Both are visible, so the last-column rule is not what holds this one.
    const onVisibilityChange = openColumnsMenu({}, [
      { ...FIRST, enableHiding: false },
      SECOND,
    ]);

    const item = itemFor(FIRST.header);
    fireEvent.click(item);
    expect(onVisibilityChange).not.toHaveBeenCalled();
    expect(item.getAttribute("aria-disabled")).toBe("true");

    // The opt-out is per column, so the menu still works around it.
    fireEvent.click(itemFor(SECOND.header));
    expect(onVisibilityChange).toHaveBeenCalled();
  });
});

// A param the schema drops is absent from the parsed search, so every expected
// value below is the whole object the route sees.
describe("organizationsSearchSchema", () => {
  const cases: [string, Record<string, unknown>, OrganizationsSearch][] = [
    ["reads a hand-written type", { type: "free" }, { type: "free" }],
    ["drops a type the API does not accept", { type: "startup" }, {}],
    [
      "drops a list, which the request cannot honour",
      { type: ["pro", "enterprise"] },
      {},
    ],
    ["reads an all-digit term the router coerced", { q: 123 }, { q: "123" }],
    ["reads a boolean term the router coerced", { q: true }, { q: "true" }],
    ["reads a null term the router coerced", { q: null }, { q: "null" }],
    ["drops a term that is a list, not a word", { q: ["acme"] }, {}],
    ["drops a term that is only whitespace", { q: "   " }, {}],
    ["trims the term a pasted link carries", { q: "  acme  " }, { q: "acme" }],
    ["reads a direction in the union", { dir: "desc" }, { dir: "desc" }],
    ["drops a direction outside the union", { dir: "sideways" }, {}],
    ["keeps the disabled flag", { disabled: true }, { disabled: true }],
    ["drops the disabled flag when it is off", { disabled: false }, {}],
    ["drops page 1, which is the default", { page: 1 }, {}],
    ["drops a page below 1", { page: 0 }, {}],
    ["drops a page between two whole ones", { page: 2.5 }, {}],
    ["keeps a page past the first", { page: 2 }, { page: 2 }],
  ];

  it.each(cases)("%s", (_name, search, expected) => {
    expect(organizationsSearchSchema(search)).toEqual(expected);
  });
});
