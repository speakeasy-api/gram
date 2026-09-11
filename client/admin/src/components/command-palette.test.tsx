import { detectPlatform } from "@tanstack/react-hotkeys";
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
  ListOrganizationsParams,
} from "@/lib/gramAdminApi";
import { organizationDashboardUrl } from "@/lib/gramAdminApi";
import { routeTree } from "@/routeTree.gen";
import { anOrganization } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

type Mounted = Awaited<ReturnType<typeof renderRouteTree>>;

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  listOrganizations: vi.fn(),
  getOrganization: vi.fn(),
  getOrganizationStats: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listOrganizationMembers: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getSession: mocks.getSession,
    listOrganizations: mocks.listOrganizations,
    getOrganization: mocks.getOrganization,
    getOrganizationStats: mocks.getOrganizationStats,
    listOrganizationProjects: mocks.listOrganizationProjects,
    listOrganizationMembers: mocks.listOrganizationMembers,
  };
});

const ORG = anOrganization({
  id: "org_01JQ",
  name: "Northwind Logistics",
  slug: "northwind",
});

// Named so no term that finds one finds the other: the search assertions below
// are about which record the server was asked for, and two records sharing a
// word cannot tell a passing filter from a passing request.
const OTHER = anOrganization({
  id: "org_01ZZ",
  name: "Umbrella Freight",
  slug: "umbrella",
  account_type: "enterprise",
  disabled_at: "2026-02-01T00:00:00Z",
});

// A second active record, because OTHER is disabled and a disabled record is
// deliberately offered no handoff. Sharing a letter with ORG and not with its
// name is what lets one term return two handoffs at once.
const THIRD = anOrganization({
  id: "org_01AA",
  name: "Redwood Analytics",
  slug: "redwood",
  account_type: "payg",
});

const RECORDS = [ORG, OTHER, THIRD];

// The one press that opens the palette, in the modifier this platform resolves
// `Mod` to. Read from the library rather than hardcoded, because the hotkey the
// component registers is resolved by the same function: a test naming Meta
// outright would pass on a developer's Mac and fail on CI.
const MOD = detectPlatform() === "mac" ? { metaKey: true } : { ctrlKey: true };

function pressTheShortcut(): void {
  fireEvent.keyDown(document, { key: "k", ...MOD });
}

function palette(): HTMLElement {
  return screen.getByRole("dialog");
}

// Record rows are matched anchored throughout. Each one is followed by its own
// "Open in Dashboard for <record>" row, so an unanchored name matches both and
// every query for a record becomes ambiguous.

function type(term: string): void {
  fireEvent.change(within(palette()).getByRole("combobox"), {
    target: { value: term },
  });
}

// Every term the palette asked the server about, in order. The table on the
// page behind it shares this mock and sends no term, so carrying one is what
// tells the palette's requests apart from its.
function searchedFor(): string[] {
  return mocks.listOrganizations.mock.calls
    .map((call) => call[0] as ListOrganizationsParams)
    .flatMap((params) => (params.q ? [params.q] : []));
}

// The forms the palette built, in the order it submitted them. A real submit
// would try to navigate the test document, and happy-dom has no tab to open, so
// the call is captured instead of performed and the element read afterwards.
let submitted: HTMLFormElement[] = [];
// Whether each form was still in the document at the moment it was submitted.
// A submit from a detached form does nothing at all, and Firefox abandons the
// navigation when the form leaves before the submission task runs.
let connectedAtSubmit: boolean[] = [];
let router: Mounted["router"] | undefined;

async function renderWithSubmitCaptured(): Promise<void> {
  ({ router } = await renderRouteTree(routeTree, {
    initialPath: "/organizations",
  }));
}

