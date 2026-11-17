import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

// A basic loading placeholder must not evaluate the table's dependency graph.
vi.mock("../Table", () => {
  throw new Error("Basic skeletons must not load Table");
});
vi.mock("motion/react", () => {
  throw new Error("Basic skeletons must not load motion");
});

afterEach(cleanup);

it("renders a basic skeleton without table or animation dependencies", async () => {
  const { Skeleton } = await import(".");
  render(<Skeleton label="Loading name" className="w-24" />);

  expect(
    screen.getByRole("status", { name: "Loading name" }).className,
  ).toContain("w-24");
});

it("renders loading text without table or animation dependencies", async () => {
  const { Text } = await import("../Text");
  const { container } = render(<Text skeleton="paragraph" />);

  expect(container.querySelectorAll(".skeleton")).toHaveLength(3);
});
