import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { GatewayDebugTab } from "./GatewayDebugTab";

const state = vi.hoisted(() => ({
  calls: [] as Array<Record<string, unknown>>,
  options: [] as Array<Record<string, unknown>>,
  result: {} as Record<string, unknown>,
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_a", slug: "first" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => false }),
}));
vi.mock("@/hooks/useSessionAgents", () => ({
  useSessionAgents: (sessions: unknown[]) => sessions,
}));
vi.mock("@gram/client/react-query/userSessions.js", () => ({
  useUserSessionsInfinite: (
    request: Record<string, unknown>,
    _security: unknown,
    options: Record<string, unknown>,
  ) => {
    state.calls.push(request);
    state.options.push(options);
    return {
      data: { pages: [{ result: { items: [] } }] },
      refetch: vi.fn(),
      fetchNextPage: vi.fn(),
      ...state.result,
    };
  },
}));
vi.mock("@/components/connections/ConnectionsList", () => ({
  ConnectionsList: ({ sessions }: { sessions: { id: string }[] }) => (
    <ul>
      {sessions.map((s) => (
        <li key={s.id}>{s.id}</li>
      ))}
    </ul>
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
        Refresh: ({ onRefresh }: { onRefresh: () => void }) => (
          <button type="button" onClick={onRefresh}>
            Refresh
          </button>
        ),
      },
    ),
  },
}));

afterEach(() => {
  cleanup();
  state.calls = [];
  state.options = [];
  state.result = {};
});

function Location() {
  return <output data-testid="location">{useLocation().search}</output>;
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <GatewayDebugTab gatewayId="gateway-1" />
      <Location />
    </MemoryRouter>,
  );
}

const withRows = { data: { pages: [{ result: { items: [{ id: "s1" }] } }] } };

it("lists only the gateway's sessions and keeps the search in the URL", () => {
  renderAt("/debug?q=dana");
  expect(state.calls.at(-1)).toEqual({
    gramProject: "first",
    metaMcpServerId: "gateway-1",
    search: "dana",
  });
  expect(
    screen.getByText(
      "Sessions issued for this gateway, identified by their recorded origin or consent selection. Unattributed sessions are not shown.",
    ),
  ).toBeTruthy();

  fireEvent.change(screen.getByRole("textbox"), { target: { value: "" } });
  expect(screen.getByTestId("location").textContent).toBe("");
  expect(state.calls.at(-1)).toMatchObject({
    metaMcpServerId: "gateway-1",
    search: undefined,
  });
});

it("says when nothing has been recorded for the gateway", () => {
  renderAt("/debug");
  expect(
    screen.getByText("No connections have been recorded for this gateway yet"),
  ).toBeTruthy();
});

it("keeps the rows and retries a failed refresh", () => {
  const refetch = vi.fn();
  state.result = { ...withRows, isError: true, isRefetchError: true, refetch };
  renderAt("/debug");
  expect(screen.getByText("s1")).toBeTruthy();
  expect(screen.getByText("Couldn’t refresh connections.")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(refetch).toHaveBeenCalledOnce();
});

it("keeps the rows and retries a failed next page", () => {
  const fetchNextPage = vi.fn();
  const refetch = vi.fn();
  state.result = {
    ...withRows,
    isError: true,
    isFetchNextPageError: true,
    hasNextPage: true,
    fetchNextPage,
    refetch,
  };
  renderAt("/debug");
  expect(screen.getByText("s1")).toBeTruthy();
  expect(screen.getByText("Couldn’t load more connections.")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(fetchNextPage).toHaveBeenCalledOnce();
  expect(refetch).not.toHaveBeenCalled();
});

it("does not request an overlong search and leaves nothing to act on", () => {
  const refetch = vi.fn();
  const fetchNextPage = vi.fn();
  state.result = {
    ...withRows,
    hasNextPage: true,
    isError: true,
    isRefetchError: true,
    refetch,
    fetchNextPage,
  };
  const overlong = "a".repeat(201);
  renderAt(`/debug?q=${overlong}`);

  expect(state.options.at(-1)).toMatchObject({ enabled: false });
  expect(state.calls.at(-1)?.search).toBeUndefined();
  expect(
    screen.getByText("Search must be 200 characters or fewer."),
  ).toBeTruthy();
  expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe(
    overlong,
  );
  expect(screen.getByTestId("location").textContent).toBe(`?q=${overlong}`);
  expect(screen.queryByText("s1")).toBeNull();
  expect(screen.queryByRole("button", { name: "Refresh" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  expect(refetch).not.toHaveBeenCalled();
  expect(fetchNextPage).not.toHaveBeenCalled();

  fireEvent.change(screen.getByRole("textbox"), { target: { value: "dana" } });
  expect(state.options.at(-1)).toMatchObject({ enabled: true });
  expect(state.calls.at(-1)?.search).toBe("dana");
});

it("counts the limit in code points, not UTF-16 units", () => {
  const astral = "\u{1D49C}";
  renderAt(`/debug?q=${encodeURIComponent(astral.repeat(200))}`);
  expect(state.options.at(-1)).toMatchObject({ enabled: true });
  expect(state.calls.at(-1)?.search).toBe(astral.repeat(200));
  cleanup();

  renderAt(`/debug?q=${encodeURIComponent(astral.repeat(201))}`);
  expect(state.options.at(-1)).toMatchObject({ enabled: false });
  expect(state.calls.at(-1)?.search).toBeUndefined();
});

it("shows loading, not an empty state, while a cleared search reloads", () => {
  state.result = { isPlaceholderData: true };
  renderAt("/debug");
  expect(
    screen.queryByText(
      "No connections have been recorded for this gateway yet",
    ),
  ).toBeNull();
  expect(screen.queryByText("No connections match your search")).toBeNull();
});

it("keeps load failures inline rather than in the error boundary", () => {
  renderAt("/debug");
  expect(state.options.at(-1)).toMatchObject({ throwOnError: false });
});
