import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation, useNavigate } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import UserSessions from "./UserSessions";

const state = vi.hoisted(() => ({
  project: { id: "project_a", slug: "first" },
  listCalls: [] as Array<Record<string, unknown>>,
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => state.project }));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ mcp: { add: { href: () => "/mcp/add" } } }),
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
  useUserSessionsInfinite: (request: Record<string, unknown>) => {
    state.listCalls.push(request);
    return { data: { pages: [] } };
  },
}));
vi.mock("./RemoteSessionRefreshPolicySetting", () => ({
  RemoteSessionRefreshPolicySetting: () => null,
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

afterEach(() => {
  cleanup();
  state.listCalls = [];
  state.project = { id: "project_a", slug: "first" };
});

function Location() {
  const location = useLocation();
  return <output data-testid="location">{location.search}</output>;
}

function Back() {
  const navigate = useNavigate();
  return (
    <>
      <button type="button" onClick={() => void navigate(-1)}>
        Back
      </button>
      <button
        type="button"
        onClick={() => {
          state.project = { id: "project_b", slug: "second" };
          void navigate("/second/sessions");
        }}
      >
        Switch project
      </button>
    </>
  );
}

function renderAt(...entries: string[]) {
  return render(
    <MemoryRouter initialEntries={entries} initialIndex={entries.length - 1}>
      <UserSessions />
      <Location />
      <Back />
    </MemoryRouter>,
  );
}

const lastSearch = () => state.listCalls.at(-1)?.search;

it("drops the search and scopes the page to the new project after a switch", () => {
  renderAt("/first/sessions?q=old%20project%20server");
  expect(screen.getByText("No connections match your search")).toBeTruthy();
  expect(state.listCalls.at(-1)).toMatchObject({
    gramProject: "first",
    search: "old project server",
  });
  expect(screen.getByTestId("page").getAttribute("data-scope")).toBe(
    "project:read",
  );

  fireEvent.click(screen.getByRole("button", { name: "Switch project" }));

  expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("");
  expect(screen.getByTestId("location").textContent).toBe("");
  expect(state.listCalls.at(-1)).toMatchObject({ gramProject: "second" });
  expect(lastSearch()).toBeUndefined();
  expect(screen.getByTestId("page").getAttribute("data-resource")).toBe(
    "project_b",
  );
  expect(
    screen.getByRole("link", { name: "Add MCP server" }).getAttribute("href"),
  ).toBe("/mcp/add");
});

it("searches on the server and keeps the search in the URL", () => {
  renderAt("/sessions?status=active");
  expect(lastSearch()).toBeUndefined();

  fireEvent.change(screen.getByRole("textbox"), {
    target: { value: " 50%_off " },
  });

  expect(screen.getByTestId("location").textContent).toBe(
    "?status=active&q=+50%25_off+",
  );
  expect(lastSearch()).toBe("50%_off");
  expect(screen.getByText("No connections match your search")).toBeTruthy();
});

it("restores the search from the URL and clears it", () => {
  renderAt("/sessions?q=dana");
  expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("dana");
  expect(lastSearch()).toBe("dana");

  fireEvent.change(screen.getByRole("textbox"), { target: { value: "" } });

  expect(screen.getByTestId("location").textContent).toBe("");
  expect(lastSearch()).toBeUndefined();
});

it("follows the search back through history", () => {
  renderAt("/sessions?q=first", "/sessions?q=second");
  expect(lastSearch()).toBe("second");

  fireEvent.click(screen.getByRole("button", { name: "Back" }));

  expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("first");
  expect(lastSearch()).toBe("first");
});
