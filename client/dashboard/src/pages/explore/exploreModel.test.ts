import type { ExploreMetaResult } from "@gram/client/models/components/exploremetaresult.js";
import type { ExploreSavedQuery } from "@gram/client/models/components/exploresavedquery.js";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  calculationDisplayLabel,
  calculationUnits,
  DEFAULT_SPEC,
  defaultsForDataset,
  groupExpressionsFromDrafts,
  pruneGroupExpressionsForSchema,
  queryBodyFromSpec,
  specFromSavedQuery,
  type ExploreSpec,
} from "./exploreModel";

const META: ExploreMetaResult = {
  datasets: [
    {
      name: "turn_usage",
      label: "Turn usage",
      category: "usage",
      description: "Usage for completed turns",
      grain: "turn",
      fields: [
        {
          name: "request_model",
          label: "Request model",
          type: "string",
          role: "dimension",
          unit: "",
          description: "Model requested from the AI provider",
          filterOps: ["in"],
        },
        {
          name: "response_model",
          label: "Response model",
          type: "string",
          role: "dimension",
          unit: "",
          description: "Model reported in the AI provider response",
          filterOps: ["in"],
        },
        {
          name: "cost_usd",
          label: "Cost",
          type: "float",
          role: "measure",
          unit: "usd",
          description: "AI usage cost in USD",
          filterOps: ["gte"],
        },
        {
          name: "input_tokens",
          label: "Input tokens",
          type: "int",
          role: "measure",
          unit: "tokens",
          description: "Input tokens",
          filterOps: ["gte"],
        },
      ],
    },
  ],
};

afterEach(() => {
  vi.useRealTimers();
});

describe("queryBodyFromSpec", () => {
  it("builds calculation timeseries with automatic buckets", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-17T12:15:00Z"));

    const spec: ExploreSpec = {
      ...DEFAULT_SPEC,
      dataset: "turn_usage",
      calculations: [{ op: "SUM", column: "cost_usd" }],
      groupBy: ["response_model"],
      groupExpressions: [
        {
          name: "Is Claude",
          dimension: "response_model",
          op: "in",
          values: ["claude"],
        },
      ],
      orderBy: "SUM(cost_usd)",
      limit: 10,
      window: "24h",
      chartType: "line",
    };

    const body = queryBodyFromSpec(spec, "timeseries");

    expect(body.dataset).toBe("turn_usage");
    expect(body.calculations).toEqual([{ op: "SUM", column: "cost_usd" }]);
    expect(body.groupBy).toEqual(["response_model"]);
    expect(body.groupExpressions).toEqual([
      {
        name: "Is Claude",
        dimension: "response_model",
        op: undefined,
        values: ["claude"],
      },
    ]);
    expect(body.granularitySeconds).toBe(600);
    expect(body.sortBy).toBeUndefined();
    expect(body.limit).toBe(0);
    expect(body.to).toEqual(new Date("2026-08-17T13:00:00Z"));
    expect(body.from).toEqual(new Date("2026-08-16T13:00:00Z"));
  });

  it("applies ordering and limits to summaries", () => {
    const spec: ExploreSpec = {
      ...DEFAULT_SPEC,
      calculations: [
        { op: "COUNT", column: "" },
        { op: "COUNT_DISTINCT", column: "user_key" },
      ],
      orderBy: "COUNT_DISTINCT(user_key)",
      limit: 25,
      chartType: "table",
    };

    const body = queryBodyFromSpec(spec, "summary");

    expect(body.calculations).toEqual([
      { op: "COUNT", column: undefined },
      { op: "COUNT_DISTINCT", column: "user_key" },
    ]);
    expect(body.granularitySeconds).toBe(0);
    expect(body.sortBy).toBe("COUNT_DISTINCT(user_key)");
    expect(body.limit).toBe(25);
  });
});

