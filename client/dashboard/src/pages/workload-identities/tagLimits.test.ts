import { expect, it } from "vitest";
import { tagsProblem } from "./tagLimits";

it("accepts up to the column's limits", () => {
  expect(tagsProblem([])).toBeNull();
  expect(
    tagsProblem(Array.from({ length: 40 }, (_, i) => `tag-${i}`)),
  ).toBeNull();
  expect(tagsProblem(["x".repeat(64)])).toBeNull();
});

it("refuses a list or a tag the server would reject", () => {
  expect(tagsProblem(Array.from({ length: 41 }, (_, i) => `tag-${i}`))).toBe(
    "At most 40 tags.",
  );
  expect(tagsProblem(["x".repeat(65)])).toBe(
    "Each tag is at most 64 characters.",
  );
});

it("counts characters as the server does, not UTF-16 units", () => {
  // Each of these is one character to the server (a code point) but two UTF-16
  // units to JavaScript's length, so a 64-character tag of them is allowed.
  expect(tagsProblem(["🚀".repeat(64)])).toBeNull();
  expect(tagsProblem(["🚀".repeat(65)])).toBe(
    "Each tag is at most 64 characters.",
  );
});
