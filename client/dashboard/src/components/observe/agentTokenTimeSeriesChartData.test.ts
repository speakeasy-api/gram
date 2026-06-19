import { describe, expect, it } from "vitest";
import { buildAgentTokenTimeSeriesChartData } from "./agentTokenTimeSeriesChartData";

describe("buildAgentTokenTimeSeriesChartData", () => {
  it("preserves category chart data and returns timestamps for zoom", () => {
    const first = Date.UTC(2026, 0, 1, 12, 0);
    const second = Date.UTC(2026, 0, 1, 13, 0);

    const result = buildAgentTokenTimeSeriesChartData(
      [
        {
          bucketTimeUnixNano: `${BigInt(first) * BigInt(1_000_000)}`,
          totalInputTokens: 10,
          totalOutputTokens: 20,
          cacheReadInputTokens: 5,
          totalCost: 0.1,
        },
        {
          bucketTimeUnixNano: `${BigInt(second) * BigInt(1_000_000)}`,
          totalInputTokens: 30,
          totalOutputTokens: 40,
          cacheReadInputTokens: 15,
          totalCost: 0.2,
        },
      ],
      second - first,
      "tokens",
    );

    expect(result.timestamps).toEqual([first, second]);
    expect(result.chartData.labels).toHaveLength(2);
    expect(result.chartData.datasets[0]?.data).toEqual([10, 30]);
    expect(result.chartData.datasets[1]?.data).toEqual([20, 40]);
    expect(result.chartData.datasets[2]?.data).toEqual([5, 15]);
    expect(result.chartData.datasets[3]?.data).toEqual([35, 85]);
  });

  it("builds cost data without changing the category chart shape", () => {
    const first = Date.UTC(2026, 0, 1, 12, 0);

    const result = buildAgentTokenTimeSeriesChartData(
      [
        {
          bucketTimeUnixNano: `${BigInt(first) * BigInt(1_000_000)}`,
          totalInputTokens: 10,
          totalOutputTokens: 20,
          cacheReadInputTokens: 5,
          totalCost: 0.123,
        },
      ],
      60 * 60 * 1000,
      "cost",
    );

    expect(result.timestamps).toEqual([first]);
    expect(result.chartData.datasets).toHaveLength(2);
    expect(result.chartData.datasets[0]?.data).toEqual([0.123]);
    expect(result.chartData.datasets[1]?.data).toEqual([0.123]);
  });
});
