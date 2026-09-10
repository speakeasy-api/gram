import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
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
