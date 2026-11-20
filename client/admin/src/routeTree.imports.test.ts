import { createMemoryHistory, createRouter } from "@tanstack/react-router";
import { expect, it, vi } from "vitest";

const evaluated = vi.hoisted(() => ({
  users: vi.fn(),
  organizations: vi.fn(),
  fleetHealth: vi.fn(),
}));

vi.mock("@/pages/users", async (importOriginal) => {
  evaluated.users();
  return importOriginal<typeof import("@/pages/users")>();
});
vi.mock("@/pages/organizations/index", async (importOriginal) => {
  evaluated.organizations();
  return importOriginal<typeof import("@/pages/organizations/index")>();
});
vi.mock("@/pages/fleet-health/FleetHealth", async (importOriginal) => {
  evaluated.fleetHealth();
  return importOriginal<typeof import("@/pages/fleet-health/FleetHealth")>();
});

it("loads the visited page without eagerly evaluating unvisited page components", async () => {
  const { routeTree } = await import("./routeTree.gen");
  expect(evaluated.users).not.toHaveBeenCalled();
  expect(evaluated.organizations).not.toHaveBeenCalled();
  expect(evaluated.fleetHealth).not.toHaveBeenCalled();

  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/users"] }),
  });
  await router.load();

  expect(router.state.status).toBe("idle");
  expect(evaluated.users).toHaveBeenCalledOnce();
  expect(evaluated.organizations).not.toHaveBeenCalled();
  expect(evaluated.fleetHealth).not.toHaveBeenCalled();
});
