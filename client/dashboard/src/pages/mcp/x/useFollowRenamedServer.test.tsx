import { renderHook } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useFollowRenamedServer } from "./useFollowRenamedServer";

const removeVisitsMatching = vi.hoisted(() =>
  vi.fn<
    (
      userId: string | undefined,
      orgSlug: string | undefined,
      projectSlug: string | undefined,
      predicate: (entry: { href: string }) => boolean,
    ) => void
  >(),
);
vi.mock("@/components/command-palette/recentlyVisited", () => ({
  removeVisitsMatching,
}));
vi.mock("@/contexts/Auth", () => ({ useUser: () => ({ id: "user-1" }) }));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "default" }),
}));
const base = (slug: string) => `/acme/projects/default/mcp/${slug}`;

afterEach(() => removeVisitsMatching.mockReset());

function follow(server: { id: string; slug?: string }) {
  const { result } = renderHook(() => useFollowRenamedServer(), {
    wrapper: ({ children }) => (
      <MemoryRouter initialEntries={[`${base("old-slug")}/settings`]}>
        <Routes>
          <Route
            path="/:orgSlug/projects/:projectSlug/mcp/:mcpServerSlug/*"
            element={children}
          />
        </Routes>
      </MemoryRouter>
    ),
  });
  result.current(server);
}

describe("useFollowRenamedServer", () => {
  it("drops recents under the retired slug", () => {
    follow({ id: "srv-1", slug: "new-slug" });

    expect(removeVisitsMatching).toHaveBeenCalledTimes(1);
    const [userId, orgSlug, projectSlug, matches] =
      removeVisitsMatching.mock.calls[0]!;
    expect([userId, orgSlug, projectSlug]).toEqual([
      "user-1",
      "acme",
      "default",
    ]);
    const visit = (href: string) => ({ href });
    expect(matches(visit(base("old-slug")))).toBe(true);
    expect(matches(visit(`${base("old-slug")}/settings`))).toBe(true);
    expect(matches(visit(base("old-slug-2")))).toBe(false);
    expect(matches(visit(base("new-slug")))).toBe(false);
  });

  it("keeps recents when the slug is unchanged", () => {
    follow({ id: "srv-1", slug: "old-slug" });

    expect(removeVisitsMatching).not.toHaveBeenCalled();
  });
});
