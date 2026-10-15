import { describe, expect, it } from "vitest";
import { mapWithConcurrency } from "./bounded-concurrency";

describe("mapWithConcurrency", () => {
  it("keeps at most limit calls in flight and results in order", async () => {
    let inFlight = 0;
    let peak = 0;
    const results = await mapWithConcurrency(
      [1, 2, 3, 4, 5, 6, 7],
      3,
      async (n) => {
        inFlight++;
        peak = Math.max(peak, inFlight);
        await new Promise<void>((resolve) => {
          setTimeout(resolve, 8 - n);
        });
        inFlight--;
        return n * 10;
      },
    );
    expect(peak).toBe(3);
    expect(results).toEqual([10, 20, 30, 40, 50, 60, 70]);
  });

  it("maps nothing to nothing", async () => {
    expect(await mapWithConcurrency([], 3, () => Promise.resolve(1))).toEqual(
      [],
    );
  });
});
