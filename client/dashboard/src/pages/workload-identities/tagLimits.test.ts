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
