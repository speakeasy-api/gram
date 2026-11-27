import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { expect, it, vi } from "vitest";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

import {
  childRoutes,
  type PagedRoute,
  withOrgPages,
  withProjectPages,
} from "@/route-pages";
import { useOrgRoutes, useRoutes } from "@/routes";

// Route elements are keyed by url; titles repeat among siblings.
it("gives sibling routes distinct urls", () => {
  const { result } = renderHook(
    () => ({
      project: withProjectPages(useRoutes()),
      org: withOrgPages(useOrgRoutes()),
    }),
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <MemoryRouter>{children}</MemoryRouter>
      ),
    },
  );
  const duplicates: string[] = [];
  const walk = (path: string, siblings: PagedRoute[]) => {
    const seen = new Set<string>();
    for (const route of siblings.filter((r) => !r.external)) {
      if (seen.has(route.url)) duplicates.push(`${path}/${route.url}`);
      seen.add(route.url);
      walk(`${path}/${route.url}`, childRoutes(route));
    }
  };
  walk("project", Object.values(result.current.project));
  walk("org", Object.values(result.current.org));
  expect(duplicates).toEqual([]);
});
