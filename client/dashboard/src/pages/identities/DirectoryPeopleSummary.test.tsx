import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const { scopes, query } = vi.hoisted(() => ({
  scopes: new Set<string>(),
  query: { enabled: vi.fn(), isError: false },
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org-1",
    projects: [{ id: "project-1", slug: "default" }],
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "default",
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, resourceId?: string) =>
      scopes.has(`${scope}@${resourceId}`),
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    identities: { people: { href: () => "/org/default/identities/people" } },
  }),
}));
vi.mock("react-router", () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));
vi.mock("@gram/client/react-query/directoryPeople.js", () => ({
  useDirectoryPeople: (
    _request: unknown,
    _security: unknown,
    options: { enabled?: boolean },
  ) => {
    query.enabled(options.enabled);
    return {
      isError: query.isError,
      data: options.enabled
        ? {
            people: [],
            counts: {
              member: 4,
              accountNotMember: 1,
              notSignedIn: 2,
              ambiguousEmail: 0,
            },
          }
        : undefined,
    };
  },
}));

import { DirectoryPeopleSummary } from "./DirectoryPeopleSummary";

afterEach(cleanup);
beforeEach(() => {
  scopes.clear();
  query.isError = false;
  vi.clearAllMocks();
});

it("shows nothing and reads nothing without org:admin", () => {
  scopes.add("org:read@org-1");
  scopes.add("project:read@project-1");
  const { container } = render(<DirectoryPeopleSummary />);
  expect(query.enabled).toHaveBeenCalledWith(false);
  expect(container.textContent).toBe("");
});

it("links an admin who can open Identities to the filtered list", () => {
  scopes.add("org:admin@org-1");
  scopes.add("project:read@project-1");
  render(<DirectoryPeopleSummary />);
  expect(
    screen.getByText(/3 people in your directory aren't members yet/),
  ).toBeDefined();
  expect(
    screen
      .getByRole("link", { name: "View in Identities" })
      .getAttribute("href"),
  ).toContain("/org/default/identities/people?directory_state=");
});

it("keeps the count but drops the link without project:read", () => {
  scopes.add("org:admin@org-1");
  render(<DirectoryPeopleSummary />);
  expect(
    screen.getByText(/3 people in your directory aren't members yet/),
  ).toBeDefined();
  expect(screen.queryByRole("link")).toBeNull();
});

it("hides a stale count when the latest read failed", () => {
  scopes.add("org:admin@org-1");
  scopes.add("project:read@project-1");
  query.isError = true;
  const { container } = render(<DirectoryPeopleSummary />);
  expect(container.textContent).toBe("");
});
