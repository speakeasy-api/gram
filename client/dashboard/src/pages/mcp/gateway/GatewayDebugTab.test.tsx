import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { GatewayDebugTab } from "./GatewayDebugTab";

const state = vi.hoisted(() => ({
  calls: [] as Array<Record<string, unknown>>,
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
  useUserSessionsInfinite: (request: Record<string, unknown>) => {
    state.calls.push(request);
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
        Refresh: () => null,
      },
    ),
  },
}));

afterEach(() => {
  cleanup();
  state.calls = [];
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
      "Sessions issued for this gateway. Sessions without a recorded origin are not shown.",
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
