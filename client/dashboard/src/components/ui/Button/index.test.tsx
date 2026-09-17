import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { Button } from ".";

afterEach(cleanup);

it.each([
  ["xs", "2"],
  ["sm", "3"],
  ["md", "4"],
  ["lg", "6"],
] as const)(
  "retains padding with size-aware edge alignment (%s)",
  (size, space) => {
    const { rerender } = render(
      <Button size={size} variant="tertiary" edge="start">
        Action
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Action" });
    expect(button.classList.contains(`px-${space}`)).toBe(true);
    expect(button.classList.contains(`-ms-${space}`)).toBe(true);
    expect(button.hasAttribute("edge")).toBe(false);
    rerender(
      <Button size={size} variant="tertiary" edge="end">
        Action
      </Button>,
    );
    expect(button.classList.contains(`-me-${space}`)).toBe(true);
    expect(button.classList.contains(`-ms-${space}`)).toBe(false);
  },
);

it("leaves regular button spacing unchanged", () => {
  render(<Button variant="tertiary">Action</Button>);
  expect(screen.getByRole("button").className).not.toMatch(/-m[se]-/);
});

it("supports padded edge alignment on disabled and slotted controls", () => {
  const { rerender } = render(
    <Button disabled size="sm" variant="tertiary" edge="start">
      Back
    </Button>,
  );
  expect(screen.getByRole("button").classList.contains("-ms-3")).toBe(true);
  rerender(
    <Button asChild size="sm" variant="tertiary" edge="start">
      <a href="/setup">Back</a>
    </Button>,
  );
  const link = screen.getByRole("link", { name: "Back" });
  expect(link.classList.contains("px-3")).toBe(true);
  expect(link.classList.contains("-ms-3")).toBe(true);
  expect(link.hasAttribute("edge")).toBe(false);
});
