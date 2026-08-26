import { onlineManager, QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationQuery } from "@/lib/adminQueries";
import { GramAdminError } from "@/lib/gramAdminApi";
import { routeTree } from "@/routeTree.gen";
import { anOrganization } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listOrganizationMembers: vi.fn(),
  listOrganizationActivity: vi.fn(),
  enableOrganization: vi.fn(),
}));

// Only the endpoints this route reaches are replaced. The rest of the module
// stays real, so toSearchParams still decides what a request carries.
vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getSession: mocks.getSession,
    getOrganization: mocks.getOrganization,
    listOrganizationProjects: mocks.listOrganizationProjects,
    listOrganizationMembers: mocks.listOrganizationMembers,
    listOrganizationActivity: mocks.listOrganizationActivity,
    enableOrganization: mocks.enableOrganization,
  };
});

const ORG = anOrganization();

// Live and trialling, so the callout has a reason to draw.
const TRIALLING = anOrganization({
  trial_state: "running",
  trial_ends_at: "2026-05-06T00:00:00Z",
});

beforeEach(() => {
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(ORG);
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
  mocks.listOrganizationMembers.mockReset();
  mocks.listOrganizationMembers.mockResolvedValue({ members: [] });
  mocks.listOrganizationActivity.mockReset();
  mocks.listOrganizationActivity.mockResolvedValue({ logs: [] });
  mocks.enableOrganization.mockReset();
});

afterEach(cleanup);

// The one region a screen reader hears a write through. Read as a node rather
// than by its text, because what this is about is the text changing.
function liveRegion(): HTMLElement {
  const found = document.querySelector("[aria-live='polite']");
  if (!(found instanceof HTMLElement)) {
    throw new Error("no live region on the record");
  }
  return found;
}

// What an operator reads, with the zero-width padding taken back out.
function spoken(region: HTMLElement): string {
  return (region.textContent ?? "").replaceAll("\u200b", "");
}

