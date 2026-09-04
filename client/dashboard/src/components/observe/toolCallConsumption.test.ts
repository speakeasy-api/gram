import { describe, expect, it } from "vitest";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { type QueryMeasures } from "@gram/client/models/components/querymeasures.js";
import { type QueryRow } from "@gram/client/models/components/queryrow.js";
import { type QuerySeries } from "@gram/client/models/components/queryseries.js";
import {
  buildConsumptionFilters,
  consumptionBarRows,
  consumptionRowLabel,
  consumptionTimeSeriesStacks,
  hasConsumptionActivity,
  rankedConsumptionRows,
  sortByForMeasure,
  sumConsumptionRows,
  visibleConsumptionRows,
} from "./toolCallConsumption";

function measures(overrides: Partial<QueryMeasures> = {}): QueryMeasures {
  return {
    cacheCreationInputTokens: 0,
    cacheReadInputTokens: 0,
    scoredCost: 0,
    scoredTokens: 0,
    totalChats: 0,
    totalCost: 0,
    totalInputTokens: 0,
    totalOutputTokens: 0,
    totalTokens: 0,
    totalToolCalls: 0,
    totalWorkUnits: 0,
    ...overrides,
  };
}

function row(
  groupValue: string,
  overrides: Partial<QueryMeasures> = {},
): QueryRow {
  return {
    groupValue,
    measures: measures(overrides),
    dimensionValues: {},
  };
}

function series(
  groupValue: string,
  values: number[],
  measure: "calls" | "tokens" = "tokens",
): QuerySeries {
  return {
    groupValue,
    points: values.map((value, index) => ({
      bucketTimeUnixNano: String(
        (1_700_000_000 + index * 3600) * 1_000_000_000,
      ),
      measures: measures(
        measure === "calls"
          ? { totalToolCalls: value }
          : { totalTokens: value },
      ),
    })),
  };
}

describe("buildConsumptionFilters", () => {
  it("always scopes to the project and adds optional filters", () => {
    expect(buildConsumptionFilters("proj-1", [], "")).toEqual([
      { dimension: Dimension.ProjectId, values: ["proj-1"] },
    ]);
    expect(
      buildConsumptionFilters("proj-1", ["claude-code", "cursor"], "team"),
    ).toEqual([
      { dimension: Dimension.ProjectId, values: ["proj-1"] },
      { dimension: Dimension.HookSource, values: ["claude-code", "cursor"] },
      { dimension: Dimension.AccountType, values: ["team"] },
    ]);
  });
});

describe("sortByForMeasure", () => {
  it("ranks by tool calls or tokens", () => {
    expect(sortByForMeasure("calls")).toBe("total_tool_calls");
    expect(sortByForMeasure("tokens")).toBe("total_tokens");
  });
});

describe("sumConsumptionRows", () => {
  it("adds every additive measure across groups", () => {
    expect(
      sumConsumptionRows([
        row("claude-code", {
          totalToolCalls: 10,
          totalInputTokens: 100,
          totalOutputTokens: 20,
          totalTokens: 120,
          totalCost: 1.5,
        }),
        row("cursor", {
          totalToolCalls: 4,
          totalInputTokens: 50,
          totalOutputTokens: 10,
          totalTokens: 60,
          totalCost: 0.25,
        }),
      ]),
    ).toEqual({
      toolCalls: 14,
      inputTokens: 150,
      outputTokens: 30,
      tokens: 180,
      cost: 1.75,
    });
  });
});

describe("hasConsumptionActivity", () => {
  it("is false when every measure is zero", () => {
    expect(hasConsumptionActivity([row(""), row("Other")])).toBe(false);
  });

  it("is true when any group has calls, tokens, or cost", () => {
    expect(hasConsumptionActivity([row("cursor", { totalToolCalls: 1 })])).toBe(
      true,
    );
    expect(hasConsumptionActivity([row("cursor", { totalTokens: 9 })])).toBe(
      true,
    );
    expect(hasConsumptionActivity([row("cursor", { totalCost: 0.01 })])).toBe(
      true,
    );
  });
});

describe("visibleConsumptionRows", () => {
  it("keeps empty hook_source groups and drops empty MCP attribution", () => {
    const rows = [row(""), row("linear")];
    expect(
      visibleConsumptionRows(rows, Dimension.HookSource).map(
        (r) => r.groupValue,
      ),
    ).toEqual(["", "linear"]);
    expect(
      visibleConsumptionRows(rows, Dimension.McpServerName).map(
        (r) => r.groupValue,
      ),
    ).toEqual(["linear"]);
    expect(
      visibleConsumptionRows(rows, Dimension.McpToolName).map(
        (r) => r.groupValue,
      ),
    ).toEqual(["linear"]);
  });
});

describe("consumptionRowLabel", () => {
  it("uses product-surface labels for agents", () => {
    expect(consumptionRowLabel(Dimension.HookSource, "claude-code")).toBe(
      "Claude Code",
    );
    expect(consumptionRowLabel(Dimension.HookSource, "cursor")).toBe("Cursor");
  });

  it("keeps MCP names and the Other rollup as-is", () => {
    expect(consumptionRowLabel(Dimension.McpToolName, "get_issue")).toBe(
      "get_issue",
    );
    expect(consumptionRowLabel(Dimension.McpServerName, "Other")).toBe("Other");
  });
});

describe("rankedConsumptionRows", () => {
  it("orders by the active measure and hides empty MCP groups", () => {
    const ranked = rankedConsumptionRows(
      [
        row("", { totalTokens: 999, totalToolCalls: 99 }),
        row("linear", { totalTokens: 10, totalToolCalls: 2 }),
        row("github", { totalTokens: 40, totalToolCalls: 1 }),
      ],
      Dimension.McpServerName,
      "tokens",
    );
    expect(ranked.map((r) => r.groupValue)).toEqual(["github", "linear"]);
  });
});

describe("consumptionBarRows", () => {
  it("returns labeled values for the ranked chart", () => {
    expect(
      consumptionBarRows(
        [
          row("claude-code", { totalToolCalls: 12 }),
          row("cursor", { totalToolCalls: 3 }),
          row("codex", { totalToolCalls: 0 }),
        ],
        Dimension.HookSource,
        "calls",
      ),
    ).toEqual([
      { label: "Claude Code", value: 12, groupValue: "claude-code" },
      { label: "Cursor", value: 3, groupValue: "cursor" },
    ]);
  });
});

describe("consumptionTimeSeriesStacks", () => {
  it("merges same-label series and ranks stacks by volume", () => {
    const { stacks } = consumptionTimeSeriesStacks(
      [series("claude-code", [10, 0]), series("cursor", [1, 2])],
      Dimension.HookSource,
      "tokens",
    );
    expect(stacks.map((s) => s.label)).toEqual(["Claude Code", "Cursor"]);
    expect(stacks[0]?.series).toEqual([10, 0]);
  });

  it("drops empty MCP attribution series", () => {
    const { stacks } = consumptionTimeSeriesStacks(
      [series("", [100, 100]), series("linear", [4, 1])],
      Dimension.McpServerName,
      "tokens",
    );
    expect(stacks.map((s) => s.label)).toEqual(["linear"]);
  });
});