describe("specFromSavedQuery", () => {
  it("restores dataset calculations and saved identity", () => {
    const saved: ExploreSavedQuery = {
      id: "0198b2ab-a360-7a6c-a4c8-1153abfd8f38",
      name: "Tool calls by provider",
      chartType: "bar",
      window: "7d",
      dataset: "events",
      calculations: [
        { op: "COUNT", column: undefined },
        { op: "COUNT_DISTINCT", column: "tool_name" },
      ],
      groupBy: ["provider"],
      groupExpressions: [
        {
          name: "Successful",
          dimension: "status",
          op: "in",
          values: ["success"],
        },
      ],
      filters: [{ dimension: "status", op: "in", values: ["success"] }],
      granularitySeconds: 3600,
      sortBy: "COUNT",
      sortDesc: true,
      limit: 10,
      createdAt: new Date("2026-08-17T10:00:00Z"),
      updatedAt: new Date("2026-08-17T11:00:00Z"),
    };

    const spec = specFromSavedQuery(saved);

    expect(spec.dataset).toBe("events");
    expect(spec.calculations).toEqual([
      { op: "COUNT", column: "" },
      { op: "COUNT_DISTINCT", column: "tool_name" },
    ]);
    expect(spec.loadedQueryId).toBe(saved.id);
    expect(spec.name).toBe(saved.name);
    expect(spec.orderBy).toBe("COUNT");
    expect(spec.groupExpressions).toEqual(saved.groupExpressions);
  });
});

describe("conditional groups", () => {
  it("normalizes complete rows and drops incomplete rows", () => {
    expect(
      groupExpressionsFromDrafts([
        {
          name: " Is Claude ",
          dimension: "response_model",
          values: ["claude", "claude"],
        },
        { name: "", dimension: "response_model", values: ["other"] },
      ]),
    ).toEqual([
      {
        name: "Is Claude",
        dimension: "response_model",
        op: undefined,
        values: ["claude"],
      },
    ]);
  });

  it("prunes fields and operators unsupported by a new dataset", () => {
    expect(
      pruneGroupExpressionsForSchema(
        [
          {
            name: "Is Claude",
            dimension: "response_model",
            op: "in",
            values: ["claude"],
          },
          {
            name: "Has cost",
            dimension: "cost_usd",
            op: "eq",
            values: ["0"],
          },
        ],
        META.datasets[0],
      ),
    ).toEqual([
      {
        name: "Is Claude",
        dimension: "response_model",
        op: "in",
        values: ["claude"],
      },
    ]);
  });

  it("removes every grouping axis from number requests", () => {
    const body = queryBodyFromSpec(
      {
        ...DEFAULT_SPEC,
        chartType: "number",
        groupBy: ["event_name"],
        groupExpressions: [
          {
            name: "Is Claude",
            dimension: "response_model",
            values: ["claude"],
          },
        ],
      },
      "summary",
    );

    expect(body.groupBy).toEqual([]);
    expect(body.groupExpressions).toEqual([]);
  });
});

describe("semantic dataset presentation", () => {
  it("uses field labels and units from the active dataset", () => {
    expect(
      calculationDisplayLabel(META, "turn_usage", "SUM(input_tokens)"),
    ).toBe("Input tokens (sum)");
    expect(
      calculationUnits(META, "turn_usage", [
        "SUM(input_tokens)",
        "SUM(cost_usd)",
      ]),
    ).toEqual(["tokens", "usd"]);
  });

  it("provides grain-appropriate usage defaults", () => {
    expect(defaultsForDataset("turn_usage")).toEqual({
      calculations: [
        { op: "SUM", column: "input_tokens" },
        { op: "SUM", column: "output_tokens" },
      ],
      groupBy: ["response_model"],
    });
    expect(defaultsForDataset("user_usage")).toEqual({
      calculations: [{ op: "SUM", column: "cost_usd" }],
      groupBy: ["user_key"],
    });
  });
});
