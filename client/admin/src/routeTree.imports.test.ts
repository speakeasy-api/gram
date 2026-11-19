import { expect, it, vi } from "vitest";

// Import the real route tree, including Overview and organizations. Unrelated
// routes must not evaluate the date picker's dependency graph.
vi.mock("react-day-picker", () => {
  throw new Error("The route tree must not eagerly load react-day-picker");
});

it("loads the complete route tree without evaluating the calendar", async () => {
  const { routeTree } = await import("./routeTree.gen");
  expect(Object.keys(routeTree.children ?? {})).not.toHaveLength(0);
});
