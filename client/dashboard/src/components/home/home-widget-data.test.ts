// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  buildHeatmap,
  computeMovers,
  dailyCost,
  heatmapKey,
} from "./home-widget-data";

const ORIGINAL_TZ = process.env.TZ;
beforeEach(() => {
  process.env.TZ = "UTC";
});
afterEach(() => {
  if (ORIGINAL_TZ === undefined) delete process.env.TZ;
  else process.env.TZ = ORIGINAL_TZ;
});

function bucket(iso: string, totalCost: number, totalToolCalls = 0) {
  return {
    bucketTimeUnixNano: String(BigInt(Date.parse(iso)) * 1_000_000n),
    totalCost,
    totalToolCalls,
  };
}

describe("dailyCost", () => {
  it("sums hourly buckets into days, oldest first", () => {
    expect(
      dailyCost([
        bucket("2026-11-19T10:00:00Z", 2),
        bucket("2026-11-18T23:00:00Z", 1),
        bucket("2026-11-19T11:00:00Z", 3),
      ]),
    ).toEqual([1, 5]);
  });

  it("returns nothing for no buckets", () => {
    expect(dailyCost([])).toEqual([]);
  });
});

describe("computeMovers", () => {
  const server = (serverName: string, toolCallCount: number) => ({
    serverName,
    toolCallCount,
  });

  it("ranks by absolute change and treats a missing week as zero", () => {
    expect(
      computeMovers(
        [server("steady", 10), server("falling", 50), server("gone", 5)],
        [server("steady", 10), server("falling", 20), server("new", 40)],
      ),
    ).toEqual([
      { name: "new", prev: 0, curr: 40, delta: 40 },
      { name: "falling", prev: 50, curr: 20, delta: -30 },
      { name: "gone", prev: 5, curr: 0, delta: -5 },
    ]);
  });

  it("leaves out a server missing from a full top-N list", () => {
    const full = Array.from({ length: 10 }, (_, i) => server(`s${i}`, 100));
    // "ghost" may rank 11th this week, so its change is unknown.
    const names = computeMovers(
      [server("s0", 50), server("ghost", 30)],
      full,
      20,
    ).map((m) => m.name);
    expect(names).toContain("s0");
    expect(names).not.toContain("ghost");
    // Same the other way round.
    expect(
      computeMovers(full, [server("s0", 50), server("fresh", 5)], 20).map(
        (m) => m.name,
      ),
    ).not.toContain("fresh");
  });

  it("caps the list", () => {
    const current = Array.from({ length: 8 }, (_, i) => server(`s${i}`, i + 1));
    expect(computeMovers([], current, 3).map((m) => m.name)).toEqual([
      "s7",
      "s6",
      "s5",
    ]);
  });
});

describe("buildHeatmap", () => {
  it("buckets tool calls by local day and hour", () => {
    const map = buildHeatmap([
      bucket("2026-11-19T14:00:00Z", 0, 3),
      bucket("2026-11-18T09:00:00Z", 0, 7),
      bucket("2026-11-19T14:00:00Z", 0, 2),
    ]);
    expect(map.days.map((d) => d.toDateString())).toEqual([
      "Wed Nov 18 2026",
      "Thu Nov 19 2026",
    ]);
    expect(map.cells.get(heatmapKey(map.days[1]!, 14))).toBe(5);
    expect(map.cells.get(heatmapKey(map.days[0]!, 9))).toBe(7);
    expect(map.max).toBe(7);
  });

  it("keeps max at least 1 when there are no calls", () => {
    expect(buildHeatmap([]).max).toBe(1);
  });

  it("totals zero for a zero-filled week", () => {
    const map = buildHeatmap([
      bucket("2026-11-18T09:00:00Z", 0, 0),
      bucket("2026-11-19T09:00:00Z", 0, 0),
    ]);
    expect(map.days).toHaveLength(2);
    expect(map.total).toBe(0);
  });
});
