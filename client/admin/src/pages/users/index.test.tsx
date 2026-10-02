import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import type { AdminListUsersResult } from "@/lib/gramAdminApi";
import { routeTree } from "@/routeTree.gen";
import { renderRouteTree } from "@/test/harness";
const mocks = vi.hoisted(() => ({ listUsers: vi.fn(), getSession: vi.fn() }));
vi.mock("@/lib/gramAdminApi", async (original) => ({
  ...(await original<typeof import("@/lib/gramAdminApi")>()),
  ...mocks,
}));
beforeEach(() => {
  mocks.getSession.mockResolvedValue({
    email: "staff@example.test",
    name: "Staff",
  });
  mocks.listUsers.mockReset().mockResolvedValue({
    users: [
      {
        id: "u1",
        display_name: "",
        email: "person@example.test",
        organizations: [],
        organization_count: 0,
      },
    ],
    total: 101,
    page: 1,
    limit: 50,
  });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});
describe("Users directory", () => {
  it("renders missing values and debounces valid edits for 300ms, resetting URL page", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/users?page=2",
    });
    await screen.findByText("person@example.test");
    expect(screen.getByText("Not recorded")).toBeTruthy();
    vi.useFakeTimers();
    const editor = screen.getByRole("textbox", { name: "Search users" });
    fireEvent.change(editor, { target: { value: "name:Al" } });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    fireEvent.change(editor, { target: { value: "name:Alice" } });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(299);
    });
    expect(mocks.listUsers).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(router.state.location.search).toMatchObject({
      q: "name:Alice",
      page: 1,
    });
    expect(mocks.listUsers).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("textbox", { name: "Search users" })).toBe(editor);
  });
  it("keeps labeled last-valid rows for invalid drafts", async () => {
    await renderRouteTree(routeTree, { initialPath: "/users" });
    await screen.findByText("person@example.test");
    fireEvent.change(screen.getByRole("textbox", { name: "Search users" }), {
      target: { value: "wrong:value" },
    });
    expect(screen.queryByText(/Showing last valid results/)).toBeNull();
    expect(screen.getByText("person@example.test")).toBeTruthy();
    expect(
      screen
        .getByRole("region", { name: "Users table" })
        .getAttribute("aria-busy"),
    ).toBe("true");
    expect(
      (screen.getByRole("button", { name: "Next" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(mocks.listUsers).toHaveBeenCalledTimes(1);
  });
  it("rejects malformed direct queries without widening them", async () => {
    await renderRouteTree(routeTree, { initialPath: "/users?q=wrong:value" });
    await screen.findByRole("textbox", { name: "Search users" });
    expect(mocks.listUsers).not.toHaveBeenCalled();
  });
  it("external navigation remounts even if restored URL equals the draft", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/users?q=Alice",
    });
    await screen.findByText("person@example.test");
    const oldInput = screen.getByRole("textbox", { name: "Search users" });
    fireEvent.change(oldInput, { target: { value: "Bob" } });
    await act(async () => {
      await router.navigate({ to: "/users", search: { q: "Bob" } });
    });
    expect(screen.getByRole("textbox", { name: "Search users" })).not.toBe(
      oldInput,
    );
    await waitFor(() => expect(mocks.listUsers).toHaveBeenCalledTimes(2));
    vi.useFakeTimers();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(301);
    });
    expect(mocks.listUsers).toHaveBeenCalledTimes(2);
  });
});

