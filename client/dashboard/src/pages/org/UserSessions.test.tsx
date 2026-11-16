import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import UserSessions from "./UserSessions";

const state = vi.hoisted(() => ({
  project: { id: "project_a", slug: "first" },
  tabbed: false,
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: state.tabbed ? "enabled" : "disabled" }),
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => state.project }));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "first" }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => false }),
}));
vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: () => ({ data: [], isError: false }),
}));
vi.mock("@/components/filters", () => ({
  defineFilters: (filters: unknown) => filters,
  useFilterState: () => ({ values: {} }),
}));
vi.mock("@gram/client/react-query/userSessionFacets.js", () => ({
  useUserSessionFacets: () => ({ data: {} }),
}));
vi.mock("@gram/client/react-query/userSessions.js", () => ({
  useUserSessionsInfinite: () => ({ data: { pages: [] } }),
}));
vi.mock("./RemoteSessionRefreshPolicySetting", () => ({
  RemoteSessionRefreshPolicySetting: () => <div>Refresh policy</div>,
}));
vi.mock("@/components/connections/ConnectionsList", () => ({
  ConnectionsList: () => null,
}));
vi.mock("@/components/page-templates", () => ({
  ResourceListPage: ({
    children,
    scope,
    resourceId,
  }: {
    children: ReactNode;
    scope: string;
    resourceId: string;
  }) => (
    <div data-testid="page" data-scope={scope} data-resource={resourceId}>
      {children}
    </div>
  ),
  TabbedPage: ({
    children,
    tabs,
    activeTab,
  }: {
    children: ReactNode;
    tabs: { value: string; label: string; href: string }[];
    activeTab: string;
  }) => (
    <div data-testid="tabbed-page" data-active={activeTab}>
      {tabs.map((tab) => (
        <a key={tab.value} href={tab.href}>
          {tab.label}
        </a>
      ))}
      {children}
    </div>
  ),
}));
vi.mock("@/components/page-layout", () => ({
  Page: {
    Toolbar: Object.assign(
      ({ children }: { children: ReactNode }) => <div>{children}</div>,
      {
        Search: ({
          value,
          onChange,
        }: {
          value: string;
          onChange: (value: string) => void;
        }) => (
          <input
            aria-label="Search connections"
            value={value}
            onChange={(event) => onChange(event.target.value)}
          />
        ),
        Filters: () => null,
        Refresh: () => null,
      },
    ),
  },
}));

afterEach(() => {
  cleanup();
  state.tabbed = false;
});

it("clears the session search when switching projects and scopes the page to the new project", () => {
  state.project = { id: "project_a", slug: "first" };
  const { rerender } = render(
    <MemoryRouter>
      <UserSessions />
    </MemoryRouter>,
  );
  expect(
    screen.getByRole("link", { name: "Add MCP server" }).getAttribute("href"),
  ).toBe("/org/projects/first/mcp/add");
  fireEvent.change(screen.getByRole("textbox"), {
    target: { value: "old project server" },
  });
  expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe(
    "old project server",
  );
  expect(screen.getByTestId("page").getAttribute("data-scope")).toBe(
    "project:read",
  );
  expect(screen.getByTestId("page").getAttribute("data-resource")).toBe(
    "project_a",
  );
  state.project = { id: "project_b", slug: "second" };
  rerender(
    <MemoryRouter>
      <UserSessions />
    </MemoryRouter>,
  );
  expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("");
  expect(screen.getByTestId("page").getAttribute("data-resource")).toBe(
    "project_b",
  );
});

it("keeps the refresh policy on the page without tabs while the flag is off", () => {
  render(
    <MemoryRouter>
      <UserSessions />
    </MemoryRouter>,
  );
  expect(screen.queryByTestId("tabbed-page")).toBeNull();
  expect(screen.getByText("Refresh policy")).toBeTruthy();
});

it("opens on the Overview tab without the refresh policy once the flag is on", () => {
  state.tabbed = true;
  state.project = { id: "project_a", slug: "first" };
  render(
    <MemoryRouter>
      <UserSessions />
    </MemoryRouter>,
  );
  expect(screen.getByTestId("tabbed-page").getAttribute("data-active")).toBe(
    "overview",
  );
  expect(
    screen.getByRole("link", { name: "Configuration" }).getAttribute("href"),
  ).toBe("/org/projects/first/mcp-sessions/configuration");
  expect(screen.queryByText("Refresh policy")).toBeNull();
  expect(screen.getByRole("textbox")).toBeTruthy();
});
