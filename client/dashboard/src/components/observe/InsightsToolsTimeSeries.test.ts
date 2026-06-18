import { describe, expect, it } from "vitest";
import { buildToolUsageTimeSeries } from "./toolUsageTimeSeriesChartData";

describe("buildToolUsageTimeSeries", () => {
  it("preserves bucketed chart data and returns timestamps for zoom", () => {
    const first = Date.UTC(2026, 0, 1, 12, 0);
    const second = Date.UTC(2026, 0, 1, 13, 0);

    const result = buildToolUsageTimeSeries(
      [
        {
          bucketStartNs: `${BigInt(first) * BigInt(1_000_000)}`,
          eventCount: 3,
          targetLabel: "Server A",
        },
        {
          bucketStartNs: `${BigInt(second) * BigInt(1_000_000)}`,
          eventCount: 5,
          targetLabel: "Server A",
        },
      ],
      (point) => point.targetLabel,
      second - first,
    );

    expect(result.timestamps).toEqual([first, second]);
    expect(result.datasets[0]?.data).toEqual([3, 5]);
  });
});