beforeEach(() => {
  submitted = [];
  connectedAtSubmit = [];
  router = undefined;
  vi.spyOn(HTMLFormElement.prototype, "submit").mockImplementation(
    function (this: HTMLFormElement) {
      // The helper detaches the form immediately after this returns, so the
      // element is held rather than its attributes read later off the document.
      submitted.push(this);
      connectedAtSubmit.push(this.isConnected);
    },
  );
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.listOrganizations.mockReset();
  // Answers the palette the way the server does, so a test can tell a record
  // the request found from one a client-side filter let through.
  mocks.listOrganizations.mockImplementation(
    (params: ListOrganizationsParams) => {
      const term = params.q?.toLowerCase() ?? "";
      if (!term) return Promise.resolve({ organizations: RECORDS });
      return Promise.resolve({
        organizations: RECORDS.filter(
          (org) =>
            org.name.toLowerCase().includes(term) ||
            org.slug.toLowerCase().includes(term) ||
            org.id.toLowerCase() === term,
        ),
      });
    },
  );
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockImplementation((idOrSlug: string) => {
    const found = RECORDS.find(
      (org) => org.id === idOrSlug || org.slug === idOrSlug,
    );
    return found
      ? Promise.resolve(found)
      : Promise.reject(new Error(`no organization ${idOrSlug}`));
  });
  mocks.getOrganizationStats.mockReset();
  mocks.getOrganizationStats.mockResolvedValue({
    total: 2,
    created_last_7_days: 0,
    customers: 1,
    customers_created_last_7_days: 0,
    trials_ending_soon: 0,
    disabled: 1,
    disabled_last_7_days: 0,
  });
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
  mocks.listOrganizationMembers.mockReset();
  mocks.listOrganizationMembers.mockResolvedValue({ members: [] });
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ logs: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("CommandPalette", () => {
  it("opens on the shortcut and closes on the same press", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    expect(screen.queryByRole("dialog")).toBeNull();

    pressTheShortcut();
    expect(await screen.findByRole("dialog")).toBeTruthy();

    // The press an operator reaches for to dismiss it is the one that opened
    // it, so the shortcut has to answer both.
    pressTheShortcut();
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("opens from the button in the header", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    fireEvent.click(
      screen.getByRole("button", { name: "Search organizations and pages" }),
    );

    expect(await screen.findByRole("dialog")).toBeTruthy();
  });

  it("opens the organization that was chosen", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    pressTheShortcut();
    await screen.findByRole("dialog");
    type("northwind");

    const result = await within(palette()).findByRole("option", {
      name: /^Northwind Logistics/,
    });
    fireEvent.click(result);

    await waitFor(() => {
      expect(router.state.location.pathname).toBe(`/organizations/${ORG.slug}`);
    });
  });

  it("keeps a record the server matched on something other than its name", async () => {
    // The whole reason cmdk's own filter is switched off. An id matches the
    // record exactly on the server and appears nowhere in its name, so a
    // client-side filter would hide the one row a pasted id asked for.
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    pressTheShortcut();
    await screen.findByRole("dialog");
    type(ORG.id);

    expect(
      await within(palette()).findByRole("option", {
        name: /^Northwind Logistics/,
      }),
    ).toBeTruthy();
  });

  it("searches disabled organizations as well as active ones", async () => {
    // The table defaults to active only. The palette is how an operator reaches
    // a record they already have in mind, and a disabled one is a leading
    // reason to go looking.
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    pressTheShortcut();
    await screen.findByRole("dialog");
    type("umbrella");

    expect(
      await within(palette()).findByRole("option", {
        name: /^Umbrella Freight/,
      }),
    ).toBeTruthy();

    expect(mocks.listOrganizations).toHaveBeenCalledWith(
      expect.objectContaining({
        q: "umbrella",
        disabled_states: ["active", "disabled"],
      }),
    );
  });

  it("says a disabled record is disabled", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    pressTheShortcut();
    await screen.findByRole("dialog");
    type("umbrella");

    const result = await within(palette()).findByRole("option", {
      name: /^Umbrella Freight/,
    });
    expect(result.textContent).toContain("Disabled");
  });

  it("asks for one term per burst of typing", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    pressTheShortcut();
    await screen.findByRole("dialog");

    type("n");
    type("no");
    type("nor");
    type("northwind");

    await within(palette()).findByRole("option", {
      name: /^Northwind Logistics/,
    });

    // The keystrokes in between reached no request: each one supersedes the
    // debounce the one before it set.
    expect(searchedFor()).toEqual(["northwind"]);
  });

  it("offers each matched organization a direct dashboard handoff", async () => {
    await renderWithSubmitCaptured();

    pressTheShortcut();
    await screen.findByRole("dialog");
    type("northwind");
    await within(palette()).findByRole("option", {
      name: /^Northwind Logistics/,
    });

    const handoff = within(palette()).getByRole("option", {
      name: /Open in Dashboard for Northwind Logistics/,
    });
    fireEvent.click(handoff);

    // The same request RecordHeader's button makes: a POST, so the admin origin
    // check protects handoff issuance, opened in a tab of its own.
    expect(submitted).toHaveLength(1);
    expect(submitted[0]?.method.toLowerCase()).toBe("post");
    expect(submitted[0]?.getAttribute("action")).toBe(
      organizationDashboardUrl(ORG.id),
    );
    expect(submitted[0]?.target).toBe("_blank");
    // noreferrer would make Chromium send `Origin: null` for this POST, which
    // the admin CSRF middleware correctly rejects.
    expect(submitted[0]?.getAttribute("rel")).toBe("noopener");
  });

  it("submits the handoff from a form that is still in the document", async () => {
    await renderWithSubmitCaptured();

    pressTheShortcut();
    await screen.findByRole("dialog");
    type("northwind");
    const handoff = await within(palette()).findByRole("option", {
      name: /Open in Dashboard for Northwind Logistics/,
    });
    fireEvent.click(handoff);

    // Connected when it ran, and still connected afterwards. The second half is
    // the regression guard: detaching the form synchronously after `submit()`
    // leaves this passing in Chromium and silently opens nothing in Firefox,
    // because the submission is processed in a later task.
    expect(connectedAtSubmit).toEqual([true]);
    expect(submitted[0]?.isConnected).toBe(true);
  });

  it("offers no dashboard handoff for a disabled organization", async () => {
    // The endpoint refuses a disabled record outright, so the row would open a
    // new tab onto a 404 — somewhere nothing on this page could explain it.
    await renderWithSubmitCaptured();

    pressTheShortcut();
    await screen.findByRole("dialog");
    type("umbrella");

    await within(palette()).findByRole("option", { name: /^Umbrella Freight/ });
    expect(
      within(palette()).queryByRole("option", {
        name: /Open in Dashboard for Umbrella Freight/,
      }),
    ).toBeNull();
  });

  it("names the record each handoff belongs to", async () => {
    // Every one of these rows reads "Open in Dashboard", so without the record
    // in the accessible name a screen reader hears a run of identical options.
    await renderWithSubmitCaptured();

    pressTheShortcut();
    await screen.findByRole("dialog");
    // A letter both records carry, so the list holds two handoffs at once.
    type("r");

    await within(palette()).findByRole("option", {
      name: /Open in Dashboard for Northwind Logistics/,
    });
    expect(
      within(palette()).getByRole("option", {
        name: /Open in Dashboard for Redwood Analytics/,
      }),
    ).toBeTruthy();
  });

  it("leaves the admin record open behind the handoff", async () => {
    await renderWithSubmitCaptured();

    pressTheShortcut();
    await screen.findByRole("dialog");
    type("northwind");
    const handoff = await within(palette()).findByRole("option", {
      name: /Open in Dashboard for Northwind Logistics/,
    });
    fireEvent.click(handoff);

    // The palette closes, but nothing navigated: the dashboard opens in a tab
    // of its own and the operator keeps the admin page they were on.
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(router?.state.location.pathname).toBe("/organizations");
  });

  it("says so when a term matches no organization", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    pressTheShortcut();
    await screen.findByRole("dialog");
    type("nothing-by-this-name");

    // Its own words, not the one the group shows while a request is open: an
    // operator told "Searching..." forever reads a finished search as a hung
    // one, and one told "No organizations match" mid-flight reads the opposite.
    expect(
      await within(palette()).findByText("No organizations match."),
    ).toBeTruthy();
  });

  it("does not call a term empty while its first request is still open", async () => {
    // The first search of a session has no previous term for keepPreviousData
    // to hold, so `data` is undefined mid-flight with nothing marking it as
    // placeholder. Reading that as "no rows" reports a record that does exist
    // as missing, in the window before its own request answers.
    let release:
      | ((value: { organizations: AdminOrganization[] }) => void)
      | undefined;
    mocks.listOrganizations.mockImplementation(
      (params: ListOrganizationsParams) => {
        if (!params.q) return Promise.resolve({ organizations: RECORDS });
        return new Promise((resolve) => {
          release = resolve;
        });
      },
    );

    await renderRouteTree(routeTree, { initialPath: "/organizations" });
    pressTheShortcut();
    await screen.findByRole("dialog");
    type("northwind");

    // Wait for the request to actually be in flight rather than for the
    // debounce: before it is issued the group is legitimately "Searching...",
    // so asserting earlier would pass with or without the fix.
    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalledWith(
        expect.objectContaining({ q: "northwind" }),
      );
    });

    expect(within(palette()).queryByText("No organizations match.")).toBeNull();
    expect(within(palette()).getByText("Searching...")).toBeTruthy();

    release?.({ organizations: [ORG] });
    expect(
      await within(palette()).findByRole("option", {
        name: /^Northwind Logistics/,
      }),
    ).toBeTruthy();
  });

  it("does not show one term's records under another term's box", async () => {
    // `keepPreviousData` holds the previous term's rows while the next request
    // is open, and Enter lands on whatever is highlighted.
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    pressTheShortcut();
    await screen.findByRole("dialog");
    type("northwind");
    await within(palette()).findByRole("option", {
      name: /^Northwind Logistics/,
    });

    type("umbrella");

    // Gone in the same commit the term changed in, rather than when the request
    // for the new one lands.
    expect(
      within(palette()).queryByRole("option", { name: /^Northwind Logistics/ }),
    ).toBeNull();

    expect(
      await within(palette()).findByRole("option", {
        name: /^Umbrella Freight/,
      }),
    ).toBeTruthy();
  });

  it("goes to a top-level page", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });

    pressTheShortcut();
    await screen.findByRole("dialog");

    fireEvent.click(
      within(palette()).getByRole("option", { name: "Projects" }),
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/projects");
    });
  });

  it("finds a page by a word that is not in its name", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    pressTheShortcut();
    await screen.findByRole("dialog");
    type("tenants");

    expect(
      within(palette()).getByRole("option", { name: "Organizations" }),
    ).toBeTruthy();
  });

  it("offers the record's own views while the operator is inside one", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });
    // The group is headed by the record, so it waits on the same query the
    // sidebar reads.
    await screen.findByText(ORG.name);

    pressTheShortcut();
    await screen.findByRole("dialog");

    fireEvent.click(
      await within(palette()).findByRole("option", { name: "Billing" }),
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe(
        `/organizations/${ORG.slug}/billing`,
      );
    });
  });

  it("offers no record views off a record", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    pressTheShortcut();
    await screen.findByRole("dialog");

    expect(
      within(palette()).queryByRole("option", { name: "Billing" }),
    ).toBeNull();
  });

  it("forgets the term when it closes", async () => {
    await renderRouteTree(routeTree, { initialPath: "/organizations" });

    pressTheShortcut();
    await screen.findByRole("dialog");
    type("northwind");
    await within(palette()).findByRole("option", {
      name: /^Northwind Logistics/,
    });

    pressTheShortcut();
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    pressTheShortcut();
    const reopened = await screen.findByRole("dialog");
    // A term left behind would be sitting above records fetched for it before
    // the palette was last dismissed.
    expect(within(reopened).getByRole("combobox")).toHaveProperty("value", "");
    expect(
      within(reopened).queryByRole("option", { name: /^Northwind Logistics/ }),
    ).toBeNull();
  });
});