it("keeps editor identity on own replaces, clears page, and restores navigation history", async () => {
  const { router } = await renderRouteTree(routeTree, {
    initialPath: "/users?page=2",
  });
  await screen.findByText("person@example.test");
  const input = screen.getByRole("textbox", { name: "Search users" });
  vi.useFakeTimers();
  fireEvent.change(input, { target: { value: "email:alice" } });
  fireEvent.keyDown(input, { key: "Enter" });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(300);
  });
  expect(
    screen.getByRole("button", { name: "Edit email filter: alice" }),
  ).toBeTruthy();
  const editor = screen.getByRole("textbox", { name: "Search users" });
  fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1);
  });
  expect(router.state.location.search).toEqual({ page: 1 });
  expect(screen.getByRole("textbox", { name: "Search users" })).toBe(editor);
  vi.useRealTimers();
  await act(async () => {
    await router.navigate({ to: "/users", search: { q: "email:before" } });
  });
  await act(async () => {
    await router.navigate({ to: "/users", search: { q: "email:after" } });
  });
  fireEvent.change(screen.getByRole("textbox", { name: "Search users" }), {
    target: { value: "pending" },
  });
  await act(async () => {
    router.history.back();
  });
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Edit email filter: before" }),
    ).toBeTruthy(),
  );
  const restored = screen.getByRole("textbox", { name: "Search users" });
  fireEvent.keyDown(restored, { key: "z", ctrlKey: true });
  expect(
    screen.getByRole("button", { name: "Edit email filter: before" }),
  ).toBeTruthy();
  vi.useFakeTimers();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(301);
  });
  expect(router.state.location.search.q).toBe("email:before");
  vi.useRealTimers();
  await act(async () => {
    router.history.forward();
  });
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Edit email filter: after" }),
    ).toBeTruthy(),
  );
});
it("does not let a canceled old request replace current results", async () => {
  let resolveOld: (value: unknown) => void = () => {};
  const old = new Promise((resolve) => {
    resolveOld = resolve;
  });
  mocks.listUsers.mockImplementation((params) =>
    params.q === "old"
      ? old
      : Promise.resolve({
          users: [
            {
              id: "new",
              display_name: "New Person",
              email: "new@example.test",
              organizations: [],
              organization_count: 0,
            },
          ],
          total: 1,
          page: 1,
          limit: 50,
        }),
  );
  await renderRouteTree(routeTree, { initialPath: "/users?q=old" });
  const input = await screen.findByRole("textbox", { name: "Search users" });
  const oldSignal = mocks.listUsers.mock.calls[0]?.[1] as AbortSignal;
  vi.useFakeTimers();
  fireEvent.change(input, { target: { value: "new" } });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(300);
  });
  expect(oldSignal.aborted).toBe(true);
  await act(async () => {
    resolveOld({
      users: [
        {
          id: "old",
          display_name: "Old Person",
          email: "old@example.test",
          organizations: [],
          organization_count: 0,
        },
      ],
      total: 1,
      page: 1,
      limit: 50,
    });
    await vi.advanceTimersByTimeAsync(1);
  });
  expect(screen.getByText("New Person")).toBeTruthy();
  expect(screen.queryByText("Old Person")).toBeNull();
});

it("shows refresh errors, exact timestamps, empty success and bounds validation", async () => {
  const { usersSearchSchema } = await import("@/lib/usersSearchRoute");
  expect(() => usersSearchSchema({ q: 23 })).toThrow("Search must be text");
  for (const page of [-1, 0, 1.5, "bad", 42949674])
    expect(() => usersSearchSchema({ page })).toThrow("Invalid users page");
  mocks.listUsers.mockResolvedValueOnce({
    users: [
      {
        id: "dated",
        display_name: "Dated",
        email: "dated@example.test",
        last_login: "2026-09-28T12:34:56Z",
        organizations: [],
        organization_count: 0,
      },
    ],
    total: 1,
    page: 1,
    limit: 50,
  });
  const { router } = await renderRouteTree(routeTree, {
    initialPath: "/users",
  });
  const dated = await screen.findByText("Dated");
  expect(dated.closest("tr")?.querySelector("time")?.title).toBe(
    "2026-09-28T12:34:56.000Z",
  );
  mocks.listUsers.mockRejectedValueOnce(new Error("Unavailable"));
  await act(async () => {
    await router.navigate({ to: "/users", search: { q: "failure" } });
  });
  expect(await screen.findByRole("alert")).toBeTruthy();
  mocks.listUsers.mockResolvedValueOnce({
    users: [],
    total: 0,
    page: 1,
    limit: 50,
  });
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByText("No users found")).toBeTruthy();
});

