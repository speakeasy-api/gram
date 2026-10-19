import { describe, expect, it } from "vitest";

import { visibleChipCount } from "./chipOverflow";

describe("visibleChipCount", () => {
  it("shows every chip when they all fit", () => {
    expect(visibleChipCount([50, 50, 50], 166, 8, 40)).toBe(3);
  });

  it("keeps room for the overflow chip when some do not fit", () => {
    // Two chips take 108px, but the overflow chip after them needs 48 more.
    expect(visibleChipCount([50, 50, 50], 150, 8, 40)).toBe(1);
  });

  it("can hide every chip behind the overflow chip", () => {
    expect(visibleChipCount([120, 50], 100, 8, 40)).toBe(0);
  });

  it("shows nothing for no chips", () => {
    expect(visibleChipCount([], 0, 8, 40)).toBe(0);
  });
});
