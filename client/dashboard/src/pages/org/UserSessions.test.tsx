import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import UserSessions from "./UserSessions";

const state = vi.hoisted(() => ({
  project: { id: "project_a", slug: "first" },
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => state.project }));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => true }),
}));
vi.mock("@/routes", () => ({ useRoutes: () => ({}) }));
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
  RemoteSessionRefreshPolicySetting: () => null,
}));
vi.mock("./ConsentToolFilteringSetting", () => ({
  ConsentToolFilteringSetting: () => null,
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

afterEach(cleanup);

it("clears the session search when switching projects and scopes the page to the new project", () => {
  state.project = { id: "project_a", slug: "first" };
  const { rerender } = render(<UserSessions />);
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
  rerender(<UserSessions />);
  expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("");
  expect(screen.getByTestId("page").getAttribute("data-resource")).toBe(
    "project_b",
  );
});
