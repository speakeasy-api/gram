import { cleanup, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { Outlet } from "@tanstack/react-router";
import { afterEach, expect, it, vi } from "vitest";
import { routeTree } from "@/routeTree.gen";
import { renderRouteTree } from "@/test/harness";
const create = vi.hoisted(() => vi.fn());
vi.mock("@/layouts/AdminLayout", () => ({ AdminLayout: () => <Outlet /> }));
vi.mock("./IssuerDetail", () => ({
  IssuerDetail: () => <Outlet />,
  IssuerOverview: () => <h1>Created overview</h1>,
  IssuerSettings: () => null,
  IssuerConvergence: () => null,
}));
vi.mock("@/lib/gramAdminClient", () => ({
  adminListGlobalIssuersQuery: () => ({
    queryKey: ["@gram/admin-client", "admin", "listGlobalIssuers"],
    queryFn: async () => ({ result: { items: [] } }),
  }),
  adminGetGlobalIssuerDuplicatePreflightQuery: () => ({
    queryKey: ["duplicates"],
    queryFn: async () => ({ matches: [] }),
  }),
  adminCreateGlobalIssuer: create,
}));
afterEach(cleanup);
it("navigates to the returned issuer ID only after invalidation completes", async () => {
  const cache = new QueryClient();
  let finish: () => void = () => {};
  const invalidation = vi.spyOn(cache, "invalidateQueries").mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  create.mockResolvedValue({ id: "returned-id" });
  const { router } = await renderRouteTree(routeTree, {
    initialPath: "/remote-session-issuers",
    queryClient: cache,
  });
  fireEvent.click(await screen.findByRole("button", { name: "Create issuer" }));
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://new.example" },
  });
  fireEvent.submit(screen.getByLabelText("Issuer URL").closest("form")!);
  await waitFor(() => expect(invalidation).toHaveBeenCalled());
  expect(router.state.location.pathname).toBe("/remote-session-issuers");
  finish();
  expect(
    await screen.findByRole("heading", { name: "Created overview" }),
  ).toBeTruthy();
  expect(router.state.location.pathname).toBe(
    "/remote-session-issuers/returned-id",
  );
});
