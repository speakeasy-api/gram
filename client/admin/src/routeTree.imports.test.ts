import { expect, it, vi } from "vitest";

vi.mock("react-day-picker", () => {
  throw new Error("The route tree must not eagerly load react-day-picker");
});

it("loads the complete route tree without evaluating the calendar", async () => {
  const { routeTree } = await import("./routeTree.gen");
  expect(Object.keys(routeTree.children ?? {})).not.toHaveLength(0);
});
