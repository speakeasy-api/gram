import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithApp } from "@/test/harness";
const list = vi.hoisted(() => vi.fn());
vi.mock("@gram/admin-client/react-query/adminListRegistryEntries.core", () => ({
  buildAdminListRegistryEntriesQuery: (_client: unknown, request: unknown) => ({
    queryKey: ["@gram/admin-client", "admin", "listRegistryEntries", request],
    queryFn: () => list(request),
  }),
}));
import { QueryClient } from "@tanstack/react-query";
import { RegistryList } from "./RegistryList";
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
it("bounds pages, sends search and publication filters, resets cursor and shows invalid summaries", async () => {
  list.mockImplementation(({ cursor }) =>
    Promise.resolve({
      entries: [
        {
          id: "00000000-0000-4000-8000-000000000001",
          name: "example.test/demo",
          published: false,
          updatedAt: "opaque",
          issues: [{ path: "/server", message: "repair required" }],
        },
      ],
      nextCursor: cursor ? undefined : "next-page",
    }),
  );
  await renderWithApp(<RegistryList />);
  await screen.findByText("1 issues");
  expect(list.mock.calls[0]?.[0]).toEqual({
    query: undefined,
    published: undefined,
    cursor: undefined,
    limit: 25,
  });
  expect(
    (screen.getByRole("button", { name: "Previous" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "Next" }));
  await waitFor(() =>
    expect(list).toHaveBeenCalledWith(
      expect.objectContaining({ cursor: "next-page", limit: 25 }),
    ),
  );
  expect(screen.getByText("Page 2")).toBeTruthy();
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Next" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true),
  );
  fireEvent.click(screen.getByRole("button", { name: "Previous" }));
  await screen.findByText("Page 1");
  expect(
    (screen.getByRole("button", { name: "Previous" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  await waitFor(() => expect(list.mock.lastCall?.[0].cursor).toBeUndefined());
  fireEvent.click(screen.getByRole("button", { name: "Next" }));
  await screen.findByText("Page 2");
  fireEvent.change(screen.getByRole("textbox", { name: "Search registry" }), {
    target: { value: "demo" },
  });
  await waitFor(() =>
    expect(list).toHaveBeenCalledWith(
      expect.objectContaining({ query: "demo", cursor: undefined }),
    ),
  );
  fireEvent.change(screen.getByRole("combobox", { name: "Publication" }), {
    target: { value: "unpublished" },
  });
  await waitFor(() =>
    expect(list).toHaveBeenCalledWith(
      expect.objectContaining({
        query: "demo",
        published: false,
        cursor: undefined,
      }),
    ),
  );
  expect(screen.getByText("Page 1")).toBeTruthy();
});

it("retries a failed list request", async () => {
  list
    .mockRejectedValueOnce(new Error("List unavailable"))
    .mockResolvedValue({ entries: [] });
  await renderWithApp(<RegistryList />);
  await screen.findByRole("alert");
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await screen.findByText("No entries found.");
  expect(screen.queryByRole("alert")).toBeNull();
});
it("shows an alert when a refetch of a cached empty list fails", async () => {
  list
    .mockResolvedValueOnce({ entries: [] })
    .mockRejectedValue(new Error("List unavailable"));
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await renderWithApp(<RegistryList />, { queryClient });
  await screen.findByText("No entries found.");
  await act(async () => {
    await queryClient.invalidateQueries();
  });
  await screen.findByRole("alert");
  expect(screen.queryByText("No entries found.")).toBeNull();
});
