import { describe, expect, it } from "vitest";
import { buildRiskTrendChartData } from "./riskTrendChartData";

describe("buildRiskTrendChartData", () => {
  it("returns timestamped line data for chart zoom", () => {
    const first = new Date(Date.UTC(2026, 0, 1, 12, 0));
    const second = new Date(Date.UTC(2026, 0, 1, 13, 0));

    const result = buildRiskTrendChartData(
      [
        { category: "secrets", bucketStart: first, findings: 2 },
        { category: "secrets", bucketStart: second, findings: 4 },
      ],
      first,
      second,
    );

    expect(result.timestamps).toEqual([first.getTime(), second.getTime()]);
    expect(result.datasets[0]?.data).toEqual([
      { x: first.getTime(), y: 2 },
      { x: second.getTime(), y: 4 },
    ]);
  });
});