it("labels committed-query placeholders and traverses first/final page boundaries", async () => {
  const result = (
    name: string,
    page: number,
    total: number,
  ): AdminListUsersResult => ({
    users: [
      {
        id: name,
        display_name: name,
        email: `${name}@example.test`,
        organizations: [],
        organization_count: 0,
      },
    ],
    total,
    page,
    limit: 50,
  });
  const first = result("New first", 1, 51);
  const final = result("New final", 2, 51);
  let resolveNew: (value: AdminListUsersResult) => void = () => {};
  const deferred = new Promise<AdminListUsersResult>((resolve) => {
    resolveNew = resolve;
  });
  mocks.listUsers.mockImplementation(({ q, page }) => {
    if (q === "old") return Promise.resolve(result("Old result", 2, 101));
    return page === 2 ? Promise.resolve(final) : deferred;
  });
  const { router } = await renderRouteTree(routeTree, {
    initialPath: "/users?q=old&page=2",
  });
  await screen.findByText("Old result");
  const previous = (): HTMLButtonElement =>
    screen.getByRole("button", { name: "Previous" });
  const next = (): HTMLButtonElement =>
    screen.getByRole("button", { name: "Next" });
  expect(previous().disabled).toBe(false);
  expect(next().disabled).toBe(false);

  vi.useFakeTimers();
  fireEvent.change(screen.getByRole("textbox", { name: "Search users" }), {
    target: { value: "new" },
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(300);
  });
  // The debounce has committed; these are placeholders for an in-flight new
  // query, not merely rows retained while the draft waits to reach the URL.
  expect(router.state.location.search).toEqual({ q: "new", page: 1 });
  expect(mocks.listUsers).toHaveBeenLastCalledWith(
    { q: "new", page: 1, limit: 50 },
    expect.any(AbortSignal),
  );
  expect(
    screen
      .getByRole("region", { name: "Users table" })
      .getAttribute("aria-busy"),
  ).toBe("true");
  expect(screen.getByText("Old result")).toBeTruthy();
  expect(screen.queryByText("New first")).toBeNull();
  expect(previous().disabled).toBe(true);
  expect(next().disabled).toBe(true);

  await act(async () => {
    resolveNew(first);
    await vi.advanceTimersByTimeAsync(1);
  });
  vi.useRealTimers();
  await screen.findByText("New first");
  expect(screen.queryByText("Old result")).toBeNull();
  expect(
    screen
      .getByRole("region", { name: "Users table" })
      .getAttribute("aria-busy"),
  ).toBeNull();
  expect(previous().disabled).toBe(true);
  expect(next().disabled).toBe(false);

  fireEvent.click(next());
  await screen.findByText("New final");
  expect(router.state.location.search).toEqual({ q: "new", page: 2 });
  expect(mocks.listUsers).toHaveBeenLastCalledWith(
    { q: "new", page: 2, limit: 50 },
    expect.any(AbortSignal),
  );
  expect(screen.queryByText("New first")).toBeNull();
  expect(previous().disabled).toBe(false);
  expect(next().disabled).toBe(true);

  fireEvent.click(previous());
  await screen.findByText("New first");
  await waitFor(() => expect(next().disabled).toBe(false));
  expect(router.state.location.search).toEqual({ q: "new", page: 1 });
  expect(screen.queryByText("New final")).toBeNull();
  expect(previous().disabled).toBe(true);
});

it("renders an invalid last login without an ISO tooltip", async () => {
  mocks.listUsers.mockResolvedValueOnce({
    users: [
      {
        id: "invalid-date",
        display_name: "Invalid date",
        email: "invalid@example.test",
        last_login: "not-a-date",
        organizations: [],
        organization_count: 0,
      },
    ],
    total: 1,
    page: 1,
    limit: 50,
  });
  await renderRouteTree(routeTree, { initialPath: "/users" });
  const row = (await screen.findByText("Invalid date")).closest("tr");
  expect(row?.querySelector("time")?.hasAttribute("title")).toBe(false);
  expect(row?.querySelector("time")?.textContent).toBe("-");
});
