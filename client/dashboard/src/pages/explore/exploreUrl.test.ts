import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { describe, expect, it } from "vitest";
import { specProblem, type ExploreSpec } from "./exploreModel";
import { decodeSpec, encodeSpec } from "./exploreUrl";

const sessions: AnalyticsDataset = {
  name: "sessions",
  kind: "event",
  grain: "session",
  description: "One row per agent session.",
  fields: [
    {
      name: "user",
      type: "string",
      role: "dimension",
      default: true,
      operators: ["equals", "in"],
    },
    {
      name: "turn_count",
      type: "int64",
      role: "measure",
      default: false,
      aggregations: ["sum", "avg"],
    },
  ],
};

const spec: ExploreSpec = {
  dataset: "sessions",
  measures: [
    { op: "count", field: "" },
    { op: "sum", field: "turn_count" },
  ],
  filters: [{ field: "user", operator: "in", values: ["a&b=c", '"quoted"'] }],
  dimensions: ["user"],
  orderBy: "sum_turn_count",
  limit: 50,
  window: "30d",
  chartType: "bar",
};

describe("encodeSpec and decodeSpec", () => {
  it("round-trip a query, filter values included", () => {
    expect(decodeSpec(encodeSpec(spec), [sessions])).toEqual(spec);
  });

  it("round-trip a query still being composed", () => {
    const composing: ExploreSpec = {
      ...spec,
      measures: [{ op: "sum", field: "" }],
      filters: [{ field: "", operator: "in", values: [] }],
      orderBy: "",
    };
    expect(decodeSpec(encodeSpec(composing), [sessions])).toEqual(composing);
  });

  it("keep only the query, whatever else the spec object carries", () => {
    const extra = { ...spec, unrelated: true } as ExploreSpec;
    expect(JSON.parse(encodeSpec(extra))).not.toHaveProperty("unrelated");
  });

  it.each([
    ["no parameter", null],
    ["an empty parameter", ""],
    ["text that is not JSON", "{"],
    ["JSON that is not an object", "[]"],
    ["another encoding version", JSON.stringify({ ...spec, v: 2 })],
    [
      "a window the builder does not offer",
      encodeSpec({ ...spec, window: "1y" as ExploreSpec["window"] }),
    ],
    [
      "a chart type the builder does not offer",
      encodeSpec({ ...spec, chartType: "pie" as ExploreSpec["chartType"] }),
    ],
    ["a limit past the cap", encodeSpec({ ...spec, limit: 5_000 })],
    ["a fractional limit", encodeSpec({ ...spec, limit: 1.5 })],
    [
      "a filter value that is not text",
      JSON.stringify({
        ...JSON.parse(encodeSpec(spec)),
        filters: [{ field: "user", operator: "in", values: [1] }],
      }),
    ],
    [
      "an unknown filter operator",
      JSON.stringify({
        ...JSON.parse(encodeSpec(spec)),
        filters: [{ field: "user", operator: "like", values: [] }],
      }),
    ],
    [
      "a filter operator inherited from Object",
      JSON.stringify({
        ...JSON.parse(encodeSpec(spec)),
        filters: [{ field: "user", operator: "toString", values: [] }],
      }),
    ],
  ])("give nothing to restore for %s", (_, raw) => {
    expect(decodeSpec(raw, [sessions])).toBeNull();
  });

  it("give nothing to restore when the catalog no longer has what the query names", () => {
    expect(decodeSpec(encodeSpec(spec), [])).toBeNull();
    expect(
      decodeSpec(encodeSpec({ ...spec, dimensions: ["gone"] }), [sessions]),
    ).toBeNull();
  });
});

describe("specProblem", () => {
  it("passes a query the catalog can answer", () => {
    expect(specProblem([sessions], spec)).toBe("");
  });

  it.each<[string, Partial<ExploreSpec>, string]>([
    [
      "a dropped dataset",
      { dataset: "retired" },
      'dataset "retired" does not exist',
    ],
    [
      "an aggregation nothing declares",
      { measures: [{ op: "p95", field: "turn_count" }], orderBy: "" },
      "sessions has no p95 aggregation",
    ],
    [
      "a dropped measure field",
      { measures: [{ op: "sum", field: "cost" }], orderBy: "" },
      'field "cost" cannot be aggregated by sum in sessions',
    ],
    [
      "a dropped dimension",
      { dimensions: ["model"] },
      'field "model" is not a dimension of sessions',
    ],
    [
      "a dropped filter field",
      { filters: [{ field: "model", operator: "in", values: ["x"] }] },
      'field "model" cannot be filtered by in in sessions',
    ],
    [
      "a dimension asked for twice",
      { dimensions: ["user", "user"] },
      "query asks for duplicate or more than 3 dimensions",
    ],
    [
      "an equals filter with more than one value",
      { filters: [{ field: "user", operator: "equals", values: ["a", "b"] }] },
      'filter "user" has too many values',
    ],
    [
      "a filter past the value cap",
      {
        filters: [
          {
            field: "user",
            operator: "in",
            values: Array.from({ length: 101 }, (_, i) => `u${i}`),
          },
        ],
      },
      'filter "user" has too many values',
    ],
    [
      "an order naming no measure",
      { orderBy: "avg_turn_count" },
      'order by "avg_turn_count" names no measure in the query',
    ],
  ])("names %s", (_, change, reason) => {
    expect(specProblem([sessions], { ...spec, ...change })).toBe(reason);
  });
});
