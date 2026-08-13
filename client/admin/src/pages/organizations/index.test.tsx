import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AdminOrganization,
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

  it("commits a burst of typing as one history entry", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/organizations",
    });
    const before = router.history.length;
    const input = screen.getByLabelText("Search organizations");

    for (const value of ["a", "ac", "acm", "acme"]) {
      fireEvent.change(input, { target: { value } });
    }

    await waitFor(() => {
      expect(router.state.location.searchStr).toContain("q=acme");
    });
    expect(router.history.length).toBe(before);
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

    // The trigger opens on pointerdown, not on click.
    fireEvent.pointerDown(screen.getByRole("button", { name: "Columns" }), {
      button: 0,
      ctrlKey: false,
      pointerType: "mouse",
    });
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Slug" }));

    // The open menu hides the rest of the page from the accessibility tree, so
    // wait for a column that stays before reading the one that went.
    await waitFor(() => {
      expect(screen.getByRole("columnheader", { name: "Name" })).toBeTruthy();
    });
    expect(screen.queryByRole("columnheader", { name: "Slug" })).toBeNull();
  });

  it("shows the filters a reloaded URL carries", async () => {
    await renderRouteTree(routeTree, {
      initialPath: urlFor({ type: ["pro"], disabled: true }),
    });

    expect(screen.getByLabelText("Account type").textContent).toContain("pro");
    expect(screen.getByRole("button", { name: "Hide disabled" })).toBeTruthy();

    await waitFor(() => {
      expect(mocks.listOrganizations).toHaveBeenCalled();
    });
    expect(lastListParams().account_type).toBe("pro");
    expect(lastListParams().include_disabled).toBe(true);
  });
});
