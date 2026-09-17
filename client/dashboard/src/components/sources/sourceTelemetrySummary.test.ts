import type { ToolMetric } from "@gram/client/models/components/toolmetric.js";
import { describe, expect, it } from "vitest";
import {
  computeTelemetrySummary,
  formatLatency,
  selectSourceToolMetrics,
  toolNameFromUrn,
} from "./sourceTelemetrySummary";

function metric(overrides: Partial<ToolMetric> & { gramUrn: string }) {
  return {
    avgLatencyMs: 0,
    callCount: 0,
    failureCount: 0,
    failureRate: 0,
    successCount: 0,
    ...overrides,
  } satisfies ToolMetric;
}

describe("computeTelemetrySummary", () => {
  it("returns null when there is no activity", () => {
    expect(computeTelemetrySummary([])).toBeNull();
  });

  it("weights latency by call count", () => {
    const summary = computeTelemetrySummary([
      metric({
        gramUrn: "tools:http:a:fast",
        callCount: 90,
        avgLatencyMs: 100,
      }),
      metric({
        gramUrn: "tools:http:a:slow",
        callCount: 10,
        avgLatencyMs: 1000,
      }),
    ]);
    expect(summary).toEqual({
      totalCalls: 100,
      totalFailures: 0,
      avgLatencyMs: 190,
      errorRate: 0,
    });
  });

  it("reports the error rate as a percentage of calls", () => {
    const summary = computeTelemetrySummary([
      metric({ gramUrn: "tools:http:a:x", callCount: 40, failureCount: 2 }),
      metric({ gramUrn: "tools:http:a:y", callCount: 10, failureCount: 3 }),
    ]);
    expect(summary?.totalFailures).toBe(5);
    expect(summary?.errorRate).toBe(10);
  });
});

describe("selectSourceToolMetrics", () => {
  it("keeps only the source's tools, in the ranking's order", () => {
    const metrics = [
      metric({ gramUrn: "tools:http:other:z", callCount: 50 }),
      metric({ gramUrn: "tools:http:a:x", callCount: 20 }),
      metric({ gramUrn: "tools:http:a:y", callCount: 5 }),
    ];
    expect(
      selectSourceToolMetrics(metrics, ["tools:http:a:y", "tools:http:a:x"]),
    ).toEqual([metrics[1], metrics[2]]);
  });

  it("returns nothing for a source with no tools", () => {
    expect(
      selectSourceToolMetrics([metric({ gramUrn: "tools:http:a:x" })], []),
    ).toEqual([]);
  });
});

describe("formatLatency", () => {
  it("shows milliseconds under a second and seconds above", () => {
    expect(formatLatency(320.4)).toBe("320ms");
    expect(formatLatency(1250)).toBe("1.3s");
  });
});

describe("toolNameFromUrn", () => {
  it("takes the trailing segment and falls back to the urn", () => {
    expect(toolNameFromUrn("tools:http:petstore:list_pets")).toBe("list_pets");
    expect(toolNameFromUrn("list_pets")).toBe("list_pets");
  });
});
