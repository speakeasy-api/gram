import { describe, expect, it } from "vitest";
import { Operator } from "@gram/client/models/components";
import { buildLogFilters } from "./observeFilterUtils";

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
