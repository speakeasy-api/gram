import { describe, expect, it } from "vitest";
import { RULE_CATEGORY_META } from "./policy-data";
import { riskCategoryFilterOptions } from "./risk-category-options";

describe("riskCategoryFilterOptions", () => {
  it("offers every category under its display label", () => {
    const options = riskCategoryFilterOptions();

    expect(options.map((option) => option.value)).toEqual(
      Object.keys(RULE_CATEGORY_META),
    );
    expect(options.find((option) => option.value === "shadow_mcp")).toEqual({
      label: "Shadow AI",
      value: "shadow_mcp",
    });
    expect(new Set(options.map((option) => option.value)).size).toBe(
      options.length,
    );
  });
});
