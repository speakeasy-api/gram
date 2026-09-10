import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import * as adminClient from "@/lib/gramAdminClient";
import { adminIssuerImageQuery } from "@/lib/gramAdminClient";
import { IssuerLogo } from "./IssuerLogo";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

it("clears and revokes the previous logo while a new image has no data", () => {
  const cache = new QueryClient({
    defaultOptions: { queries: { enabled: false } },
  });
  cache.setQueryData(
    adminIssuerImageQuery("first").queryKey,
    new Blob(["logo"]),
  );
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:first");
  const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  const view = render(
    <QueryClientProvider client={cache}>
      <IssuerLogo id="first" />
    </QueryClientProvider>,
  );
  expect(view.getByRole("img").getAttribute("src")).toBe("blob:first");
  view.rerender(
    <QueryClientProvider client={cache}>
      <IssuerLogo id="second" />
    </QueryClientProvider>,
  );
  expect(revoke).toHaveBeenCalledWith("blob:first");
  expect(view.queryByRole("img")).toBeNull();
});

// Keep the real query options and React Query observer; only replace transport.
it("shows an inline fallback even when the client defaults to throwing errors", async () => {
  const cache = new QueryClient({
    defaultOptions: { queries: { retry: false, throwOnError: true } },
  });
  const options = adminIssuerImageQuery("missing");
  vi.spyOn(adminClient, "adminIssuerImageQuery").mockReturnValue({
    ...options,
    queryFn: async () => {
      throw new Error("image unavailable");
    },
  });
  const view = render(
    <QueryClientProvider client={cache}>
      <IssuerLogo id="missing" />
    </QueryClientProvider>,
  );
  expect((await view.findByRole("alert")).textContent).toBe("Logo unavailable");
  expect(view.queryByRole("img")).toBeNull();
  view.unmount();
  cache.clear();
});

it("keeps a cached logo after refetch failure and revokes it on unmount", async () => {
  const cache = new QueryClient({
    defaultOptions: { queries: { retry: false, throwOnError: true } },
  });
  const options = adminIssuerImageQuery("cached");
  const blob = new Blob(["logo"]);
  const queryFn = vi
    .fn()
    .mockResolvedValueOnce(blob)
    .mockRejectedValue(new Error("temporary failure"));
  vi.spyOn(adminClient, "adminIssuerImageQuery").mockReturnValue({
    ...options,
    queryFn,
  });
  const create = vi
    .spyOn(URL, "createObjectURL")
    .mockReturnValue("blob:cached");
  const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  const view = render(
    <QueryClientProvider client={cache}>
      <IssuerLogo id="cached" />
    </QueryClientProvider>,
  );
  expect((await view.findByRole("img")).getAttribute("src")).toBe(
    "blob:cached",
  );
  await act(async () => {
    await cache.refetchQueries({ queryKey: options.queryKey });
  });
  await waitFor(() =>
    expect(cache.getQueryState(options.queryKey)?.status).toBe("error"),
  );
  expect(queryFn).toHaveBeenCalledTimes(2);
  expect(cache.getQueryData(options.queryKey)).toBe(blob);
  expect(view.getByRole("img").getAttribute("src")).toBe("blob:cached");
  expect(view.queryByRole("alert")).toBeNull();
  expect(create).toHaveBeenCalledTimes(1);
  expect(revoke).not.toHaveBeenCalled();
  view.unmount();
  expect(revoke).toHaveBeenCalledExactlyOnceWith("blob:cached");
  cache.clear();
});
