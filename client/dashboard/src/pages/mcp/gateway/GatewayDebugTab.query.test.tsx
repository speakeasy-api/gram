import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Component, type ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { GatewayDebugTab } from "./GatewayDebugTab";

type Page = { result: { items: { id: string }[] }; "~next"?: string };

const server = vi.hoisted(() => ({
  fetch: vi.fn<(cursor: string | undefined) => Promise<Page>>(),
}));

vi.mock("@gram/client/react-query/userSessions.js", async () => {
  const { useInfiniteQuery } = await import("@tanstack/react-query");
  return {
    useUserSessionsInfinite: (
      request: Record<string, unknown>,
      _security: unknown,
      options: Record<string, unknown>,
    ) =>
      useInfiniteQuery({
        queryKey: ["user-sessions", request],
        queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
          server.fetch(pageParam),
        initialPageParam: undefined as string | undefined,
        getNextPageParam: (page: Page) => page["~next"],
        ...options,
      }),
  };
});
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_a", slug: "first" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => false }),
}));
vi.mock("@/hooks/useSessionAgents", () => ({
  useSessionAgents: (sessions: unknown[]) => sessions,
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
        Search: () => null,
        Refresh: ({ onRefresh }: { onRefresh: () => void }) => (
          <button type="button" onClick={onRefresh}>
            Refresh
          </button>
        ),
      },
    ),
  },
}));

class Boundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? <p>error boundary</p> : this.props.children;
  }
}

afterEach(() => {
  cleanup();
  server.fetch.mockReset();
});

function renderTab() {
  const client = new QueryClient({
    defaultOptions: { queries: { throwOnError: true, retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/debug"]}>
        <Boundary>
          <GatewayDebugTab gatewayId="gateway-1" />
        </Boundary>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const page = (id: string, next?: string): Page => ({
  result: { items: [{ id }] },
  "~next": next,
});

it("shows a failed first load inline and recovers on retry", async () => {
  server.fetch.mockRejectedValueOnce(new Error("500"));
  renderTab();
  expect(await screen.findByText("Couldn't load connections.")).toBeTruthy();
  expect(screen.queryByText("error boundary")).toBeNull();

  server.fetch.mockResolvedValueOnce(page("s1"));
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByText("s1")).toBeTruthy();
  expect(screen.queryByText("Couldn't load connections.")).toBeNull();
});

it("keeps rows through a failed refresh and recovers on retry", async () => {
  server.fetch.mockResolvedValueOnce(page("s1"));
  renderTab();
  expect(await screen.findByText("s1")).toBeTruthy();

  server.fetch.mockRejectedValueOnce(new Error("500"));
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  expect(await screen.findByText("Couldn’t refresh connections.")).toBeTruthy();
  expect(screen.getByText("s1")).toBeTruthy();
  expect(screen.queryByText("error boundary")).toBeNull();

  server.fetch.mockResolvedValueOnce(page("s1-updated"));
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByText("s1-updated")).toBeTruthy();
  expect(screen.queryByText("s1")).toBeNull();
  expect(screen.queryByText("Couldn’t refresh connections.")).toBeNull();
});

it("keeps rows through a failed next page and recovers on retry", async () => {
  server.fetch.mockResolvedValueOnce(page("s1", "cursor-2"));
  renderTab();
  expect(await screen.findByText("s1")).toBeTruthy();

  server.fetch.mockRejectedValueOnce(new Error("500"));
  fireEvent.click(screen.getByRole("button", { name: "Load more" }));
  expect(
    await screen.findByText("Couldn’t load more connections."),
  ).toBeTruthy();
  expect(screen.getByText("s1")).toBeTruthy();
  expect(screen.queryByText("error boundary")).toBeNull();

  server.fetch.mockResolvedValueOnce(page("s2"));
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByText("s2")).toBeTruthy();
  expect(screen.getByText("s1")).toBeTruthy();
  expect(screen.queryByText("Couldn’t load more connections.")).toBeNull();
  expect(server.fetch.mock.calls).toEqual([
    [undefined],
    ["cursor-2"],
    ["cursor-2"],
  ]);
});
