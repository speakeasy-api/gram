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
  mocks.enableOrganization.mockReset();
});

afterEach(cleanup);

describe("RecordLayout", () => {
  it("renders the record name once the query resolves", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}`,
    });

    expect(await screen.findByRole("heading", { name: ORG.name })).toBeTruthy();
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
  it.each(["", "/projects", "/members"])(
    "renders the header and the callout above the view at %s",
    async (view) => {
      mocks.getOrganization.mockResolvedValue(TRIALLING);

      await renderRouteTree(routeTree, {
        initialPath: `/organizations/${TRIALLING.slug}${view}`,
      });

      expect(
        await screen.findByRole("link", { name: /Open in Gram/ }),
      ).toBeTruthy();
      const callout = await screen.findByRole("status");
      expect(callout.textContent).toContain("Trial ends");
    },
  );

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

    const live = await screen.findByText(`${disabled.name} is enabled.`);
    expect(live.getAttribute("aria-live")).toBe("polite");
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
