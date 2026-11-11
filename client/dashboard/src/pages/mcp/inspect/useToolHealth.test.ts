// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  FAILING_RATE,
  MIN_CALLS_FOR_RATE,
  aggregateToolHealth,
  bareToolName,
  isFailing,
} from "./useToolHealth";

describe("isFailing", () => {
  it("is false with no health data", () => {
    expect(isFailing(undefined)).toBe(false);
  });

  it("ignores failure rates from too few calls", () => {
    expect(isFailing({ calls: MIN_CALLS_FOR_RATE - 1, failureRate: 1 })).toBe(
      false,
    );
  });

  it("flags a tool at the call and rate thresholds", () => {
    expect(
      isFailing({ calls: MIN_CALLS_FOR_RATE, failureRate: FAILING_RATE }),
    ).toBe(true);
  });

  it("does not flag a tool below the failure rate", () => {
    expect(isFailing({ calls: 1_000, failureRate: FAILING_RATE / 2 })).toBe(
      false,
    );
  });
});

describe("bareToolName", () => {
  it("strips the gateway's qualified prefix", () => {
    expect(bareToolName("linear--create_issue")).toBe("create_issue");
  });

  it("strips a double-underscore prefix", () => {
    expect(bareToolName("linear__x")).toBe("x");
  });

  it("leaves an unprefixed name alone", () => {
    expect(bareToolName("plain")).toBe("plain");
  });
});

describe("aggregateToolHealth", () => {
  it("rates failures against completed calls and sums repeated tools", () => {
    const health = aggregateToolHealth([
      { toolName: "a", eventCount: 20, successCount: 6, failureCount: 2 },
      { toolName: "a", eventCount: 10, successCount: 1, failureCount: 1 },
    ]);
    expect(health.get("a")).toEqual({ calls: 30, failureRate: 0.3 });
  });

  it("reports a zero rate when nothing completed", () => {
    expect(
      aggregateToolHealth([
        { toolName: "b", eventCount: 5, successCount: 0, failureCount: 0 },
      ]).get("b"),
    ).toEqual({ calls: 5, failureRate: 0 });
  });
});
