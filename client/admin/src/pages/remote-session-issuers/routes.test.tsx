import {
  cleanup,
  screen,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Outlet } from "@tanstack/react-router";
import { renderRouteTree } from "@/test/harness";
import { routeTree } from "@/routeTree.gen";
vi.mock("./ConvergenceSummary", () => ({ ConvergenceSummary: () => null }));
vi.mock("@/layouts/AdminLayout", () => ({ AdminLayout: () => <Outlet /> }));
vi.mock("@/pages/remote-session-issuers/IssuerEditor", () => ({
  IssuerEditor: () => <h2>Settings form</h2>,
}));
vi.mock("@/pages/remote-session-issuers/Convergence", () => ({
  Convergence: () => <h2>Candidate review</h2>,
  ConvergenceHelp: () => null,
}));
const remove = vi.hoisted(() => vi.fn());
const refresh = vi.hoisted(() => vi.fn());
vi.mock("@/lib/gramAdminClient", () => ({
  adminDeleteGlobalIssuer: remove,
  adminRefreshGlobalIssuerMetadata: refresh,
  adminGetGlobalIssuerQuery: ({ id }: { id: string }) => ({
    queryKey: ["issuer", id],
    queryFn: async () => ({
      issuer: {
        id,
        name: "Example provider",
        slug: "example",
        issuer: "https://issuer.example",
      },
      globalClientCount: 2,
      tenantClientCount: 3,
    }),
  }),
}));
beforeEach(() => {
  remove.mockReset();
  refresh.mockReset().mockResolvedValue({ discoveryWarnings: [] });
});
afterEach(cleanup);
it("supports direct settings entry and native overview/convergence navigation", async () => {
  const { router } = await renderRouteTree(routeTree, {
    initialPath: "/remote-session-issuers/example/settings",
  });
  expect(
    await screen.findByRole("heading", { name: "Settings form" }),
  ).toBeTruthy();
  expect(
    screen.getByRole("link", { name: "Settings" }).getAttribute("href"),
  ).toBe("/remote-session-issuers/example/settings");
  fireEvent.click(screen.getByRole("link", { name: "Overview" }));
  await waitFor(() =>
    expect(router.state.location.pathname).toBe(
      "/remote-session-issuers/example",
    ),
  );
  expect(await screen.findByText("Platform / tenant clients")).toBeTruthy();
  fireEvent.click(screen.getByRole("link", { name: "Convergence" }));
  expect(
    await screen.findByRole("heading", { name: "Candidate review" }),
  ).toBeTruthy();
  expect(router.state.location.pathname).toBe(
    "/remote-session-issuers/example/convergence",
  );
  router.history.back();
  await waitFor(() =>
    expect(router.state.location.pathname).toBe(
      "/remote-session-issuers/example",
    ),
  );
  router.history.forward();
  await waitFor(() =>
    expect(router.state.location.pathname).toBe(
      "/remote-session-issuers/example/convergence",
    ),
  );
});

it("keeps server dependency-race errors visible after delete confirmation", async () => {
  remove.mockRejectedValue(new Error("Tenant dependencies changed"));
  const { router } = await renderRouteTree(routeTree, {
    initialPath: "/remote-session-issuers/example",
  });
  fireEvent.click(await screen.findByRole("button", { name: "Delete issuer" }));
  expect(await screen.findByText(/Counts are advisory/)).toBeTruthy();
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(
    within(dialog).getByRole("button", { name: "Delete issuer" }),
  );
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Tenant dependencies changed",
  );
  expect(remove).toHaveBeenCalledWith({ id: "example" });
  expect(router.state.location.pathname).toBe(
    "/remote-session-issuers/example",
  );
});

it("refreshes issuer metadata and displays discovery warnings", async () => {
  refresh.mockResolvedValue({
    discoveryWarnings: ["Discovery endpoint unavailable"],
  });
  await renderRouteTree(routeTree, {
    initialPath: "/remote-session-issuers/example",
  });
  fireEvent.click(
    await screen.findByRole("button", { name: "Refresh metadata" }),
  );
  expect(
    await screen.findByText("Discovery endpoint unavailable"),
  ).toBeTruthy();
  expect(refresh).toHaveBeenCalledExactlyOnceWith({ id: "example" });
});
