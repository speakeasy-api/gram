import { describe, expect, it } from "vitest";
import { Operator } from "@gram/client/models/components";
import { buildLogFilters, mergeFilterChip } from "./observeFilterUtils";

describe("buildLogFilters", () => {
  it("uses exact in matching for a single curated filter value", () => {
    const result = buildLogFilters([
      {
        display: "api",
        filters: ["api"],
        path: "gram.tool_call.source",
      },
    ]);

    expect(result).toEqual([
      {
        path: "gram.tool_call.source",
        operator: Operator.In,
        values: ["api"],
      },
    ]);
  });

  it("groups multiple curated filter values by path with in matching", () => {
    const result = buildLogFilters([
      {
        display: "api",
        filters: ["api"],
        path: "gram.tool_call.source",
      },
      {
        display: "staging",
        filters: ["api-staging"],
        path: "gram.tool_call.source",
      },
      {
        display: "alex@example.com",
        filters: ["alex@example.com"],
        path: "user.email",
      },
    ]);

    expect(result).toEqual([
      {
        path: "gram.tool_call.source",
        operator: Operator.In,
        values: ["api", "api-staging"],
      },
      {
        path: "user.email",
        operator: Operator.In,
        values: ["alex@example.com"],
      },
    ]);
  });

  it("returns undefined when no filters are active", () => {
    expect(buildLogFilters([])).toBeUndefined();
  });
});

describe("mergeFilterChip", () => {
  it("merges a partially overlapping chip for the same path", () => {
    const result = mergeFilterChip(
      [
        {
          display: "api",
          filters: ["api"],
          path: "gram.tool_call.source",
        },
      ],
      {
        display: "API group",
        filters: ["api", "api-v2"],
        path: "gram.tool_call.source",
      },
    );

    expect(result).toEqual({
      filters: [
        {
          display: "api, api-v2",
          filters: ["api", "api-v2"],
          path: "gram.tool_call.source",
        },
      ],
      merged: {
        display: "api, api-v2",
        filters: ["api", "api-v2"],
        path: "gram.tool_call.source",
      },
    });
  });

  it("skips only when the incoming chip is fully subsumed", () => {
    const activeFilters = [
      {
        display: "api, api-v2",
        filters: ["api", "api-v2"],
        path: "gram.tool_call.source",
      },
    ];

    expect(
      mergeFilterChip(activeFilters, {
        display: "api",
        filters: ["api"],
        path: "gram.tool_call.source",
      }),
    ).toEqual({ filters: activeFilters, merged: null });
  });
});
