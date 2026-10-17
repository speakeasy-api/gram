import { describe, expect, it, vi } from "vitest";
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

  it("rejects a limit that would never run anything", async () => {
    const fn = vi.fn(async (n: number) => n);
    await expect(mapWithConcurrency([1, 2], 0, fn)).rejects.toThrow(RangeError);
    await expect(mapWithConcurrency([1, 2], 1.5, fn)).rejects.toThrow(
      RangeError,
    );
    expect(fn).not.toHaveBeenCalled();
  });

  it("maps nothing to nothing", async () => {
    expect(await mapWithConcurrency([], 3, () => Promise.resolve(1))).toEqual(
      [],
    );
  });
});
