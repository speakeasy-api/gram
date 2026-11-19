import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { ConnectionsTab } from "./ConnectionsTab";

const state = vi.hoisted(() => ({
  project: { id: "project_a", slug: "first" },
  listParams: [] as unknown[],
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => state.project }));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "first" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => false }),
}));
vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: () => ({ data: [], isError: false }),
}));
vi.mock("@/components/filters", () => ({
  defineFilters: (filters: unknown) => filters,
  useFilterState: () => ({ values: { subjectUrn: "user:u1" } }),
}));
vi.mock("@gram/client/react-query/userSessionFacets.js", () => ({
  useUserSessionFacets: () => ({ data: {} }),
}));
vi.mock("@gram/client/react-query/userSessions.js", () => ({
  useUserSessionsInfinite: (params: unknown) => {
    state.listParams.push(params);
    return { data: { pages: [] } };
  },
}));
vi.mock("@/components/connections/ConnectionsList", () => ({
  ConnectionsList: () => null,
}));
vi.mock("@/components/page-layout", () => ({
  Page: {
    Toolbar: Object.assign(
      ({ children }: { children: ReactNode }) => <div>{children}</div>,
      {
        Search: () => <input aria-label="Search connections" />,
        Filters: () => null,
        Refresh: () => null,
      },
    ),
  },
}));

afterEach(() => {
  cleanup();
  state.listParams = [];
});

it("lists only the chosen provider's connections, keeping the query's filters", () => {
  render(
    <MemoryRouter>
      <ConnectionsTab providerId="issuer_1" />
    </MemoryRouter>,
  );
  expect(state.listParams.at(-1)).toMatchObject({
    gramProject: "first",
    userSessionIssuerId: "issuer_1",
    subjectUrn: "user:u1",
  });
  expect(
    screen.getByRole("textbox", { name: "Search connections" }),
  ).toBeTruthy();
});