describe("RecordLayout", () => {
  it("renders the record name once the query resolves", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    expect(await screen.findByRole("heading", { name: ORG.name })).toBeTruthy();
  });

  it("keeps record context around the activity view and requests by explicit ID", async () => {
    mocks.listOrganizationActivity.mockImplementation(
      (organizationID: string) =>
        organizationID === ORG.id
          ? Promise.resolve({ logs: [] })
          : Promise.reject(
              new Error(`unexpected organization ${organizationID}`),
            ),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/activity`,
    });

    expect(await screen.findByRole("heading", { name: ORG.name })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Activity" })).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /Open in Dashboard/ }),
    ).toBeTruthy();
    expect(mocks.listOrganizationActivity).toHaveBeenCalledTimes(1);
    expect(mocks.listOrganizationActivity).toHaveBeenCalledWith(
      ORG.id,
      undefined,
    );
  });

  it("renders the record name on every view, not only the index", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/members`,
    });

    expect(await screen.findByRole("heading", { name: ORG.name })).toBeTruthy();
  });

  // The claim that the header and the callout are record chrome rather than
  // part of Overview. Without it, putting both inside `Overview.tsx` passes
  // every other test in this file.
  it.each(["", "/activity", "/projects", "/members"])(
    "renders the header and the callout above the view at %s",
    async (view) => {
      mocks.getOrganization.mockResolvedValue(TRIALLING);

      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${TRIALLING.slug}${view}`,
      });

      expect(
        await screen.findByRole("button", { name: /Open in Dashboard/ }),
      ).toBeTruthy();
      const callout = await screen.findByRole("status");
      expect(callout.textContent).toContain("Trial ends");
    },
  );

  it("draws the callout under the record's title, not above it", async () => {
    mocks.getOrganization.mockResolvedValue(TRIALLING);

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${TRIALLING.slug}`,
    });

    const heading = await screen.findByRole("heading", {
      name: TRIALLING.name,
    });
    const callout = await screen.findByRole("status");
    // The drawing puts the trial badge in the title and the warning beneath
    // it. Both present in either order passes every other test here.
    expect(
      heading.compareDocumentPosition(callout) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("speaks the result of a write started from the record chrome", async () => {
    // The actions report through context whose default is silent, so a record
    // page with no reporter of its own announces nothing at all.
    const disabled = anOrganization({ disabled_at: "2026-02-01T00:00:00Z" });
    mocks.getOrganization.mockResolvedValue(disabled);
    mocks.enableOrganization.mockResolvedValue({
      ...disabled,
      disabled_at: undefined,
    });

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${disabled.slug}`,
    });

    fireEvent.click(
      await screen.findByRole("button", { name: `Re-enable ${disabled.name}` }),
    );

    await waitFor(() => {
      expect(spoken(liveRegion())).toBe(`${disabled.name} is enabled.`);
    });
    expect(liveRegion().getAttribute("aria-live")).toBe("polite");
    // Heard and not read. Without the class every write result is printed into
    // the record as body text, between the callout and the view.
    expect(liveRegion().className.split(" ")).toContain("sr-only");
  });

  it("shows a failed re-enable, and does not only speak it", async () => {
    const disabled = anOrganization({ disabled_at: "2026-02-01T00:00:00Z" });
    mocks.getOrganization.mockResolvedValue(disabled);
    mocks.enableOrganization.mockRejectedValue(
      new GramAdminError(500, { message: "enable failed" }, "500"),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${disabled.slug}`,
    });

    fireEvent.click(
      await screen.findByRole("button", { name: `Re-enable ${disabled.name}` }),
    );

    // Re-enable is the one write with no dialog of its own to report in, so
    // without this banner a sighted operator presses the button, watches the
    // record stay disabled and is told nothing about why. The live region
    // carries the same sentence and is skipped: it is heard, not read.
    const banner = await screen.findByText(/Could not re-enable/, {
      ignore: "[aria-live]",
    });
    expect(banner.className.split(" ")).toContain("text-destructive");
  });

  it("does not draw one record's failure over the next one", async () => {
    const disabled = anOrganization({ disabled_at: "2026-02-01T00:00:00Z" });
    const other = anOrganization({
      id: "org_2",
      name: "Second Org",
      slug: "second-org",
    });
    mocks.getOrganization.mockImplementation((idOrSlug: string) =>
      Promise.resolve(idOrSlug === other.slug ? other : disabled),
    );
    mocks.enableOrganization.mockRejectedValue(
      new GramAdminError(500, { message: "enable failed" }, "500"),
    );

    // Both records already in the cache, which is the state the list navigates
    // from. Without the seed the second arrives pending, the layout paints its
    // loading state, and the unmount that follows clears the banner for a
    // reason that has nothing to do with the record it belonged to.
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    qc.setQueryData(organizationQuery(disabled.slug).queryKey, disabled);
    qc.setQueryData(organizationQuery(other.slug).queryKey, other);

    const { router } = await renderRouteTree(routeTree, {
      initialPath: `/organizations/${disabled.slug}`,
      queryClient: qc,
    });

    fireEvent.click(
      await screen.findByRole("button", { name: `Re-enable ${disabled.name}` }),
    );
    await screen.findByText(/Could not re-enable/, { ignore: "[aria-live]" });

    await router.navigate({
      to: "/organizations/$idOrSlug",
      params: { idOrSlug: other.slug },
    });

    expect(
      await screen.findByRole("heading", { name: other.name }),
    ).toBeTruthy();
    // The route does not remount when its parameter changes. A banner that
    // survives it tells the operator an organization they have only just opened
    // failed something nobody did to it.
    expect(
      screen.queryByText(/Could not re-enable/, { ignore: "[aria-live]" }),
    ).toBeNull();
  });

  it("speaks a failure again when the same one comes back", async () => {
    const disabled = anOrganization({ disabled_at: "2026-02-01T00:00:00Z" });
    mocks.getOrganization.mockResolvedValue(disabled);
    mocks.enableOrganization.mockRejectedValue(
      new GramAdminError(500, { message: "enable failed" }, "500"),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${disabled.slug}`,
    });

    const button = await screen.findByRole("button", {
      name: `Re-enable ${disabled.name}`,
    });

    // Three presses, not two. A region is announced when its text changes, and
    // nothing else on the page reports this failure: the banner's words are
    // unchanged too. Two presses pass under any padding that changes once, so
    // they cannot tell alternating from a longer cycle that goes quiet on the
    // third.
    let previous = "";
    let sentence: string | undefined;
    for (let press = 1; press <= 3; press++) {
      fireEvent.click(button);
      await waitFor(() => {
        expect(liveRegion().textContent).not.toBe(previous);
      });
      previous = liveRegion().textContent ?? "";
      // The operator is told the same thing every time, not a different thing.
      sentence ??= spoken(liveRegion());
      expect(spoken(liveRegion())).toBe(sentence);
    }
    expect(sentence).toContain(`Could not re-enable ${disabled.name}`);
  });

  it("says it is loading rather than reporting an error it has not had", async () => {
    // Pending forever. A record that has not arrived is not a record that
    // failed: without the loading branch this paints "Error:" with nothing
    // after it, for as long as the request takes.
    mocks.getOrganization.mockImplementation(() => new Promise(() => {}));

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    expect(await screen.findByText("Loading...")).toBeTruthy();
    expect(screen.queryByText(/Error/)).toBeNull();
  });

  it("says it is loading rather than reporting an error while the read is paused", async () => {
    // Offline, so the query is pending and not fetching. React Query calls that
    // neither loading nor errored, and there is no error to name: a branch that
    // asks about the failure first prints the words "Error: null".
    onlineManager.setOnline(false);
    try {
      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${ORG.slug}`,
      });

      expect(await screen.findByText("Loading...")).toBeTruthy();
      expect(screen.queryByText(/Error/)).toBeNull();
    } finally {
      onlineManager.setOnline(true);
    }
  });

  it("keeps a record it is holding when a refetch over it fails", async () => {
    // The cache seeded before the mount is what `useOpenOrganization` leaves
    // behind, so the layout's own read is a refetch over a record the operator
    // is already reading.
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    qc.setQueryData(organizationQuery(ORG.slug).queryKey, ORG);
    mocks.getOrganization.mockRejectedValue(
      new GramAdminError(500, { message: "organization read failed" }, "500"),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
      queryClient: qc,
    });

    await waitFor(() => {
      expect(
        qc.getQueryState(organizationQuery(ORG.slug).queryKey)?.status,
      ).toBe("error");
    });
    // A record still in hand is a record the operator keeps reading. Asking
    // about the failure first takes the page out from under them.
    expect(screen.getByRole("heading", { name: ORG.name })).toBeTruthy();
    expect(screen.queryByText(/organization read failed/)).toBeNull();
  });

  it("renders no view at all when the record fails to load", async () => {
    mocks.getOrganization.mockRejectedValue(
      new GramAdminError(404, { message: "organization not found" }, "404"),
    );

    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    expect(await screen.findByText(/organization not found/)).toBeTruthy();
    // A record that failed to load has no views. Rendering the outlet anyway
    // leaves the operator reading a card of dashes for an organization that
    // may not exist.
    expect(screen.queryByText("Account type")).toBeNull();
  });
});
