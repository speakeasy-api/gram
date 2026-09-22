import { describe, expect, it } from "vitest";
import { chunk } from "./utils";

describe("chunk", () => {
  it("splits into fixed batches and keeps order", () => {
    expect(chunk([1, 2, 3, 4, 5], 2)).toEqual([[1, 2], [3, 4], [5]]);
    expect(chunk([], 2)).toEqual([]);
  });
});
