import { describe, expect, it } from "vitest";
import { describeDashboards } from "./widgetUsage";

const dashboards = (...names: string[]) =>
  names.map((name, index) => ({ id: `d-${index}`, name }));

describe("describeDashboards", () => {
  it("names up to three dashboards as a phrase", () => {
    expect(describeDashboards([])).toBe("");
    expect(describeDashboards(dashboards("Agent activity"))).toBe(
      "“Agent activity”",
    );
    expect(describeDashboards(dashboards("Agent activity", "Costs"))).toBe(
      "“Agent activity” and “Costs”",
    );
    expect(
      describeDashboards(dashboards("Agent activity", "Costs", "Latency")),
    ).toBe("“Agent activity”, “Costs” and “Latency”");
  });

  it("counts the rest past three", () => {
    expect(describeDashboards(dashboards("A", "B", "C", "D"))).toBe(
      "“A”, “B”, “C” and 1 more dashboard",
    );
    expect(describeDashboards(dashboards("A", "B", "C", "D", "E"))).toBe(
      "“A”, “B”, “C” and 2 more dashboards",
    );
  });
});
